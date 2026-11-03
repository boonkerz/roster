// Package statemon überwacht den Systemzustand: lauschende Ports, Kernelmodule, Cron-Einträge,
// UID-0-/Administratorkonten, Autostart-Dienste und geplante Aufgaben. Je Kategorie wird
// eine sortierte Liste von Einträgen aufgenommen, mit der Baseline verglichen und jede
// Änderung als AuthEvent „state_change“ (Service = Kategorie) gemeldet.
package statemon

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/boonkerz/roster/internal/shared"
)

// Snapshot ist der Zustand je Kategorie (sortierte, eindeutige Einträge).
type Snapshot map[string][]string

// Baseline ist der gespeicherte Zustand.
type Baseline struct {
	Taken time.Time `json:"taken"`
	Items Snapshot  `json:"items"`
}

// Load liest die Baseline (nil bei Fehlen).
func Load(path string) *Baseline {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var bl Baseline
	if json.Unmarshal(b, &bl) != nil || bl.Items == nil {
		return nil
	}
	return &bl
}

// Save schreibt die Baseline atomar.
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

// Path ist der Dateiname der Baseline im Zustandsverzeichnis.
func Path(stateDir string) string { return filepath.Join(stateDir, "state-baseline.json") }

// Take nimmt den aktuellen Zustand aller Kategorien der Plattform auf.
func Take(ctx context.Context) Snapshot {
	snap := Snapshot{}
	for cat, fn := range collectors() {
		items := fn(ctx)
		snap[cat] = uniqSorted(items)
	}
	return snap
}

func uniqSorted(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

// maxDetail begrenzt die Länge des Detailtexts eines Ereignisses.
const maxDetail = 480

// Diff vergleicht zwei Zustände und liefert je geänderter Kategorie ein Ereignis
// ("+ neu, - weg"). Kategorien, die nur in einem Zustand vorkommen, werden übersprungen
// (Plattformwechsel/neue Version ohne Fehlalarm).
func Diff(prev, cur Snapshot, now time.Time) []shared.AuthEvent {
	var cats []string
	for c := range cur {
		if _, ok := prev[c]; ok {
			cats = append(cats, c)
		}
	}
	sort.Strings(cats)
	var out []shared.AuthEvent
	for _, c := range cats {
		was := map[string]bool{}
		for _, s := range prev[c] {
			was[s] = true
		}
		is := map[string]bool{}
		for _, s := range cur[c] {
			is[s] = true
		}
		var parts []string
		for _, s := range cur[c] {
			if !was[s] {
				parts = append(parts, "+ "+s)
			}
		}
		for _, s := range prev[c] {
			if !is[s] {
				parts = append(parts, "- "+s)
			}
		}
		if len(parts) == 0 {
			continue
		}
		detail := strings.Join(parts, ", ")
		if len(detail) > maxDetail {
			detail = detail[:maxDetail] + "…"
		}
		h := sha256.Sum256([]byte(c + "|" + detail + "|" + now.Format(time.RFC3339)))
		out = append(out, shared.AuthEvent{Time: now, Kind: "state_change", Service: c, Detail: detail, RecordID: "s:" + hex.EncodeToString(h[:6])})
	}
	return out
}
