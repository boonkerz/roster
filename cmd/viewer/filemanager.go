package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/jupiterrider/purego-sdl3/sdl"

	"github.com/boonkerz/roster/internal/sdlui"
	"github.com/boonkerz/roster/internal/shared"
)

// Zweipanel-Dateimanager (Midnight-Commander-Stil) als Overlay über dem Remote-Bild.
// Links das lokale Dateisystem des Operators, rechts das des Geräts. Der Geräte-Zugriff
// läuft über dieselbe Command-Queue wie die Web-UI (viewer-files-API, Token-Auth).
// Alle Netz-/Dateioperationen laufen in Goroutinen und aktualisieren den Zustand unter
// fm.mu; die Render-Schleife zeichnet währenddessen einen „…"-Hinweis.

type fmEntry struct {
	name string
	path string
	dir  bool
	size int64
}

type fmPane struct {
	remote  bool
	path    string
	entries []fmEntry
	sel     int
	top     int
	err     string
	loading bool
	marked  map[string]bool // Mehrfachauswahl (Pfad → markiert)
}

func (p *fmPane) markCount() int {
	n := 0
	for _, on := range p.marked {
		if on {
			n++
		}
	}
	return n
}

func (p *fmPane) toggleMark(i int) {
	if i < 0 || i >= len(p.entries) || p.entries[i].name == ".." {
		return
	}
	if p.marked == nil {
		p.marked = map[string]bool{}
	}
	k := p.entries[i].path
	if p.marked[k] {
		delete(p.marked, k)
	} else {
		p.marked[k] = true
	}
}

// promptState hält eine modale Eingabe (Ordnername) bzw. eine Bestätigung (Löschen).
type promptState struct {
	kind  string   // "" | "mkdir" | "delete" | "conflict"
	text  string   // Eingabepuffer (mkdir)
	paths []string // zu löschende Pfade (delete)
	label string
}

type fileManager struct {
	txt *sdlui.Text
	rn  *sdl.Renderer
	cl  *fmClient
	cfg *launchConfig

	// Datei-Sitzung (schneller Kanal); fsUnsupported = Agent zu alt → Befehlswarteschlange.
	fsMu          sync.Mutex
	fs            *fsClient
	fsUnsupported bool
	batch         *shared.FileReq // laufende Sammelübertragung (begin), für Neuverbindungen

	xfer   xferState
	askCh  chan string
	active bool
	scale  float32 // UI-Skalierung (HiDPI); 1.0 = normal

	mu     sync.Mutex
	local  fmPane
	remote fmPane
	focus  int // 0 = lokal, 1 = Gerät
	status string
	busy   bool
	prompt promptState

	// Layout-Trefferzonen (gesetzt beim Zeichnen, gelesen bei Mausklick).
	colX  [2]float32
	colW  float32
	listY float32
	rowH  float32

	// Buttonleiste (gesetzt beim Zeichnen, gelesen bei Mausklick).
	btns       []fmButton
	btnY, btnH float32
}

// fmButton ist ein anklickbarer Knopf in der Aktionsleiste des Dateimanagers.
type fmButton struct {
	id, label string
	x, w      float32
}

func newFileManager(txt *sdlui.Text, cfg *launchConfig) *fileManager {
	fm := &fileManager{
		txt:   txt,
		rn:    txt.Renderer(),
		cl:    newFMClient(cfg),
		cfg:   cfg,
		scale: 1,
	}
	fm.local.remote = false
	fm.remote.remote = true
	home, _ := os.UserHomeDir()
	if home == "" {
		home = "/"
	}
	fm.local.path = home
	fm.remote.path = "" // "" = Wurzeln (Laufwerke bzw. /) auf dem Gerät
	return fm
}

// toggle öffnet/schließt den Manager; beim ersten Öffnen werden beide Panels geladen.
func (fm *fileManager) toggle() {
	fm.active = !fm.active
	if !fm.active {
		return
	}
	fm.mu.Lock()
	needLocal := fm.local.entries == nil
	needRemote := fm.remote.entries == nil
	fm.mu.Unlock()
	if needLocal {
		fm.load(&fm.local, fm.local.path)
	}
	if needRemote {
		fm.load(&fm.remote, fm.remote.path)
	}
	go func() { _, _ = fm.fileSession() }() // schnellen Kanal vorab aufbauen
}

func (fm *fileManager) curPane() *fmPane {
	if fm.focus == 1 {
		return &fm.remote
	}
	return &fm.local
}

func (fm *fileManager) otherPane() *fmPane {
	if fm.focus == 1 {
		return &fm.local
	}
	return &fm.remote
}

// load füllt ein Panel (async). Für das lokale Panel per os.ReadDir, für das Gerät
// über die viewer-files-API.
func (fm *fileManager) load(p *fmPane, path string) {
	fm.mu.Lock()
	p.loading = true
	p.err = ""
	fm.mu.Unlock()
	go func() {
		var l fmListing
		if p.remote {
			l = fm.remoteBrowse(path)
		} else {
			l = localBrowse(path)
		}
		fm.mu.Lock()
		p.loading = false
		if l.err != "" {
			p.err = l.err
		} else {
			p.path = l.path
			p.entries = withParent(l)
			p.sel, p.top = 0, 0
			p.marked = nil
		}
		fm.mu.Unlock()
	}()
}

// withParent stellt einen „../"-Eintrag voran (sofern ein Elternverzeichnis existiert).
func withParent(l fmListing) []fmEntry {
	out := make([]fmEntry, 0, len(l.entries)+1)
	if l.parent != "" || l.path != "" {
		out = append(out, fmEntry{name: "..", path: l.parent, dir: true})
	}
	out = append(out, l.entries...)
	return out
}

// --- Eingabe ---

// onKey verarbeitet einen Tastendruck. Liefert true, wenn er verbraucht wurde.
func (fm *fileManager) onKey(sym sdl.Keycode, mod sdl.Keymod) bool {
	fm.mu.Lock()
	// Konfliktfrage einer laufenden Übertragung.
	if fm.prompt.kind == "conflict" {
		switch sym {
		case sdl.KeycodeO:
			fm.answerLocked(policyOverwrite)
		case sdl.KeycodeG:
			fm.answerLocked(policySkipSame)
		case sdl.KeycodeS:
			fm.answerLocked(policySkip)
		case sdl.KeycodeEscape:
			fm.answerLocked("")
		}
		fm.mu.Unlock()
		return true
	}
	// Laufende Übertragung: Esc bricht ab (Teildateien bleiben zum Fortsetzen).
	if fm.xfer.active && sym == sdl.KeycodeEscape {
		if fm.xfer.cancel != nil {
			fm.xfer.cancel()
		}
		fm.mu.Unlock()
		return true
	}
	// Modale Eingabe zuerst.
	if fm.prompt.kind != "" {
		switch sym {
		case sdl.KeycodeEscape:
			fm.prompt = promptState{}
		case sdl.KeycodeReturn, sdl.KeycodeKpEnter:
			pr := fm.prompt
			fm.prompt = promptState{}
			fm.mu.Unlock()
			fm.commitPrompt(pr)
			return true
		case sdl.KeycodeBackspace:
			if fm.prompt.kind == "mkdir" && fm.prompt.text != "" {
				r := []rune(fm.prompt.text)
				fm.prompt.text = string(r[:len(r)-1])
			}
		}
		fm.mu.Unlock()
		return true
	}
	p := fm.curPane()
	n := len(p.entries)
	switch sym {
	case sdl.KeycodeEscape:
		fm.mu.Unlock()
		fm.active = false
		return true
	case sdl.KeycodeTab:
		fm.focus ^= 1
	case sdl.KeycodeUp:
		if p.sel > 0 {
			p.sel--
		}
	case sdl.KeycodeDown:
		if p.sel < n-1 {
			p.sel++
		}
	case sdl.KeycodePageUp:
		p.sel -= 12
		if p.sel < 0 {
			p.sel = 0
		}
	case sdl.KeycodePageDown:
		p.sel += 12
		if p.sel > n-1 {
			p.sel = n - 1
		}
	case sdl.KeycodeHome:
		p.sel = 0
	case sdl.KeycodeEnd:
		p.sel = n - 1
	case sdl.KeycodeReturn, sdl.KeycodeKpEnter, sdl.KeycodeRight:
		fm.mu.Unlock()
		fm.enter(p)
		return true
	case sdl.KeycodeBackspace, sdl.KeycodeLeft:
		fm.mu.Unlock()
		fm.goParent(p)
		return true
	case sdl.KeycodeF5:
		fm.mu.Unlock()
		fm.transfer()
		return true
	case sdl.KeycodeSpace, sdl.KeycodeInsert:
		// Markieren und weiter (wie im Midnight Commander).
		p.toggleMark(p.sel)
		if p.sel < n-1 {
			p.sel++
		}
	case sdl.KeycodeA:
		if mod&sdl.KeymodCtrl == 0 {
			fm.mu.Unlock()
			return false
		}
		// Strg+A: alle markieren bzw. – wenn schon alle markiert – keine.
		all := p.markCount() >= len(p.entries)-1
		p.marked = map[string]bool{}
		if !all {
			for _, e := range p.entries {
				if e.name != ".." {
					p.marked[e.path] = true
				}
			}
		}
	case sdl.KeycodeF7:
		fm.prompt = promptState{kind: "mkdir", label: "Neuer Ordner in " + paneLabel(p) + ":"}
	case sdl.KeycodeF8, sdl.KeycodeDelete:
		fm.prompt = deletePrompt(p)
	default:
		fm.mu.Unlock()
		return false
	}
	fm.mu.Unlock()
	return true
}

// onText nimmt getippte Zeichen für die modale Eingabe (Ordnername) entgegen.
func (fm *fileManager) onText(s string) {
	fm.mu.Lock()
	defer fm.mu.Unlock()
	if fm.prompt.kind == "mkdir" {
		fm.prompt.text += s
	}
}

// enter öffnet das ausgewählte Verzeichnis (oder ../).
func (fm *fileManager) enter(p *fmPane) {
	fm.mu.Lock()
	if len(p.entries) == 0 {
		fm.mu.Unlock()
		return
	}
	e := p.entries[p.sel]
	fm.mu.Unlock()
	if !e.dir {
		return
	}
	if e.name == ".." {
		fm.goParent(p)
		return
	}
	fm.load(p, e.path)
}

func (fm *fileManager) goParent(p *fmPane) {
	fm.mu.Lock()
	entries := p.entries
	fm.mu.Unlock()
	if len(entries) > 0 && entries[0].name == ".." {
		fm.load(p, entries[0].path)
	}
}

// transfer kopiert die Auswahl (markierte Einträge, sonst den aktuellen) ins andere
// Panel (F5) – Dateien und ganze Ordner. Läuft asynchron mit Fortschritt in der Fußzeile.
func (fm *fileManager) transfer() {
	fm.mu.Lock()
	if fm.busy {
		fm.mu.Unlock()
		return
	}
	src := fm.curPane()
	dst := fm.otherPane()
	items := src.selection()
	dstPath := dst.path
	srcRemote := src.remote
	fm.mu.Unlock()
	if len(items) == 0 {
		return
	}
	if dstPath == "" {
		fm.setStatus("Bitte im Zielpanel zuerst einen Ordner öffnen")
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	fm.mu.Lock()
	fm.busy = true
	fm.xfer = xferState{active: true, cancel: cancel}
	fm.mu.Unlock()
	go func() {
		defer cancel()
		start := time.Now()
		err := fm.runTransfer(ctx, items, dstPath, srcRemote)
		if errors.Is(err, errNoFileSession) {
			err = fm.legacyTransfer(items, dstPath, srcRemote)
		}
		fm.mu.Lock()
		x := fm.xfer
		fm.xfer = xferState{}
		fm.busy = false
		if err == nil {
			src.marked = nil
		}
		fm.mu.Unlock()
		switch {
		case errors.Is(err, context.Canceled):
			fm.setStatus(fmt.Sprintf("Abgebrochen nach %d/%d Dateien – erneut F5 setzt fort", x.done, x.files))
		case err != nil:
			fm.setStatus("Fehler: " + err.Error())
		default:
			d := time.Since(start)
			msg := fmt.Sprintf("✓ %d Datei(en) übertragen", x.done-x.skipped)
			if x.skipped > 0 {
				msg += fmt.Sprintf(", %d übersprungen", x.skipped)
			}
			if x.total > 0 && d > 0 {
				msg += fmt.Sprintf(" · %s in %s (%s/s)", humanSize(x.total), fmtDur(d), humanSize(int64(float64(x.sent)/d.Seconds())))
			}
			fm.setStatus(msg)
		}
		fm.load(dst, dstPath) // Zielpanel aktualisieren
	}()
}

// legacyTransfer: alter Weg über die Befehlswarteschlange (Agents vor 0.29.0).
func (fm *fileManager) legacyTransfer(items []fmEntry, dstPath string, srcRemote bool) error {
	var count int
	for _, e := range items {
		fm.setStatus("Übertrage " + e.name + " … (langsamer Modus: Agent aktualisieren)")
		if err := fm.copyTree(e, dstPath, srcRemote, !srcRemote, &count); err != nil {
			return err
		}
	}
	return nil
}

// remoteBrowse listet über die Datei-Sitzung, falls schon verbunden – sonst klassisch.
func (fm *fileManager) remoteBrowse(path string) fmListing {
	fm.fsMu.Lock()
	c := fm.fs
	fm.fsMu.Unlock()
	if c == nil {
		return fm.cl.browse(path)
	}
	r, err := c.call(shared.FileReq{Op: "list", Path: path}, time.Minute)
	if err != nil {
		if !isRemoteErr(err) {
			fm.dropSession(c)
		}
		return fm.cl.browse(path)
	}
	return parseListing(path, string(r.Listing))
}

// deletePrompt baut die Löschbestätigung für Markierte bzw. den aktuellen Eintrag.
func deletePrompt(p *fmPane) promptState {
	items := p.selection()
	if len(items) == 0 {
		return promptState{}
	}
	pr := promptState{kind: "delete"}
	for _, e := range items {
		pr.paths = append(pr.paths, e.path)
	}
	if len(items) == 1 {
		pr.label = "Löschen: " + items[0].name + "  (Enter=ja, Esc=nein)"
	} else {
		pr.label = fmt.Sprintf("%d Einträge löschen?  (Enter=ja, Esc=nein)", len(items))
	}
	return pr
}

// copyTree kopiert src (Datei oder Ordner) rekursiv in das Zielverzeichnis dstDir.
// srcRemote/dstRemote geben die Seiten an. count zählt tatsächlich kopierte Dateien.
func (fm *fileManager) copyTree(src fmEntry, dstDir string, srcRemote, dstRemote bool, count *int) error {
	if !src.dir {
		return fm.copyFile(src, dstDir, srcRemote, dstRemote, count)
	}
	target := joinFor(dstDir, src.name, dstRemote)
	if err := fm.mkdirFor(target, dstRemote); err != nil {
		return fmt.Errorf("Ordner %s: %s", src.name, err)
	}
	var l fmListing
	if srcRemote {
		l = fm.cl.browse(src.path)
	} else {
		l = localBrowse(src.path)
	}
	if l.err != "" {
		return fmt.Errorf("%s: %s", src.name, l.err)
	}
	for _, c := range l.entries {
		if c.name == "." || c.name == ".." {
			continue
		}
		if err := fm.copyTree(c, target, srcRemote, dstRemote, count); err != nil {
			return err
		}
	}
	return nil
}

// copyFile überträgt eine einzelne Datei zwischen den Seiten.
func (fm *fileManager) copyFile(e fmEntry, dstDir string, srcRemote, dstRemote bool, count *int) error {
	target := joinFor(dstDir, e.name, dstRemote)
	progress := func(s string) { fm.setStatus(e.name + ": " + s) }
	var err error
	if srcRemote {
		err = fm.cl.readTo(e.path, target, progress)
	} else {
		var f *os.File
		if f, err = os.Open(e.path); err == nil {
			var fi os.FileInfo
			if fi, err = f.Stat(); err == nil && fi.Size() > maxViewerTransfer {
				err = fmt.Errorf("zu groß (max 4 GB)")
			} else if err == nil {
				err = fm.cl.write(target, f, fi.Size(), progress)
			}
			f.Close()
		}
	}
	if err != nil {
		return fmt.Errorf("%s: %s", e.name, err)
	}
	*count++
	fm.setStatus(fmt.Sprintf("kopiere … %s  (%d)", e.name, *count))
	return nil
}

// joinFor verbindet Verzeichnis + Name für die jeweilige Seite (Remote: geräte-
// gerechtes Trennzeichen; lokal: filepath.Join).
func joinFor(dir, name string, remote bool) string {
	if remote {
		return remoteJoin(dir, name)
	}
	return filepath.Join(dir, name)
}

// mkdirFor legt ein Verzeichnis auf der jeweiligen Seite an (idempotent: MkdirAll
// bzw. agentseitig ebenfalls MkdirAll – bestehende Ordner sind kein Fehler).
func (fm *fileManager) mkdirFor(path string, remote bool) error {
	if remote {
		return fm.cl.mkdir(path)
	}
	return os.MkdirAll(path, 0755)
}

// commitPrompt führt die bestätigte modale Aktion aus (Ordner anlegen / löschen).
func (fm *fileManager) commitPrompt(pr promptState) {
	switch pr.kind {
	case "mkdir":
		name := strings.TrimSpace(pr.text)
		if name == "" {
			return
		}
		fm.mu.Lock()
		p := fm.curPane()
		base := p.path
		remote := p.remote
		fm.mu.Unlock()
		go func() {
			var err error
			if remote {
				err = fm.cl.mkdir(remoteJoin(base, name))
			} else {
				err = os.Mkdir(filepath.Join(base, name), 0755)
			}
			if err != nil {
				fm.setStatus("Fehler: " + err.Error())
				return
			}
			fm.setStatus("✓ Ordner angelegt: " + name)
			fm.load(p, base)
		}()
	case "delete":
		fm.mu.Lock()
		p := fm.curPane()
		base := p.path
		remote := p.remote
		fm.mu.Unlock()
		go func() {
			for _, path := range pr.paths {
				var err error
				if remote {
					err = fm.cl.delete(path)
				} else {
					err = os.RemoveAll(path)
				}
				if err != nil {
					fm.setStatus("Fehler: " + err.Error())
					fm.load(p, base)
					return
				}
			}
			fm.setStatus(fmt.Sprintf("✓ %d gelöscht", len(pr.paths)))
			fm.load(p, base)
		}()
	}
}

func (fm *fileManager) setStatus(s string) {
	fm.mu.Lock()
	fm.status = s
	fm.mu.Unlock()
}

// doButton führt eine über die Buttonleiste ausgelöste Aktion aus (ohne gehaltenes
// fm.mu – die einzelnen Helfer sperren selbst).
func (fm *fileManager) doButton(id string) {
	switch id {
	case "copy":
		fm.transfer()
	case "mkdir":
		fm.startMkdir()
	case "delete":
		fm.startDelete()
	case "refresh":
		fm.refresh()
	case "close":
		fm.active = false
	case policyOverwrite, policySkipSame, policySkip, "cancel":
		fm.mu.Lock()
		if id == "cancel" {
			id = ""
		}
		fm.answerLocked(id)
		fm.mu.Unlock()
	case "abort":
		fm.mu.Lock()
		if fm.xfer.cancel != nil {
			fm.xfer.cancel()
		}
		fm.mu.Unlock()
	}
}

// startMkdir öffnet die modale Ordner-Eingabe für das fokussierte Panel.
func (fm *fileManager) startMkdir() {
	fm.mu.Lock()
	defer fm.mu.Unlock()
	p := fm.curPane()
	fm.prompt = promptState{kind: "mkdir", label: "Neuer Ordner in " + paneLabel(p) + ":"}
}

// startDelete öffnet die Löschbestätigung für den ausgewählten Eintrag.
func (fm *fileManager) startDelete() {
	fm.mu.Lock()
	defer fm.mu.Unlock()
	fm.prompt = deletePrompt(fm.curPane())
}

// refresh lädt das fokussierte Panel neu.
func (fm *fileManager) refresh() {
	fm.mu.Lock()
	p := fm.curPane()
	base := p.path
	fm.mu.Unlock()
	fm.load(p, base)
}

// --- Maus ---

// onClick verarbeitet einen Linksklick (Fensterkoordinaten). Liefert true, wenn er
// im Manager lag.
func (fm *fileManager) onClick(mx, my float32, double bool) bool {
	fm.mu.Lock()
	// Buttonleiste (unter den Listen, über der Fußzeile) zuerst prüfen.
	if fm.btnH > 0 && my >= fm.btnY && my < fm.btnY+fm.btnH {
		id := ""
		for _, b := range fm.btns {
			if mx >= b.x && mx < b.x+b.w {
				id = b.id
				break
			}
		}
		fm.mu.Unlock()
		if id != "" {
			fm.doButton(id)
		}
		return true
	}
	if my < fm.listY || fm.rowH <= 0 {
		fm.mu.Unlock()
		return false
	}
	col := -1
	for i := 0; i < 2; i++ {
		if mx >= fm.colX[i] && mx < fm.colX[i]+fm.colW {
			col = i
		}
	}
	if col < 0 {
		fm.mu.Unlock()
		return true // Klick im Overlay, aber neben den Listen
	}
	fm.focus = col
	p := fm.curPane()
	idx := p.top + int((my-fm.listY)/fm.rowH)
	if idx >= 0 && idx < len(p.entries) {
		mod := sdl.GetModState()
		switch {
		case mod&sdl.KeymodCtrl != 0: // Strg+Klick: einzeln markieren
			p.toggleMark(idx)
			p.sel = idx
			fm.mu.Unlock()
			return true
		case mod&sdl.KeymodShift != 0: // Umschalt+Klick: Bereich markieren
			a, b := p.sel, idx
			if a > b {
				a, b = b, a
			}
			if p.marked == nil {
				p.marked = map[string]bool{}
			}
			for i := a; i <= b; i++ {
				if p.entries[i].name != ".." {
					p.marked[p.entries[i].path] = true
				}
			}
			p.sel = idx
			fm.mu.Unlock()
			return true
		}
		p.sel = idx
		fm.mu.Unlock()
		if double {
			fm.enter(p)
		}
		return true
	}
	fm.mu.Unlock()
	return true
}

// onWheel scrollt das fokussierte Panel.
func (fm *fileManager) onWheel(dy float32) {
	fm.mu.Lock()
	defer fm.mu.Unlock()
	p := fm.curPane()
	step := 3
	if dy > 0 {
		p.sel -= step
	} else {
		p.sel += step
	}
	if p.sel < 0 {
		p.sel = 0
	}
	if p.sel > len(p.entries)-1 {
		p.sel = len(p.entries) - 1
	}
}

// --- HTTP-Client für die viewer-files-API ---

const maxViewerTransfer = 4 << 30 // wie Server/Agent; gestreamt

type fmListing struct {
	path    string
	parent  string
	entries []fmEntry
	err     string
}

type fmClient struct {
	http   *http.Client
	xfer   *http.Client // ohne Zeitlimit: große Dateien
	base   string       // https://host
	device string
	token  string
}

func newFMClient(cfg *launchConfig) *fmClient {
	c := &fmClient{http: newHTTPClient(cfg.Insecure), base: httpBase(cfg.URL), device: cfg.Device, token: cfg.Token}
	c.xfer = &http.Client{Transport: c.http.Transport}
	return c
}

func (c *fmClient) url(p string) string {
	return c.base + "/api/v1/devices/" + c.device + "/viewer-files" + p
}

func (c *fmClient) post(path string, body any) (string, error) {
	buf, _ := json.Marshal(body)
	req, _ := http.NewRequest(http.MethodPost, c.url(path), bytes.NewReader(buf))
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("server: %s", resp.Status)
	}
	var r struct {
		CommandID string `json:"command_id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return "", err
	}
	return r.CommandID, nil
}

// poll wartet auf das Ergebnis eines eingereihten Befehls.
func (c *fmClient) poll(id string) (string, int, error) { return c.pollProgress(id, nil) }

// pollProgress wartet, solange der Agent Fortschritt meldet; Abbruch erst nach 60 s Stillstand.
func (c *fmClient) pollProgress(id string, onProgress func(string)) (string, int, error) {
	deadline := time.Now().Add(60 * time.Second)
	last := ""
	for time.Now().Before(deadline) {
		req, _ := http.NewRequest(http.MethodGet, c.url("/command/"+id), nil)
		req.Header.Set("Authorization", "Bearer "+c.token)
		resp, err := c.http.Do(req)
		if err != nil {
			return "", 0, err
		}
		var cmd struct {
			Status   string `json:"status"`
			ExitCode int    `json:"exit_code"`
			Output   string `json:"output"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&cmd)
		resp.Body.Close()
		if cmd.Status == "done" {
			return cmd.Output, cmd.ExitCode, nil
		}
		if cmd.Output != last {
			last = cmd.Output
			deadline = time.Now().Add(60 * time.Second)
			if onProgress != nil && last != "" {
				onProgress(last)
			}
		}
		time.Sleep(400 * time.Millisecond)
	}
	return "", 0, fmt.Errorf("Zeitüberschreitung – Gerät antwortet nicht")
}

func (c *fmClient) browse(path string) fmListing {
	id, err := c.post("/browse", map[string]string{"path": path})
	if err != nil {
		return fmListing{err: err.Error()}
	}
	out, _, err := c.poll(id)
	if err != nil {
		return fmListing{err: err.Error()}
	}
	return parseListing(path, out)
}

// parseListing liest eine Verzeichnisliste im browse_dir-Format.
func parseListing(path, out string) fmListing {
	var raw struct {
		Path    string `json:"path"`
		Parent  string `json:"parent"`
		Error   string `json:"error"`
		Entries []struct {
			Name string `json:"name"`
			Path string `json:"path"`
			Dir  bool   `json:"dir"`
			Size int64  `json:"size"`
		} `json:"entries"`
	}
	if err := json.Unmarshal([]byte(out), &raw); err != nil {
		return fmListing{err: "ungültige Antwort"}
	}
	if raw.Error != "" {
		return fmListing{path: path, err: raw.Error}
	}
	l := fmListing{path: raw.Path, parent: raw.Parent}
	for _, e := range raw.Entries {
		l.entries = append(l.entries, fmEntry{name: e.Name, path: e.Path, dir: e.Dir, size: e.Size})
	}
	return l
}

// xferClient liefert den Client ohne Zeitlimit (Fallback: normaler Client).
func (c *fmClient) xferClient() *http.Client {
	if c.xfer != nil {
		return c.xfer
	}
	return c.http
}

// readTo lädt eine Datei vom Gerät nach local (erst Zwischendatei, dann umbenennen).
func (c *fmClient) readTo(path, local string, progress func(string)) error {
	id, err := c.post("/read", map[string]string{"path": path})
	if err != nil {
		return err
	}
	out, exit, err := c.pollProgress(id, progress)
	if err != nil {
		return err
	}
	if exit != 0 {
		return fmt.Errorf("%s", out)
	}
	req, _ := http.NewRequest(http.MethodGet, c.url("/blob/"+id), nil)
	req.Header.Set("Authorization", "Bearer "+c.token)
	resp, err := c.xferClient().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("download: %s", resp.Status)
	}
	tmp, err := os.CreateTemp(filepath.Dir(local), ".roster-download-*")
	if err != nil {
		return err
	}
	pr := &progressReader{r: resp.Body, total: resp.ContentLength, verb: "Herunterladen", on: progress}
	_, err = io.Copy(tmp, io.LimitReader(pr, maxViewerTransfer+1))
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil && resp.ContentLength >= 0 && pr.n != resp.ContentLength {
		err = fmt.Errorf("unvollständig (%d von %d Bytes)", pr.n, resp.ContentLength)
	}
	if err == nil {
		_ = os.Chmod(tmp.Name(), 0o644)
		err = os.Rename(tmp.Name(), local)
	}
	if err != nil {
		os.Remove(tmp.Name())
	}
	return err
}

// write lädt einen Datenstrom auf das Gerät (über den Server) und wartet auf das Schreiben.
func (c *fmClient) write(path string, body io.Reader, size int64, progress func(string)) error {
	pr := &progressReader{r: body, total: size, verb: "Hochladen zum Server", on: progress}
	req, _ := http.NewRequest(http.MethodPost, c.url("/write")+"?path="+url.QueryEscape(path), pr)
	req.ContentLength = size
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/octet-stream")
	resp, err := c.xferClient().Do(req)
	if err != nil {
		return err
	}
	var r struct {
		CommandID string `json:"command_id"`
		Error     string `json:"error"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&r)
	resp.Body.Close()
	if resp.StatusCode >= 300 || r.CommandID == "" {
		if r.Error != "" {
			return fmt.Errorf("%s", r.Error)
		}
		return fmt.Errorf("upload: %s", resp.Status)
	}
	out, exit, err := c.pollProgress(r.CommandID, progress)
	if err != nil {
		return err
	}
	if exit != 0 {
		return fmt.Errorf("%s", out)
	}
	return nil
}

// progressReader meldet höchstens alle 500 ms den Stand.
type progressReader struct {
	r     io.Reader
	n     int64
	total int64
	verb  string
	on    func(string)
	last  time.Time
}

func (p *progressReader) Read(b []byte) (int, error) {
	n, err := p.r.Read(b)
	p.n += int64(n)
	if p.on != nil && time.Since(p.last) > 500*time.Millisecond {
		p.last = time.Now()
		if p.total > 0 {
			p.on(fmt.Sprintf("%s %d / %d MB (%d %%)", p.verb, p.n>>20, p.total>>20, p.n*100/p.total))
		} else {
			p.on(fmt.Sprintf("%s %d MB", p.verb, p.n>>20))
		}
	}
	return n, err
}

func (c *fmClient) mkdir(path string) error {
	id, err := c.post("/mkdir", map[string]string{"path": path})
	if err != nil {
		return err
	}
	out, exit, err := c.poll(id)
	if err != nil {
		return err
	}
	if exit != 0 {
		return fmt.Errorf("%s", out)
	}
	return nil
}

func (c *fmClient) delete(path string) error {
	id, err := c.post("/delete", map[string]string{"path": path})
	if err != nil {
		return err
	}
	out, exit, err := c.poll(id)
	if err != nil {
		return err
	}
	if exit != 0 {
		return fmt.Errorf("%s", out)
	}
	return nil
}

// --- lokales Dateisystem ---

func localBrowse(path string) fmListing {
	if path == "" {
		path = "/"
	}
	parent := filepath.Dir(path)
	if parent == path {
		parent = ""
	}
	des, err := os.ReadDir(path)
	if err != nil {
		return fmListing{path: path, parent: parent, err: err.Error()}
	}
	l := fmListing{path: path, parent: parent}
	for _, d := range des {
		info, ierr := d.Info()
		fe := fmEntry{name: d.Name(), path: filepath.Join(path, d.Name()), dir: d.IsDir()}
		if ierr == nil {
			fe.size = info.Size()
		}
		l.entries = append(l.entries, fe)
	}
	sort.Slice(l.entries, func(i, j int) bool {
		if l.entries[i].dir != l.entries[j].dir {
			return l.entries[i].dir
		}
		return strings.ToLower(l.entries[i].name) < strings.ToLower(l.entries[j].name)
	})
	return l
}

// --- Helfer ---

// remoteJoin verbindet ein Geräte-Verzeichnis mit einem Namen unter Beibehaltung des
// erkannten Trennzeichens (Windows „\", sonst „/").
func remoteJoin(dir, name string) string {
	if dir == "" {
		return name
	}
	sep := "/"
	if strings.Contains(dir, "\\") || (len(dir) >= 2 && dir[1] == ':') {
		sep = "\\"
	}
	return strings.TrimRight(dir, "/\\") + sep + name
}

func paneLabel(p *fmPane) string {
	if p.remote {
		return "Gerät"
	}
	return "lokal"
}

func newHTTPClient(insecure bool) *http.Client {
	c := &http.Client{Timeout: 90 * time.Second}
	if insecure {
		c.Transport = &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}
	}
	return c
}

// httpBase liefert die http(s)-Basis-URL aus der (evtl. ws/wss-)Server-URL.
func httpBase(u string) string {
	switch {
	case strings.HasPrefix(u, "wss://"):
		return "https://" + strings.TrimPrefix(u, "wss://")
	case strings.HasPrefix(u, "ws://"):
		return "http://" + strings.TrimPrefix(u, "ws://")
	case strings.HasPrefix(u, "http://"), strings.HasPrefix(u, "https://"):
		return u
	default:
		return "https://" + u
	}
}

func humanSize(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1fG", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.1fM", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%dK", n/(1<<10))
	default:
		return fmt.Sprintf("%dB", n)
	}
}
