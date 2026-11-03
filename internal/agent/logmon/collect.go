package logmon

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/boonkerz/roster/internal/shared"
)

const (
	maxEvents   = 500             // Ereignisse je Lauf (Rest beim nächsten)
	maxReadSize = 8 * 1024 * 1024 // Bytes je Datei und Lauf (bei sehr großen Zuwächsen wird nachgeholt)
)

// fileCursor merkt sich je Datei Inode und Offset (bzw. je Container die letzte Zeit bei
// `docker logs`-Fallback).
type fileCursor struct {
	Inode  uint64 `json:"inode,omitempty"`
	Offset int64  `json:"offset,omitempty"`
	Since  string `json:"since,omitempty"` // RFC3339 (docker logs)
	Seen   string `json:"seen,omitempty"`  // zuletzt benutzt (RFC3339); alte Einträge verfallen
}

type cursors map[string]*fileCursor

func cursorPath(stateDir string) string { return filepath.Join(stateDir, "logmon-cursor.json") }

func loadCursors(stateDir string) cursors {
	c := cursors{}
	if b, err := os.ReadFile(cursorPath(stateDir)); err == nil {
		_ = json.Unmarshal(b, &c)
	}
	return c
}

func saveCursors(stateDir string, c cursors) {
	cutoff := time.Now().Add(-30 * 24 * time.Hour)
	for k, v := range c {
		if t, err := time.Parse(time.RFC3339, v.Seen); err == nil && t.Before(cutoff) {
			delete(c, k)
		}
	}
	b, err := json.Marshal(c)
	if err != nil {
		return
	}
	tmp := cursorPath(stateDir) + ".tmp"
	if os.WriteFile(tmp, b, 0o600) == nil {
		_ = os.Rename(tmp, cursorPath(stateDir))
	}
}

// Collect liest alle Quellen inkrementell und liefert neue Ereignisse (chronologisch je
// Quelle) sowie je Quelle den Lesestatus dieses Laufs (Events = Ereignisse dieses Laufs).
func Collect(ctx context.Context, stateDir string, sources []shared.LogSource) ([]shared.AuthEvent, []shared.LogSourceStatus) {
	if len(sources) == 0 {
		return nil, nil
	}
	cur := loadCursors(stateDir)
	now := time.Now()
	var out []shared.AuthEvent
	var status []shared.LogSourceStatus
	budget := maxEvents
	for _, src := range sources {
		st := shared.LogSourceStatus{ID: src.ID, Name: src.Name, Type: src.Type, Target: src.Target, LastRun: now}
		if budget <= 0 {
			st.Error = "Ereignis-Budget dieses Laufs erschöpft, Rest folgt"
			status = append(status, st)
			continue
		}
		var ev []shared.AuthEvent
		switch src.Type {
		case "file":
			ev, st.Targets, st.Error = collectFiles(src, cur, now, budget)
		case "docker":
			ev, st.Targets, st.Error = collectDocker(ctx, src, cur, now, budget)
		case "journal":
			ev, st.Targets, st.Error = collectJournal(ctx, src, cur, now, budget)
		case "docker_events":
			ev, st.Targets, st.Error = collectDockerEvents(ctx, src, cur, now, budget)
		default:
			st.Error = "unbekannter Typ " + src.Type
		}
		st.Events = len(ev)
		out = append(out, ev...)
		budget -= len(ev)
		status = append(status, st)
	}
	saveCursors(stateDir, cur)
	return out, status
}

// collectFiles liest alle zum Glob passenden Dateien und meldet, wie viele gefunden wurden.
func collectFiles(src shared.LogSource, cur cursors, now time.Time, budget int) ([]shared.AuthEvent, int, string) {
	paths := globPaths(src.Target)
	if len(paths) == 0 {
		return nil, 0, "keine Datei passt auf " + src.Target
	}
	var out []shared.AuthEvent
	var errs []string
	ok := 0
	for _, p := range paths {
		if budget-len(out) <= 0 {
			break
		}
		ev, err := readFile(src, p, "f:"+src.ID+":"+p, cur, now, budget-len(out))
		if err != nil {
			errs = append(errs, err.Error())
			continue
		}
		ok++
		out = append(out, ev...)
	}
	return out, ok, strings.Join(errs, "; ")
}

// globPaths löst ein Glob-Muster auf (ohne Muster: der Pfad selbst, falls vorhanden).
func globPaths(target string) []string {
	if strings.ContainsAny(target, "*?[") {
		m, _ := filepath.Glob(target)
		sort.Strings(m)
		return m
	}
	if _, err := os.Stat(target); err != nil {
		return nil
	}
	return []string{target}
}

// readFile liest neue Zeilen ab dem Cursor; Rotation (Inode anders/Datei kürzer) beginnt
// vorn, der allererste Lauf springt ans Ende (kein Altbestand).
func readFile(src shared.LogSource, p, key string, cur cursors, now time.Time, budget int) ([]shared.AuthEvent, error) {
	f, err := os.Open(p)
	if err != nil {
		return nil, fmt.Errorf("%s: %v", p, shortErr(err))
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("%s: %v", p, shortErr(err))
	}
	if fi.IsDir() {
		return nil, fmt.Errorf("%s ist ein Ordner", p)
	}
	inode := inodeOf(fi)
	c := cur[key]
	first := c == nil
	if first {
		c = &fileCursor{}
		cur[key] = c
	}
	c.Seen = now.Format(time.RFC3339)
	offset := c.Offset
	switch {
	case first:
		offset = fi.Size()
	case c.Inode != inode || offset > fi.Size():
		offset = 0
	}
	if offset >= fi.Size() {
		c.Inode, c.Offset = inode, offset
		return nil, nil
	}
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return nil, err
	}
	r := bufio.NewReaderSize(io.LimitReader(f, maxReadSize), 256*1024)
	var out []shared.AuthEvent
	pos := offset
	for len(out) < budget {
		line, err := r.ReadString('\n')
		if err != nil {
			break // unvollständige letzte Zeile beim nächsten Lauf
		}
		pos += int64(len(line))
		if ev, ok := Parse(src, Unwrap(line), now); ok {
			ev.RecordID = fmt.Sprintf("l:%s:%d:%d", short(key), inode, pos)
			out = append(out, ev)
		}
	}
	c.Inode, c.Offset = inode, pos
	return out, nil
}

// shortErr kürzt os-Fehler auf den Kern („permission denied“ statt „open /x: permission denied“).
func shortErr(err error) error {
	if pe, ok := err.(*os.PathError); ok {
		return pe.Err
	}
	return err
}

func short(s string) string {
	if len(s) > 24 {
		return s[len(s)-24:]
	}
	return s
}

// --- Docker ---

type container struct{ ID, Name, LogPath string }

func dockerOut(ctx context.Context, args ...string) (string, error) {
	cctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	out, err := exec.CommandContext(cctx, "docker", args...).Output()
	if err != nil {
		// Fehlertext des Docker-CLI mitgeben („permission denied while trying to connect …“).
		if ee, ok := err.(*exec.ExitError); ok {
			if msg := strings.TrimSpace(string(ee.Stderr)); msg != "" {
				if i := strings.IndexByte(msg, '\n'); i > 0 {
					msg = msg[:i]
				}
				return string(out), errors.New(msg)
			}
		}
	}
	return string(out), err
}

// matchName prüft Container-Namen gegen den exakten Namen oder ein Glob-Muster
// (Groß-/Kleinschreibung egal); "nginx-proxy" trifft also nicht "nginx-proxy-le".
func matchName(pattern, name string) bool {
	p, n := strings.ToLower(strings.TrimSpace(pattern)), strings.ToLower(name)
	if p == "" {
		return false
	}
	if strings.ContainsAny(p, "*?[") {
		ok, _ := path.Match(p, n)
		return ok
	}
	return n == p
}

// containers listet laufende Container, deren Name zum Muster passt, mit Log-Pfad; dazu
// die Namen aller laufenden Container (für Hinweise, wenn nichts passt).
func containers(ctx context.Context, pattern string) ([]container, []string, error) {
	if _, err := exec.LookPath("docker"); err != nil {
		return nil, nil, errors.New("docker nicht gefunden")
	}
	out, err := dockerOut(ctx, "ps", "--no-trunc", "--format", "{{.ID}}\t{{.Names}}")
	if err != nil {
		return nil, nil, fmt.Errorf("docker ps: %v", err)
	}
	var ids, names []string
	byID := map[string]*container{}
	for _, line := range strings.Split(out, "\n") {
		parts := strings.SplitN(strings.TrimSpace(line), "\t", 2)
		if len(parts) != 2 {
			continue
		}
		names = append(names, parts[1])
		if !matchName(pattern, parts[1]) {
			continue
		}
		ids = append(ids, parts[0])
		byID[parts[0]] = &container{ID: parts[0], Name: parts[1]}
	}
	sort.Strings(names)
	if len(ids) == 0 {
		return nil, names, nil
	}
	args := append([]string{"inspect", "-f", "{{.Id}}\t{{.LogPath}}"}, ids...)
	if out, err := dockerOut(ctx, args...); err == nil {
		for _, line := range strings.Split(out, "\n") {
			parts := strings.SplitN(strings.TrimSpace(line), "\t", 2)
			if len(parts) == 2 {
				if c := byID[parts[0]]; c != nil {
					c.LogPath = parts[1]
				}
			}
		}
	}
	var list []container
	for _, id := range ids {
		list = append(list, *byID[id])
	}
	return list, names, nil
}

// collectDocker liest die json-file-Protokolle passender Container direkt (Offset-Cursor);
// ohne lesbaren Log-Pfad (anderer Log-Treiber) über `docker logs --since`.
func collectDocker(ctx context.Context, src shared.LogSource, cur cursors, now time.Time, budget int) ([]shared.AuthEvent, int, string) {
	list, _, err := containers(ctx, src.Target)
	if err != nil {
		return nil, 0, err.Error()
	}
	if len(list) == 0 {
		return nil, 0, "kein laufender Container passt auf " + src.Target
	}
	var out []shared.AuthEvent
	var errs []string
	for _, c := range list {
		if budget-len(out) <= 0 {
			break
		}
		key := "d:" + src.ID + ":" + c.ID
		if c.LogPath != "" {
			if _, err := os.Stat(c.LogPath); err == nil {
				ev, err := readFile(src, c.LogPath, key, cur, now, budget-len(out))
				if err != nil {
					errs = append(errs, c.Name+": "+err.Error())
				}
				out = append(out, ev...)
				continue
			}
		}
		out = append(out, dockerLogs(ctx, src, c, key, cur, now, budget-len(out))...)
	}
	return out, len(list), strings.Join(errs, "; ")
}

func dockerLogs(ctx context.Context, src shared.LogSource, c container, key string, cur cursors, now time.Time, budget int) []shared.AuthEvent {
	cs := cur[key]
	if cs == nil {
		// Erster Lauf: nur Neues ab jetzt.
		cur[key] = &fileCursor{Since: now.Format(time.RFC3339Nano), Seen: now.Format(time.RFC3339)}
		return nil
	}
	cs.Seen = now.Format(time.RFC3339)
	out, err := dockerOut(ctx, "logs", "--timestamps", "--since", cs.Since, c.ID)
	if err != nil {
		return nil
	}
	var events []shared.AuthEvent
	last := cs.Since
	sinceT, _ := time.Parse(time.RFC3339Nano, cs.Since)
	for _, raw := range strings.Split(out, "\n") {
		if raw == "" {
			continue
		}
		l := Unwrap(raw)
		if !l.Time.IsZero() {
			if !l.Time.After(sinceT) {
				continue // --since ist inklusiv
			}
			last = l.Time.Format(time.RFC3339Nano)
		}
		if ev, ok := Parse(src, l, now); ok {
			ev.RecordID = "dl:" + short(key) + ":" + l.Time.Format(time.RFC3339Nano)
			events = append(events, ev)
			if len(events) >= budget {
				break
			}
		}
	}
	cs.Since = last
	return events
}
