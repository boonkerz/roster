// Package fim ist die Datei-Integritätsüberwachung des Agents: Es hält je Gerät eine
// Baseline (Größe, Rechte, Besitzer, Änderungszeit, SHA-256) der überwachten Pfade und
// meldet Abweichungen als Ereignisse (neu / geändert / gelöscht / Attribute).
package fim

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/boonkerz/roster/internal/shared"
)

const (
	maxEntries  = 50000     // Obergrenze je Scan (danach Ereignis „limit“)
	maxHashSize = 256 << 20 // größere Dateien: nur Größe/Zeit/Rechte
)

// Entry ist der gemerkte Zustand einer Datei oder eines Verzeichnisses.
type Entry struct {
	RuleID  string `json:"r"`
	Size    int64  `json:"s"`
	Mode    string `json:"m"`           // z. B. "-rw-r--r--"
	Owner   string `json:"o,omitempty"` // user:group (Unix)
	ModTime int64  `json:"t"`           // Unix-Nanosekunden
	Hash    string `json:"h,omitempty"` // SHA-256 hex (Dateien ≤ maxHashSize)
	Dir     bool   `json:"d,omitempty"`
}

// Baseline ist der persistierte Zustand aller überwachten Pfade.
type Baseline struct {
	Created time.Time        `json:"created"`
	Scanned time.Time        `json:"scanned"`
	Entries map[string]Entry `json:"entries"`
}

// Load liest die Baseline; fehlende Datei → leere Baseline (nil-Map = noch nie gescannt).
func Load(path string) *Baseline {
	b, err := os.ReadFile(path)
	if err != nil {
		return &Baseline{}
	}
	var bl Baseline
	if err := json.Unmarshal(b, &bl); err != nil {
		return &Baseline{}
	}
	return &bl
}

// Save schreibt die Baseline atomar (tmp + rename).
func Save(path string, bl *Baseline) error {
	b, err := json.Marshal(bl)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Stats fasst einen Scan zusammen.
type Stats struct {
	Files, Hashed, Errors int
	Limit                 bool
}

// Scan vergleicht den aktuellen Zustand der Regeln mit prev und liefert die neue
// Baseline samt Ereignissen. Ist prev leer (erster Lauf), entsteht nur die Baseline und
// ein einzelnes Ereignis „baseline“.
func Scan(ctx context.Context, rules []shared.FIMRule, prev *Baseline, now time.Time) (*Baseline, []shared.FIMEvent, Stats) {
	var st Stats
	next := &Baseline{Created: prev.Created, Scanned: now, Entries: map[string]Entry{}}
	first := prev.Entries == nil
	if first {
		next.Created = now
	}
	active := map[string]shared.FIMRule{}
	for _, r := range rules {
		if !platformMatches(r.Platforms) {
			continue
		}
		active[r.ID] = r
	}
	var events []shared.FIMEvent
	ownerCache := map[string]string{}
	visit := func(r shared.FIMRule, p string, info fs.FileInfo) bool {
		if len(next.Entries) >= maxEntries {
			st.Limit = true
			return false
		}
		e := Entry{RuleID: r.ID, Size: info.Size(), Mode: info.Mode().String(), ModTime: info.ModTime().UnixNano(), Dir: info.IsDir()}
		e.Owner = ownerOf(info, ownerCache)
		if !e.Dir {
			st.Files++
			old, had := prev.Entries[p]
			if rv, ok := info.Sys().(regValue); ok {
				e.Hash = rv.hash // Registry-Wert: Hash kommt vom Walker (Typ + Daten)
			} else if had && old.Size == e.Size && old.ModTime == e.ModTime && old.Hash != "" {
				e.Hash = old.Hash // unverändert laut Metadaten: Hash übernehmen
			} else if e.Size <= maxHashSize {
				if h, err := hashFile(p); err == nil {
					e.Hash = h
					st.Hashed++
				} else {
					st.Errors++
				}
			}
		}
		next.Entries[p] = e
		return true
	}
	for _, r := range active {
		if ctx.Err() != nil {
			break
		}
		walkRule(r, visit)
	}
	if first {
		events = append(events, shared.FIMEvent{Time: now, Change: "baseline", Path: "", Size: int64(st.Files)})
		if st.Limit {
			events = append(events, shared.FIMEvent{Time: now, Change: "limit", Size: maxEntries})
		}
		return next, events, st
	}
	// Diff: neu / geändert / Attribute
	paths := make([]string, 0, len(next.Entries))
	for p := range next.Entries {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, p := range paths {
		cur := next.Entries[p]
		r := active[cur.RuleID]
		old, had := prev.Entries[p]
		switch {
		case !had:
			if cur.Dir && !shared.IsRegistryPath(p) {
				continue // neue Verzeichnisse nicht einzeln melden (ihre Dateien schon); neue Registry-Schlüssel schon
			}
			events = append(events, shared.FIMEvent{Time: now, RuleID: cur.RuleID, Path: p, Change: "created",
				NewHash: cur.Hash, NewMode: cur.Mode, NewOwner: cur.Owner, Size: cur.Size, Alert: r.Alert})
		case !cur.Dir && ((cur.Hash != "" && old.Hash != "" && cur.Hash != old.Hash) || (cur.Hash == "" && (cur.Size != old.Size || cur.ModTime != old.ModTime))):
			events = append(events, shared.FIMEvent{Time: now, RuleID: cur.RuleID, Path: p, Change: "modified",
				OldHash: old.Hash, NewHash: cur.Hash, OldMode: old.Mode, NewMode: cur.Mode, OldOwner: old.Owner, NewOwner: cur.Owner, Size: cur.Size, Alert: r.Alert})
		case cur.Mode != old.Mode || cur.Owner != old.Owner:
			events = append(events, shared.FIMEvent{Time: now, RuleID: cur.RuleID, Path: p, Change: "attrs",
				OldMode: old.Mode, NewMode: cur.Mode, OldOwner: old.Owner, NewOwner: cur.Owner, Size: cur.Size, Alert: r.Alert})
		}
	}
	// Gelöscht: war in der Baseline, Regel existiert noch, Pfad fehlt jetzt.
	var gone []string
	for p := range prev.Entries {
		if _, ok := next.Entries[p]; !ok {
			gone = append(gone, p)
		}
	}
	sort.Strings(gone)
	for _, p := range gone {
		old := prev.Entries[p]
		r, ruleStillActive := active[old.RuleID]
		if !ruleStillActive || (old.Dir && !shared.IsRegistryPath(p)) {
			continue // Regel entfernt/anderes System: still verwerfen; Verzeichnisse nicht melden (Registry-Schlüssel schon)
		}
		events = append(events, shared.FIMEvent{Time: now, RuleID: old.RuleID, Path: p, Change: "deleted",
			OldHash: old.Hash, OldMode: old.Mode, OldOwner: old.Owner, Size: old.Size, Alert: r.Alert})
	}
	if st.Limit {
		events = append(events, shared.FIMEvent{Time: now, Change: "limit", Size: maxEntries})
	}
	return next, events, st
}

// walkRule besucht alle Pfade einer Regel (Glob im Pfad erlaubt, Symlinks werden nicht
// verfolgt, Sockets/Gerätedateien übersprungen).
func walkRule(r shared.FIMRule, visit func(r shared.FIMRule, p string, info fs.FileInfo) bool) {
	if shared.IsRegistryPath(r.Path) {
		walkRegistry(r, visit)
		return
	}
	roots := []string{r.Path}
	if strings.ContainsAny(r.Path, "*?[") {
		roots, _ = filepath.Glob(r.Path)
	}
	for _, root := range roots {
		info, err := os.Lstat(root)
		if err != nil || info.Mode()&os.ModeSymlink != 0 {
			continue
		}
		if !info.IsDir() {
			if info.Mode().IsRegular() && !excluded(r, root) {
				visit(r, root, info)
			}
			continue
		}
		if !r.Recursive {
			entries, err := os.ReadDir(root)
			if err != nil {
				continue
			}
			visit(r, root, info)
			for _, de := range entries {
				if de.Type()&os.ModeSymlink != 0 || !de.Type().IsRegular() {
					continue
				}
				p := filepath.Join(root, de.Name())
				fi, err := de.Info()
				if err != nil || excluded(r, p) {
					continue
				}
				if !visit(r, p, fi) {
					return
				}
			}
			continue
		}
		stop := false
		_ = filepath.WalkDir(root, func(p string, de fs.DirEntry, err error) error {
			if err != nil || stop {
				if de != nil && de.IsDir() && err != nil {
					return fs.SkipDir
				}
				return nil
			}
			if de.Type()&os.ModeSymlink != 0 {
				return nil
			}
			if excluded(r, p) {
				if de.IsDir() {
					return fs.SkipDir
				}
				return nil
			}
			if !de.IsDir() && !de.Type().IsRegular() {
				return nil
			}
			fi, err := de.Info()
			if err != nil {
				return nil
			}
			if !visit(r, p, fi) {
				stop = true
				return fs.SkipAll
			}
			return nil
		})
	}
}

// excluded prüft die Ausschluss-Muster einer Regel gegen Pfad (Slash-Form) und Dateiname.
func excluded(r shared.FIMRule, p string) bool {
	if len(r.Exclude) == 0 {
		return false
	}
	slash := filepath.ToSlash(p)
	base := path.Base(slash)
	for _, pat := range r.Exclude {
		pat = strings.TrimSpace(pat)
		if pat == "" {
			continue
		}
		sp := filepath.ToSlash(pat)
		if ok, _ := path.Match(sp, slash); ok {
			return true
		}
		if ok, _ := path.Match(sp, base); ok {
			return true
		}
		if strings.HasSuffix(sp, "/") && strings.HasPrefix(slash+"/", sp) {
			return true // absolutes Verzeichnis-Präfix ("/var/cache/")
		}
		trimmed := strings.TrimSuffix(sp, "/")
		if !strings.Contains(trimmed, "/") {
			// Muster ohne Pfadtrenner: trifft jedes Segment (Verzeichnis- oder Dateiname).
			for _, seg := range strings.Split(slash, "/") {
				if ok, _ := path.Match(trimmed, seg); ok && seg != "" {
					return true
				}
			}
		} else if !strings.ContainsAny(trimmed, "*?[") && strings.HasPrefix(slash, trimmed+"/") {
			return true // Verzeichnis-Präfix ohne Glob
		}
	}
	return false
}

func platformMatches(platforms []string) bool {
	if len(platforms) == 0 {
		return true
	}
	for _, p := range platforms {
		if strings.EqualFold(strings.TrimSpace(p), runtime.GOOS) {
			return true
		}
	}
	return false
}

func hashFile(p string) (string, error) {
	f, err := os.Open(p)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
