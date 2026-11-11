package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/boonkerz/roster/internal/shared"
)

// Übertragung mehrerer Dateien/Ordner über die Datei-Sitzung: erst planen (Ordner
// rekursiv auflisten, vorhandene Ziele ermitteln), bei Konflikten einmal fragen, dann
// der Reihe nach übertragen. Abgebrochene Dateien bleiben als „*.roster-part“ liegen
// und werden beim nächsten Mal fortgesetzt; Verbindungsabbrüche werden bis zu dreimal
// automatisch wiederaufgenommen.

// Konflikt-Regeln für bereits vorhandene Zieldateien.
const (
	policyOverwrite = "overwrite" // immer überschreiben
	policySkipSame  = "skipsame"  // überspringen, wenn gleich groß – sonst überschreiben
	policySkip      = "skip"      // immer überspringen
)

type xferJob struct {
	src, dst string
	size     int64
	dir      bool
	exists   bool  // Zieldatei vorhanden
	dstSize  int64 // deren Größe
	part     int64 // Größe einer Teildatei (Fortsetzen)
}

// xferState ist der sichtbare Fortschritt (unter fm.mu).
type xferState struct {
	active      bool
	files, done int
	skipped     int
	total, sent int64
	cur         string
	speed       float64 // Bytes/s (geglättet)
	started     time.Time
	lastT       time.Time
	lastSent    int64
	cancel      context.CancelFunc
}

// fileSession liefert die (ggf. neu aufgebaute) Datei-Sitzung. Kennt der Agent sie nicht,
// wird das gemerkt und der langsame Weg über die Befehlswarteschlange genutzt.
func (fm *fileManager) fileSession() (*fsClient, error) {
	fm.fsMu.Lock()
	defer fm.fsMu.Unlock()
	if fm.fs != nil {
		return fm.fs, nil
	}
	if fm.fsUnsupported {
		return nil, errNoFileSession
	}
	c, err := dialFileSession(fm.cfg, fm.cl)
	if err != nil {
		if errors.Is(err, errNoFileSession) {
			fm.fsUnsupported = true
		}
		return nil, err
	}
	fm.fs = c
	if fm.batch != nil { // nach Neuverbindung mitten in einer Sammelübertragung
		_, _ = c.call(*fm.batch, 30*time.Second)
	}
	return c, nil
}

// dropSession verwirft eine kaputte Sitzung (nächster Zugriff baut neu auf).
func (fm *fileManager) dropSession(c *fsClient) {
	fm.fsMu.Lock()
	if fm.fs == c {
		fm.fs = nil
	}
	fm.fsMu.Unlock()
	c.close()
}

// selection liefert die markierten Einträge eines Panels, sonst den aktuellen.
func (p *fmPane) selection() []fmEntry {
	var out []fmEntry
	for _, e := range p.entries {
		if e.name != ".." && p.marked[e.path] {
			out = append(out, e)
		}
	}
	if len(out) == 0 && len(p.entries) > 0 && p.sel < len(p.entries) && p.entries[p.sel].name != ".." {
		out = append(out, p.entries[p.sel])
	}
	return out
}

// runTransfer überträgt items von src nach dstDir (Gegenseite) über die Datei-Sitzung.
func (fm *fileManager) runTransfer(ctx context.Context, items []fmEntry, dstDir string, srcRemote bool) error {
	dstRemote := !srcRemote
	fm.setStatus("Plane Übertragung …")
	jobs, err := fm.plan(ctx, items, dstDir, srcRemote, dstRemote)
	if err != nil {
		return err
	}
	conflicts := 0
	for _, j := range jobs {
		if !j.dir && j.exists {
			conflicts++
		}
	}
	policy := policyOverwrite
	if conflicts > 0 {
		ans, err := fm.ask(ctx, conflicts)
		if err != nil {
			return err
		}
		policy = ans
	}
	// Ordner bzw. mehrere Einträge: fürs Audit-Log als EINE Übertragung klammern.
	if len(items) > 1 || items[0].dir {
		fm.beginBatch(items, dstDir)
		defer fm.endBatch()
	}
	var total int64
	files := 0
	for _, j := range jobs {
		if !j.dir {
			files++
			total += j.size
		}
	}
	fm.mu.Lock()
	fm.xfer.files, fm.xfer.total = files, total
	fm.xfer.started, fm.xfer.lastT = time.Now(), time.Now()
	fm.mu.Unlock()

	var base int64 // bereits abgeschlossene Bytes
	for _, j := range jobs {
		if err := ctx.Err(); err != nil {
			return err
		}
		if j.dir {
			if err := fm.mkdirSide(ctx, j.dst, dstRemote); err != nil {
				return fmt.Errorf("Ordner %s: %v", j.dst, err)
			}
			continue
		}
		skip := j.exists && (policy == policySkip || (policy == policySkipSame && j.dstSize == j.size))
		if skip {
			base += j.size
			fm.mu.Lock()
			fm.xfer.done++
			fm.xfer.skipped++
			fm.xfer.sent = base
			fm.mu.Unlock()
			continue
		}
		resume := int64(0)
		if !j.exists && j.part > 0 && j.part <= j.size {
			resume = j.part
		}
		name := filepath.Base(filepath.FromSlash(strings.ReplaceAll(j.src, "\\", "/")))
		fm.mu.Lock()
		fm.xfer.cur = name
		fm.mu.Unlock()
		b := base
		on := func(n int64) { fm.progress(b + n) }
		if err := fm.copyOne(ctx, j, srcRemote, resume, on); err != nil {
			return fmt.Errorf("%s: %v", name, err)
		}
		base += j.size
		fm.mu.Lock()
		fm.xfer.done++
		fm.mu.Unlock()
		fm.progress(base)
	}
	return nil
}

// beginBatch/endBatch klammern eine Sammelübertragung (Agents vor 0.30.1 antworten mit
// einem Fehler – das ist egal, dann protokolliert der Server je Datei).
func (fm *fileManager) beginBatch(items []fmEntry, dstDir string) {
	var names []string
	for i, it := range items {
		if i == 5 {
			names = append(names, fmt.Sprintf("+%d", len(items)-5))
			break
		}
		n := it.name
		if it.dir {
			n += "/"
		}
		names = append(names, n)
	}
	req := shared.FileReq{Op: "begin", Path: dstDir, Label: strings.Join(names, ", ")}
	fm.fsMu.Lock()
	fm.batch = &req
	c := fm.fs
	fm.fsMu.Unlock()
	if c != nil {
		_, _ = c.call(req, 30*time.Second)
	}
}

func (fm *fileManager) endBatch() {
	fm.fsMu.Lock()
	fm.batch = nil
	c := fm.fs
	fm.fsMu.Unlock()
	if c != nil {
		_, _ = c.call(shared.FileReq{Op: "end"}, 30*time.Second)
	}
}

// copyOne überträgt eine Datei; bei Verbindungsabbruch bis zu dreimal neu verbinden
// und fortsetzen.
func (fm *fileManager) copyOne(ctx context.Context, j xferJob, srcRemote bool, resume int64, on func(int64)) error {
	var err error
	for attempt := 0; attempt < 4; attempt++ {
		if attempt > 0 {
			fm.setStatus(fmt.Sprintf("Verbindung unterbrochen – setze fort (Versuch %d) …", attempt+1))
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Duration(attempt) * 2 * time.Second):
			}
		}
		var c *fsClient
		if c, err = fm.fileSession(); err != nil {
			continue
		}
		if srcRemote {
			err = fm.download(ctx, c, j, on)
		} else {
			err = fm.upload(ctx, c, j, resume, on)
		}
		if err == nil || isRemoteErr(err) || ctx.Err() != nil {
			if err != nil && !isRemoteErr(err) {
				fm.dropSession(c) // abgebrochener Datenstrom – Verbindung unbrauchbar
			}
			return err
		}
		fm.dropSession(c)
		// Fortsetzen: aktuelle Teildatei-Größe auf der Zielseite neu ermitteln.
		if !srcRemote {
			if nc, nerr := fm.fileSession(); nerr == nil {
				if st, serr := nc.call(shared.FileReq{Op: "stat", Path: j.dst}, 30*time.Second); serr == nil {
					resume = st.Part
				}
			}
		}
	}
	return err
}

func (fm *fileManager) upload(ctx context.Context, c *fsClient, j xferJob, resume int64, on func(int64)) error {
	f, err := os.Open(j.src)
	if err != nil {
		return &remoteErr{err.Error()}
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return &remoteErr{err.Error()}
	}
	if fi.Size() > maxViewerTransfer {
		return &remoteErr{"zu groß (max 4 GB)"}
	}
	return c.put(ctx, j.dst, f, fi.Size(), resume, on)
}

// download schreibt in „<ziel>.roster-part“ (ggf. fortgesetzt) und benennt danach um.
func (fm *fileManager) download(ctx context.Context, c *fsClient, j xferJob, on func(int64)) error {
	part := j.dst + shared.FilePartSuffix
	if err := os.MkdirAll(filepath.Dir(j.dst), 0o755); err != nil {
		return &remoteErr{err.Error()}
	}
	offset := int64(0)
	flags := os.O_WRONLY | os.O_CREATE | os.O_TRUNC
	if !j.exists {
		if fi, err := os.Stat(part); err == nil && fi.Size() <= j.size {
			offset, flags = fi.Size(), os.O_WRONLY|os.O_APPEND
		}
	}
	f, err := os.OpenFile(part, flags, 0o644)
	if err != nil {
		return &remoteErr{err.Error()}
	}
	size, err := c.get(ctx, j.src, offset, f, on)
	if cerr := f.Close(); err == nil && cerr != nil {
		err = &remoteErr{cerr.Error()}
	}
	if err != nil {
		return err
	}
	if fi, serr := os.Stat(part); serr != nil || fi.Size() != size {
		os.Remove(part)
		return &remoteErr{"unvollständig empfangen"}
	}
	if err := os.Rename(part, j.dst); err != nil {
		return &remoteErr{err.Error()}
	}
	return nil
}

func (fm *fileManager) mkdirSide(ctx context.Context, path string, remote bool) error {
	if !remote {
		return os.MkdirAll(path, 0o755)
	}
	c, err := fm.fileSession()
	if err != nil {
		return err
	}
	_, err = c.call(shared.FileReq{Op: "mkdir", Path: path}, time.Minute)
	if err != nil && !isRemoteErr(err) {
		fm.dropSession(c)
	}
	return err
}

// plan listet die Quelle (Ordner rekursiv) und ermittelt vorhandene Ziele samt Teildateien.
func (fm *fileManager) plan(ctx context.Context, items []fmEntry, dstDir string, srcRemote, dstRemote bool) ([]xferJob, error) {
	var jobs []xferJob
	for _, it := range items {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		dstTop := joinFor(dstDir, it.name, dstRemote)
		if !it.dir {
			jobs = append(jobs, xferJob{src: it.path, dst: dstTop, size: it.size})
			continue
		}
		jobs = append(jobs, xferJob{src: it.path, dst: dstTop, dir: true})
		entries, err := fm.walkSide(it.path, srcRemote)
		if err != nil {
			return nil, fmt.Errorf("%s auflisten: %v", it.name, err)
		}
		for _, e := range entries {
			if strings.HasSuffix(e.Rel, shared.FilePartSuffix) {
				continue // eigene Teildateien nie mitkopieren
			}
			jobs = append(jobs, xferJob{
				src: joinRel(it.path, e.Rel, srcRemote), dst: joinRel(dstTop, e.Rel, dstRemote),
				size: e.Size, dir: e.Dir,
			})
		}
	}
	// Vorhandene Ziele: pro oberstem Eintrag einmal auflisten statt je Datei zu fragen.
	existing := map[string]int64{}
	for _, it := range items {
		dstTop := joinFor(dstDir, it.name, dstRemote)
		if it.dir {
			entries, err := fm.walkSide(dstTop, dstRemote)
			if err != nil {
				return nil, fmt.Errorf("Ziel prüfen: %v", err)
			}
			for _, e := range entries {
				if !e.Dir {
					existing[joinRel(dstTop, e.Rel, dstRemote)] = e.Size
				}
			}
			continue
		}
		for _, p := range []string{dstTop, dstTop + shared.FilePartSuffix} {
			if sz, ok, err := fm.statSide(p, dstRemote); err != nil {
				return nil, err
			} else if ok {
				existing[p] = sz
			}
		}
	}
	for i := range jobs {
		if jobs[i].dir {
			continue
		}
		if sz, ok := existing[jobs[i].dst]; ok {
			jobs[i].exists, jobs[i].dstSize = true, sz
		}
		if sz, ok := existing[jobs[i].dst+shared.FilePartSuffix]; ok {
			jobs[i].part = sz
		}
	}
	return jobs, nil
}

func (fm *fileManager) walkSide(root string, remote bool) ([]shared.FileWalkEntry, error) {
	if !remote {
		return walkLocal(root)
	}
	c, err := fm.fileSession()
	if err != nil {
		return nil, err
	}
	r, err := c.call(shared.FileReq{Op: "walk", Path: root}, 5*time.Minute)
	if err != nil && !isRemoteErr(err) {
		fm.dropSession(c)
	}
	return r.Entries, err
}

func (fm *fileManager) statSide(p string, remote bool) (int64, bool, error) {
	if !remote {
		fi, err := os.Stat(p)
		if err != nil {
			return 0, false, nil
		}
		return fi.Size(), !fi.IsDir(), nil
	}
	c, err := fm.fileSession()
	if err != nil {
		return 0, false, err
	}
	r, err := c.call(shared.FileReq{Op: "stat", Path: p}, 30*time.Second)
	if err != nil {
		if !isRemoteErr(err) {
			fm.dropSession(c)
		}
		return 0, false, err
	}
	return r.Size, r.Exists && !r.Dir, nil
}

func walkLocal(root string) ([]shared.FileWalkEntry, error) {
	if _, err := os.Stat(root); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []shared.FileWalkEntry
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || p == root || d.Type()&fs.ModeSymlink != 0 {
			return nil
		}
		rel, rerr := filepath.Rel(root, p)
		if rerr != nil {
			return nil
		}
		e := shared.FileWalkEntry{Rel: filepath.ToSlash(rel), Dir: d.IsDir()}
		if !e.Dir {
			if fi, ierr := d.Info(); ierr == nil {
				e.Size = fi.Size()
			}
		}
		out = append(out, e)
		return nil
	})
	return out, err
}

// joinRel hängt einen relativen „a/b/c“-Pfad an (Geräteseite mit deren Trennzeichen).
func joinRel(base, rel string, remote bool) string {
	if !remote {
		return filepath.Join(base, filepath.FromSlash(rel))
	}
	p := base
	for _, part := range strings.Split(rel, "/") {
		p = remoteJoin(p, part)
	}
	return p
}

// ask zeigt die Konfliktfrage und wartet auf die Antwort.
func (fm *fileManager) ask(ctx context.Context, conflicts int) (string, error) {
	ch := make(chan string, 1)
	fm.mu.Lock()
	fm.askCh = ch
	fm.prompt = promptState{kind: "conflict", label: fmt.Sprintf(
		"%d Datei(en) gibt es am Ziel schon:  [O] überschreiben   [G] überspringen, wenn gleich groß   [S] überspringen   [Esc] abbrechen", conflicts)}
	fm.mu.Unlock()
	select {
	case a := <-ch:
		if a == "" {
			return "", context.Canceled
		}
		return a, nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

// answer beantwortet die Konfliktfrage ("" = abbrechen). Mit gehaltenem fm.mu aufrufen.
func (fm *fileManager) answerLocked(a string) {
	if fm.askCh != nil {
		fm.askCh <- a
		fm.askCh = nil
	}
	fm.prompt = promptState{}
}

// progress aktualisiert Bytes und Geschwindigkeit (geglättet).
func (fm *fileManager) progress(sent int64) {
	fm.mu.Lock()
	defer fm.mu.Unlock()
	x := &fm.xfer
	x.sent = sent
	if dt := time.Since(x.lastT).Seconds(); dt >= 0.5 {
		inst := float64(sent-x.lastSent) / dt
		if x.speed == 0 {
			x.speed = inst
		} else {
			x.speed = 0.7*x.speed + 0.3*inst
		}
		x.lastT, x.lastSent = time.Now(), sent
	}
}

// statusLine fasst den Fortschritt zusammen (unter fm.mu).
func (x *xferState) statusLine() string {
	s := fmt.Sprintf("%d/%d Dateien · %s / %s", x.done, x.files, humanSize(x.sent), humanSize(x.total))
	if x.speed > 0 {
		s += fmt.Sprintf(" · %s/s", humanSize(int64(x.speed)))
		if rest := x.total - x.sent; rest > 0 {
			s += " · noch " + fmtDur(time.Duration(float64(rest)/x.speed)*time.Second)
		}
	}
	if x.cur != "" {
		s += " · " + x.cur
	}
	return s + "   (Esc = abbrechen)"
}

func fmtDur(d time.Duration) string {
	d = d.Round(time.Second)
	if d >= time.Hour {
		return fmt.Sprintf("%d:%02d:%02d", int(d.Hours()), int(d.Minutes())%60, int(d.Seconds())%60)
	}
	return fmt.Sprintf("%d:%02d", int(d.Minutes()), int(d.Seconds())%60)
}
