// Package flow führt Abläufe aus dem Flow-Designer auf dem Agent aus: ein Graph aus
// Bausteinen (Dateien finden, Text ersetzen, Docker Compose, Befehl, HTTP, Prüfen,
// Zurückrollen, Warten), verbunden über die Ausgänge ok/fail. Im Trockenlauf meldet jeder
// Baustein, was er täte – Dateien werden nicht geändert, Befehle nicht ausgeführt.
package flow

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/boonkerz/roster/internal/shared"
)

const (
	maxRuntime   = 30 * time.Minute
	maxOutput    = 8 << 10
	maxDiff      = 20 << 10
	maxResult    = 256 << 10
	maxSteps     = 200
	maxReplFiles = 200
)

// Options steuern einen Lauf.
type Options struct {
	RunID  string
	DryRun bool
	Params map[string]string
	// RunScript führt ein Skript aus (policy.RunScriptEnv); per Feld injiziert, damit das
	// Paket ohne Abhängigkeit zu policy testbar bleibt.
	RunScript func(ctx context.Context, shell, script string) (int, string)
}

// composeProject ist ein von find_files (Modus compose_image) gefundenes Compose-Projekt.
type composeProject struct {
	Project  string   `json:"project"`
	Dir      string   `json:"dir"`
	Files    []string `json:"files"`
	Services []string `json:"services"`
	Images   []string `json:"images,omitempty"` // passende Image-Referenzen (für die Inhaltsprüfung)
}

// step sind die Variablen eines ausgeführten Knotens.
type step struct {
	Status   string
	Out      string
	JSON     any
	Value    string
	Files    []string
	Projects []composeProject
}

type backup struct{ Path, Backup string }

type runner struct {
	g       shared.FlowGraph
	opt     Options
	steps   map[string]*step
	backups []backup
	last    *step // letzter Schritt mit Dateien/Projekten (Standardquelle)
}

// Run führt den Graphen aus und liefert das Ergebnis.
func Run(ctx context.Context, g shared.FlowGraph, opt Options) shared.FlowResult {
	if err := shared.ValidateFlow(g); err != nil {
		return shared.FlowResult{Status: "fail", Error: err.Error()}
	}
	ctx, cancel := context.WithTimeout(ctx, maxRuntime)
	defer cancel()
	r := &runner{g: g, opt: opt, steps: map[string]*step{}}
	if r.opt.Params == nil {
		r.opt.Params = map[string]string{}
	}
	byID := map[string]shared.FlowNode{}
	for _, n := range g.Nodes {
		byID[n.ID] = n
	}
	cur, _ := shared.FlowStart(g)
	var res shared.FlowResult
	hadFail, rolledBack := false, false
	for i := 0; cur != "" && i < maxSteps; i++ {
		n := byID[cur]
		start := time.Now()
		nr, st := r.exec(ctx, n)
		nr.ID, nr.Type, nr.Label = n.ID, n.Type, n.Label
		nr.DurationMs = time.Since(start).Milliseconds()
		nr.Output = clip(nr.Output, maxOutput)
		nr.Diff = clip(nr.Diff, maxDiff)
		res.Nodes = append(res.Nodes, nr)
		st.Status = nr.Status
		r.steps[n.ID] = st
		if len(st.Files) > 0 || len(st.Projects) > 0 {
			r.last = st
		}
		if n.Type == "rollback" && !opt.DryRun {
			rolledBack = true
		}
		switch nr.Status {
		case "empty":
			res.Status = "nothing"
			res.RolledBack = nil
			return finish(res)
		case "fail":
			hadFail = true
			nxt := shared.FlowEdgeTo(g, n.ID, "fail")
			if nxt == "" { // nicht behandelt: abbrechen und automatisch zurückrollen
				res.Error = fmt.Sprintf("Baustein %q fehlgeschlagen", labelOf(n))
				if restored := r.rollback(); len(restored) > 0 {
					res.RolledBack = restored
					res.Status = "rolled_back"
				} else {
					res.Status = "fail"
				}
				return finish(res)
			}
			cur = nxt
		default:
			cur = shared.FlowEdgeTo(g, n.ID, "ok")
		}
		if ctx.Err() != nil {
			res.Error = "Zeitlimit des Ablaufs überschritten"
			res.RolledBack = r.rollback()
			res.Status = "fail"
			return finish(res)
		}
	}
	switch {
	case opt.DryRun:
		res.Status = "dry"
	case rolledBack:
		res.Status = "rolled_back"
	case hadFail:
		res.Status = "fail"
	default:
		res.Status = "ok"
	}
	return finish(res)
}

// finish kürzt das Ergebnis auf eine übertragbare Größe.
func finish(res shared.FlowResult) shared.FlowResult {
	for {
		b, _ := json.Marshal(res)
		if len(b) <= maxResult {
			return res
		}
		cut := false
		for i := range res.Nodes {
			if len(res.Nodes[i].Diff) > 2000 {
				res.Nodes[i].Diff = res.Nodes[i].Diff[:2000] + "\n… (gekürzt)"
				cut = true
			} else if len(res.Nodes[i].Output) > 2000 {
				res.Nodes[i].Output = res.Nodes[i].Output[:2000] + "\n… (gekürzt)"
				cut = true
			}
		}
		if !cut {
			return res
		}
	}
}

func labelOf(n shared.FlowNode) string {
	if n.Label != "" {
		return n.Label
	}
	return n.ID
}

func (r *runner) exec(ctx context.Context, n shared.FlowNode) (shared.NodeResult, *step) {
	cfg := r.config(n)
	switch n.Type {
	case "find_files":
		return r.findFiles(ctx, cfg)
	case "replace":
		return r.replace(cfg)
	case "compose":
		return r.compose(ctx, cfg)
	case "command":
		return r.command(ctx, cfg)
	case "http":
		return r.httpNode(ctx, cfg)
	case "check":
		return r.check(ctx, cfg)
	case "rollback":
		return r.rollbackNode(ctx, cfg)
	case "wait":
		return r.wait(ctx, cfg)
	case "read_value":
		return r.readValue(cfg)
	case "yaml":
		return r.yamlEdit(cfg)
	}
	return shared.NodeResult{Status: "fail", Output: "unbekannter Baustein"}, &step{}
}

func (r *runner) hasBackup(path string) bool {
	for _, b := range r.backups {
		if b.Path == path {
			return true
		}
	}
	return false
}

// rollback stellt alle Backups dieses Laufs wieder her (neueste zuerst).
func (r *runner) rollback() []string {
	if r.opt.DryRun {
		return nil
	}
	var restored []string
	for i := len(r.backups) - 1; i >= 0; i-- {
		b := r.backups[i]
		data, err := os.ReadFile(b.Backup)
		if err != nil {
			continue
		}
		mode := os.FileMode(0o644)
		if fi, err := os.Stat(b.Path); err == nil {
			mode = fi.Mode().Perm()
		}
		if err := writeAtomic(b.Path, data, mode); err == nil {
			restored = append(restored, b.Path)
		}
	}
	r.backups = nil
	return restored
}

// --- Konfiguration und Platzhalter ---

type config map[string]any

func (c config) str(k string) string {
	switch v := c[k].(type) {
	case string:
		return v
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(v)
	}
	return ""
}

func (c config) num(k string, def float64) float64 {
	switch v := c[k].(type) {
	case float64:
		return v
	case string:
		if f, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil {
			return f
		}
	}
	return def
}

func (c config) bool(k string) bool {
	switch v := c[k].(type) {
	case bool:
		return v
	case string:
		return v == "true" || v == "1" || v == "yes"
	}
	return false
}

// config setzt Platzhalter in alle String-Werte des Knotens ein.
func (r *runner) config(n shared.FlowNode) config {
	out := config{}
	for k, v := range n.Config {
		if s, ok := v.(string); ok {
			out[k] = r.expand(s)
		} else {
			out[k] = v
		}
	}
	return out
}

var varRe = regexp.MustCompile(`\{\{\s*(param|steps)\.([A-Za-z0-9_.\-]+)\s*\}\}`)

// expand ersetzt {{param.X}}, {{steps.ID.out|value|status|files}}, {{steps.ID.json.a.b.0}}.
func (r *runner) expand(s string) string {
	return varRe.ReplaceAllStringFunc(s, func(m string) string {
		sub := varRe.FindStringSubmatch(m)
		if sub[1] == "param" {
			return r.opt.Params[sub[2]]
		}
		parts := strings.SplitN(sub[2], ".", 3)
		st := r.steps[parts[0]]
		if st == nil || len(parts) < 2 {
			return ""
		}
		switch parts[1] {
		case "out":
			return strings.TrimSpace(st.Out)
		case "value":
			return st.Value
		case "status":
			return st.Status
		case "files":
			return strings.Join(st.Files, "\n")
		case "json":
			if len(parts) < 3 {
				b, _ := json.Marshal(st.JSON)
				return string(b)
			}
			return jsonString(jsonPath(st.JSON, parts[2]))
		}
		return ""
	})
}

// jsonPath folgt einem Pfad wie "data.items.0.version".
func jsonPath(v any, path string) any {
	if strings.TrimSpace(path) == "" {
		return v
	}
	for _, p := range strings.Split(path, ".") {
		switch t := v.(type) {
		case map[string]any:
			v = t[p]
		case []any:
			i, err := strconv.Atoi(p)
			if err != nil || i < 0 || i >= len(t) {
				return nil
			}
			v = t[i]
		default:
			return nil
		}
	}
	return v
}

func jsonString(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(t)
	}
	b, _ := json.Marshal(v)
	return string(b)
}

func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "\n… (gekürzt)"
}

func lines(s string) []string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			out = append(out, l)
		}
	}
	return out
}
