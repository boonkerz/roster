package flow

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/boonkerz/roster/internal/shared"
	"github.com/boonkerz/roster/internal/shared/yamledit"
)

// Dünne Hüllen um das gemeinsame Paket yamledit (auch die Editor-Vorschau rechnet damit).
func splitPath(p string) ([]string, error)   { return yamledit.SplitPath(p) }
func expandBraces(p string) []string         { return yamledit.ExpandBraces(p) }
func parseYAML(b []byte) (*yaml.Node, error) { return yamledit.Parse(b) }
func lookup(root *yaml.Node, segs []string) (*yaml.Node, *yaml.Node, int) {
	return yamledit.Lookup(root, segs)
}
func isYAMLFile(p string) bool                    { return yamledit.IsYAMLFile(p) }
func isEnvFile(p string) bool                     { return yamledit.IsEnvFile(p) }
func envLookup(b []byte, k string) (string, bool) { return yamledit.EnvLookup(b, k) }
func imageTag(s string) string                    { return yamledit.ImageTag(s) }
func imageRepo(s string) string                   { return yamledit.ImageRepo(s) }
func extractValue(v, mode string) string          { return yamledit.Extract(v, mode) }
func applyOp(t, op string, segs []string, v string) (string, string, error) {
	return yamledit.ApplyOp(t, op, segs, v)
}

// --- Baustein „Wert lesen“ ---

func (r *runner) readValue(c config) (shared.NodeResult, *step) {
	st := &step{}
	files := lines(c.str("files"))
	if len(files) == 0 && r.last != nil {
		files = r.last.Files
		st.Files, st.Projects = r.last.Files, r.last.Projects
	}
	path := strings.TrimSpace(c.str("path"))
	if path == "" {
		return shared.NodeResult{Status: "fail", Output: "Pfad fehlt"}, st
	}
	segs, err := splitPath(path)
	if err != nil {
		return shared.NodeResult{Status: "fail", Output: err.Error()}, st
	}
	for _, f := range files {
		if isRosterArtifact(f) {
			continue
		}
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		var val string
		var found bool
		if isEnvFile(f) {
			val, found = envLookup(data, path)
		} else {
			root, err := parseYAML(data)
			if err != nil {
				continue
			}
			n, _, depth := lookup(root, segs)
			if depth == len(segs) && n != nil && n.Kind == yaml.ScalarNode {
				val, found = n.Value, true
			}
		}
		if !found {
			continue
		}
		st.Value = extractValue(val, c.str("extract"))
		out := fmt.Sprintf("%s = %q (aus %s)", path, val, f)
		if st.Value != val {
			out += fmt.Sprintf("\n→ %s: %q", c.str("extract"), st.Value)
		}
		return shared.NodeResult{Status: "ok", Output: out}, st
	}
	if c.bool("optional") {
		return shared.NodeResult{Status: "ok", Output: path + " nicht gefunden (optional, Wert leer)"}, st
	}
	return shared.NodeResult{Status: "fail", Output: path + " in keiner der Dateien gefunden"}, st
}

// --- Baustein „YAML ändern“ ---

func (r *runner) yamlEdit(c config) (shared.NodeResult, *step) {
	st := &step{}
	files := lines(c.str("files"))
	if len(files) == 0 && r.last != nil {
		files = r.last.Files
		st.Projects = r.last.Projects
	}
	var yfiles []string
	for _, f := range files {
		if isYAMLFile(f) && !isRosterArtifact(f) {
			yfiles = append(yfiles, f)
		}
	}
	if len(yfiles) == 0 {
		return shared.NodeResult{Status: "fail", Output: "keine YAML-Datei (*.yml/*.yaml) vom vorigen Baustein oder unter „Dateien“"}, st
	}
	ops, err := shared.ParseYAMLOps(c.str("ops"))
	if err != nil {
		return shared.NodeResult{Status: "fail", Output: err.Error()}, st
	}
	texts := map[string]string{}
	orig := map[string]string{}
	for _, f := range yfiles {
		data, err := os.ReadFile(f)
		if err != nil {
			return shared.NodeResult{Status: "fail", Output: f + ": " + err.Error()}, st
		}
		texts[f], orig[f] = string(data), string(data)
	}
	var log []string
	changes := 0
	for _, op := range ops {
		for _, p := range expandBraces(op.Path) {
			segs, err := splitPath(p)
			if err != nil {
				return shared.NodeResult{Status: "fail", Output: fmt.Sprintf("Zeile %d: %v", op.Line, err)}, st
			}
			f := pickFile(yfiles, texts, segs)
			nt, msg, err := applyOp(texts[f], op.Op, segs, op.Value)
			if err != nil {
				return shared.NodeResult{Status: "fail", Output: fmt.Sprintf("Zeile %d (%s %s): %v", op.Line, op.Op, p, err)}, st
			}
			if nt != texts[f] {
				changes++
				texts[f] = nt
			}
			log = append(log, fmt.Sprintf("%s %s: %s", op.Op, p, msg))
		}
	}
	var diffs, changed []string
	for _, f := range yfiles {
		if texts[f] == orig[f] {
			continue
		}
		if _, err := parseYAML([]byte(texts[f])); err != nil {
			return shared.NodeResult{Status: "fail", Output: f + ": Ergebnis wäre kein gültiges YAML: " + err.Error()}, st
		}
		changed = append(changed, f)
		diffs = append(diffs, unifiedDiff(f, orig[f], texts[f]))
		if r.opt.DryRun {
			continue
		}
		fi, _ := os.Stat(f)
		if !r.hasBackup(f) {
			bk := f + ".roster-bak-" + r.opt.RunID
			if err := os.WriteFile(bk, []byte(orig[f]), 0o600); err != nil {
				return shared.NodeResult{Status: "fail", Output: "Backup fehlgeschlagen: " + err.Error()}, st
			}
			r.backups = append(r.backups, backup{Path: f, Backup: bk})
		}
		if err := writeAtomic(f, []byte(texts[f]), fi.Mode().Perm()); err != nil {
			return shared.NodeResult{Status: "fail", Output: f + ": " + err.Error()}, st
		}
	}
	st.Files = yfiles
	out := fmt.Sprintf("%d Änderung(en) in %d Datei(en)", changes, len(changed))
	if r.opt.DryRun && changes > 0 {
		out += " (Vorschau – nichts geändert)"
	}
	out += "\n" + strings.Join(log, "\n")
	if changes == 0 {
		switch {
		case c.bool("fail_if_unchanged"):
			return shared.NodeResult{Status: "fail", Output: out}, st
		case !c.bool("continue_if_unchanged"):
			return shared.NodeResult{Status: "empty", Output: "nichts zu ändern – Ablauf endet hier\n" + out}, st
		}
	}
	status := "ok"
	if r.opt.DryRun {
		status = "dry"
	}
	return shared.NodeResult{Status: status, Output: out, Diff: strings.Join(diffs, "\n"), Files: changed}, st
}

// pickFile wählt die Datei, in der der Pfad am weitesten existiert (erste bei Gleichstand).
func pickFile(files []string, texts map[string]string, segs []string) string {
	best, bestDepth := files[0], -1
	for _, f := range files {
		root, err := parseYAML([]byte(texts[f]))
		if err != nil {
			continue
		}
		_, _, d := lookup(root, segs)
		if d > bestDepth {
			best, bestDepth = f, d
		}
	}
	return best
}
