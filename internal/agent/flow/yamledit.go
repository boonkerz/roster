package flow

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/boonkerz/roster/internal/shared"
)

// „Wert lesen“ und „YAML ändern“: Pfade wie services.php-fpm.image in YAML/JSON (und Schlüssel
// in .env-Dateien). Änderungen werden positionsgenau als Textänderung angewendet – Kommentare,
// Anführungszeichen und Einrückung der übrigen Datei bleiben unverändert.

// --- Pfade ---

// splitPath zerlegt "services.\"a.b\".image" in Segmente.
func splitPath(p string) ([]string, error) {
	var segs []string
	var cur strings.Builder
	inQ := false
	for _, r := range p {
		switch {
		case r == '"':
			inQ = !inQ
		case r == '.' && !inQ:
			segs = append(segs, cur.String())
			cur.Reset()
		default:
			cur.WriteRune(r)
		}
	}
	if inQ {
		return nil, fmt.Errorf("Anführungszeichen im Pfad %q nicht geschlossen", p)
	}
	segs = append(segs, cur.String())
	for _, s := range segs {
		if strings.TrimSpace(s) == "" {
			return nil, fmt.Errorf("leeres Segment im Pfad %q", p)
		}
	}
	return segs, nil
}

// expandBraces erweitert {a,b,c} (auch mehrfach) zu einzelnen Pfaden.
func expandBraces(p string) []string {
	i := strings.Index(p, "{")
	if i < 0 {
		return []string{p}
	}
	j := strings.Index(p[i:], "}")
	if j < 0 {
		return []string{p}
	}
	j += i
	var out []string
	for _, alt := range strings.Split(p[i+1:j], ",") {
		if alt = strings.TrimSpace(alt); alt == "" {
			continue
		}
		out = append(out, expandBraces(p[:i]+alt+p[j+1:])...)
	}
	return out
}

func docRoot(n *yaml.Node) *yaml.Node {
	if n != nil && n.Kind == yaml.DocumentNode && len(n.Content) > 0 {
		return n.Content[0]
	}
	return n
}

// lookup folgt dem Pfad. depth = Zahl der aufgelösten Segmente; key = Schlüssel-Node des
// letzten aufgelösten Map-Eintrags (nil bei Listenindex/Wurzel).
func lookup(root *yaml.Node, segs []string) (node, key *yaml.Node, depth int) {
	node = root
	for _, s := range segs {
		switch {
		case node == nil:
			return nil, key, depth
		case node.Kind == yaml.AliasNode:
			node = node.Alias
			fallthrough
		case node.Kind == yaml.MappingNode:
			if node.Kind != yaml.MappingNode {
				return node, key, depth
			}
			found := false
			for i := 0; i+1 < len(node.Content); i += 2 {
				if node.Content[i].Value == s {
					key, node, found = node.Content[i], node.Content[i+1], true
					break
				}
			}
			if !found {
				return node, key, depth
			}
		case node.Kind == yaml.SequenceNode:
			idx, err := strconv.Atoi(s)
			if err != nil || idx < 0 || idx >= len(node.Content) {
				return node, key, depth
			}
			key, node = nil, node.Content[idx]
		default:
			return node, key, depth
		}
		depth++
	}
	return node, key, depth
}

func parseYAML(data []byte) (*yaml.Node, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	root := docRoot(&doc)
	if root == nil || root.Kind == 0 {
		return &yaml.Node{Kind: yaml.MappingNode}, nil
	}
	return root, nil
}

func isYAMLFile(p string) bool {
	e := strings.ToLower(filepath.Ext(p))
	return e == ".yml" || e == ".yaml"
}

func isEnvFile(p string) bool {
	b := strings.ToLower(filepath.Base(p))
	return b == ".env" || strings.HasPrefix(b, ".env.") || strings.HasSuffix(b, ".env")
}

// envLookup liest KEY aus einer .env-Datei (export-Präfix, Anführungszeichen, Kommentare).
func envLookup(data []byte, key string) (string, bool) {
	sc := bufio.NewScanner(strings.NewReader(string(data)))
	for sc.Scan() {
		l := strings.TrimSpace(sc.Text())
		if l == "" || strings.HasPrefix(l, "#") {
			continue
		}
		l = strings.TrimPrefix(l, "export ")
		k, v, ok := strings.Cut(l, "=")
		if !ok || strings.TrimSpace(k) != key {
			continue
		}
		v = strings.TrimSpace(v)
		if len(v) >= 2 && (v[0] == '"' && v[len(v)-1] == '"' || v[0] == '\'' && v[len(v)-1] == '\'') {
			v = v[1 : len(v)-1]
		} else if i := strings.Index(v, " #"); i >= 0 {
			v = strings.TrimSpace(v[:i])
		}
		return v, true
	}
	return "", false
}

// imageTag liefert das Tag einer Image-Referenz ("latest", wenn keins angegeben ist).
func imageTag(ref string) string {
	ref = strings.TrimSpace(ref)
	if i := strings.Index(ref, "@"); i >= 0 {
		ref = ref[:i]
	}
	if i := strings.LastIndex(ref, ":"); i > strings.LastIndex(ref, "/") {
		return ref[i+1:]
	}
	return "latest"
}

func extractValue(v, mode string) string {
	switch mode {
	case "image_tag":
		return imageTag(v)
	case "image_repo":
		return imageRepo(v)
	}
	return v
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

// applyOp wendet eine Operation auf den Text an und liefert den neuen Text (rein, getestet).
func applyOp(text, op string, segs []string, value string) (string, string, error) {
	root, err := parseYAML([]byte(text))
	if err != nil {
		return text, "", fmt.Errorf("kein gültiges YAML: %v", err)
	}
	ls := strings.Split(text, "\n")
	node, key, depth := lookup(root, segs)
	full := depth == len(segs)
	if full && strings.Contains(value, "{{value") {
		cur := ""
		if node != nil && node.Kind == yaml.ScalarNode {
			cur = node.Value
		}
		value = strings.NewReplacer("{{value.repo}}", imageRepo(cur), "{{value.tag}}", imageTag(cur), "{{value}}", cur).Replace(value)
	}
	switch op {
	case "set":
		if full {
			if node.Kind != yaml.ScalarNode {
				return text, "", fmt.Errorf("Ziel ist keine einzelne Angabe (Liste/Map) – set ersetzt nur Werte")
			}
			if sameScalar(node, value) {
				return text, "unverändert", nil
			}
			nl, err := replaceScalar(ls, node, value)
			if err != nil {
				return text, "", err
			}
			return strings.Join(nl, "\n"), fmt.Sprintf("%q → %s", node.Value, value), nil
		}
		nl, err := createPath(ls, root, segs, value, false)
		return strings.Join(nl, "\n"), "angelegt", err
	case "ensure":
		if full {
			return text, "vorhanden", nil
		}
		nl, err := createPath(ls, root, segs, value, false)
		return strings.Join(nl, "\n"), "angelegt", err
	case "append":
		if full {
			switch {
			case node.Kind == yaml.SequenceNode && node.Style&yaml.FlowStyle == 0 && len(node.Content) > 0:
				for _, it := range node.Content {
					if sameScalar(it, value) {
						return text, "schon vorhanden", nil
					}
				}
				last := node.Content[len(node.Content)-1]
				dash := node.Column - 1
				end := entryEnd(ls, last.Line-1, dash)
				nl := insertLines(ls, end+1, []string{strings.Repeat(" ", dash) + "- " + value})
				return strings.Join(nl, "\n"), "angehängt", nil
			case isEmptyValue(node) && key != nil:
				nl := clearValue(ls, key, node)
				ind := key.Column - 1 + childIndent(ls, root)
				nl = insertLines(nl, key.Line, []string{strings.Repeat(" ", ind) + "- " + value})
				return strings.Join(nl, "\n"), "angehängt", nil
			case node.Kind == yaml.SequenceNode:
				return text, "", fmt.Errorf("Liste in Kurzform ([a, b]) wird nicht umgeschrieben – bitte als Block-Liste anlegen")
			default:
				return text, "", fmt.Errorf("Ziel ist keine Liste")
			}
		}
		nl, err := createPath(ls, root, segs, value, true)
		return strings.Join(nl, "\n"), "Liste angelegt", err
	case "delete":
		if !full {
			return text, "nicht vorhanden", nil
		}
		if value != "" && node.Kind == yaml.SequenceNode {
			for _, it := range node.Content {
				if sameScalar(it, value) {
					dash := node.Column - 1
					end := entryEnd(ls, it.Line-1, dash)
					return strings.Join(append(ls[:it.Line-1:it.Line-1], ls[end+1:]...), "\n"), "Eintrag entfernt", nil
				}
			}
			return text, "Eintrag nicht vorhanden", nil
		}
		if key == nil {
			return text, "", fmt.Errorf("nur Map-Einträge oder Listeneinträge mit „= wert“ lassen sich löschen")
		}
		end := entryEnd(ls, key.Line-1, key.Column-1)
		return strings.Join(append(ls[:key.Line-1:key.Line-1], ls[end+1:]...), "\n"), "entfernt", nil
	}
	return text, "", fmt.Errorf("unbekannte Operation %s", op)
}

// sameScalar vergleicht einen Skalar-Node mit einem Rohwert (YAML-Auswertung beider Seiten).
func sameScalar(n *yaml.Node, raw string) bool {
	if n == nil || n.Kind != yaml.ScalarNode {
		return false
	}
	var v yaml.Node
	if yaml.Unmarshal([]byte(raw), &v) == nil {
		if r := docRoot(&v); r != nil && r.Kind == yaml.ScalarNode {
			return r.Value == n.Value
		}
	}
	return strings.Trim(raw, `"'`) == n.Value
}

func isEmptyValue(n *yaml.Node) bool {
	if n == nil {
		return true
	}
	switch n.Kind {
	case yaml.ScalarNode:
		return n.Tag == "!!null" || n.Value == ""
	case yaml.MappingNode, yaml.SequenceNode:
		return len(n.Content) == 0
	}
	return false
}

// entryEnd liefert die letzte Zeile (0-basiert) eines Eintrags, der in Zeile start mit der
// Einrückung base beginnt: alle folgenden Zeilen mit größerer Einrückung (leere dazwischen).
func entryEnd(ls []string, start, base int) int {
	last := start
	for j := start + 1; j < len(ls); j++ {
		t := strings.TrimSpace(ls[j])
		if t == "" {
			continue
		}
		if indentOf(ls[j]) <= base {
			break
		}
		last = j
	}
	return last
}

func indentOf(l string) int { return len(l) - len(strings.TrimLeft(l, " ")) }

// childIndent schätzt die Einrückungsstufe der Datei (Abstand Wurzel → erste Kind-Ebene).
func childIndent(ls []string, root *yaml.Node) int {
	if root.Kind == yaml.MappingNode {
		for i := 1; i < len(root.Content); i += 2 {
			v := root.Content[i]
			if v.Kind == yaml.MappingNode && len(v.Content) > 0 {
				if d := v.Content[0].Column - root.Content[i-1].Column; d > 0 {
					return d
				}
			}
		}
	}
	return 2
}

func insertLines(ls []string, at int, add []string) []string {
	out := make([]string, 0, len(ls)+len(add))
	out = append(out, ls[:at]...)
	out = append(out, add...)
	return append(out, ls[at:]...)
}

// clearValue entfernt einen leeren Wert ({} / [] / null) hinter dem Schlüssel, damit dort
// Kinder als Block angehängt werden können.
func clearValue(ls []string, key, val *yaml.Node) []string {
	out := append([]string{}, ls...)
	if val == nil || val.Line != key.Line {
		return out
	}
	l := out[key.Line-1]
	c := val.Column - 1
	if c < 0 || c > len(l) {
		return out
	}
	rest := l[c:]
	comment := ""
	if i := strings.Index(rest, " #"); i >= 0 {
		comment = rest[i:]
	}
	out[key.Line-1] = strings.TrimRight(l[:c], " ") + comment
	return out
}

// createPath legt fehlende Teile des Pfads als Block an (asList: Blatt ist eine Liste mit value).
func createPath(ls []string, root *yaml.Node, segs []string, value string, asList bool) ([]string, error) {
	step := childIndent(ls, root)
	parent, pkey := root, (*yaml.Node)(nil)
	i := 0
	for ; i < len(segs); i++ {
		n, k, d := lookup(parent, segs[i:i+1])
		if d == 0 {
			break
		}
		parent, pkey = n, k
	}
	if parent.Kind == yaml.SequenceNode || (parent.Kind == yaml.ScalarNode && !isEmptyValue(parent)) {
		return ls, fmt.Errorf("Pfadteil %q ist kein Map-Eintrag", strings.Join(segs[:i], "."))
	}
	if parent.Kind == yaml.MappingNode && parent.Style&yaml.FlowStyle != 0 && len(parent.Content) > 0 {
		return ls, fmt.Errorf("Map in Kurzform ({a: b}) wird nicht umgeschrieben")
	}
	// Einfügeposition und Einrückung der ersten neuen Zeile.
	var at, ind int
	out := ls
	switch {
	case parent.Kind == yaml.MappingNode && len(parent.Content) > 0:
		first, lastKey := parent.Content[0], parent.Content[len(parent.Content)-2]
		ind = first.Column - 1
		at = entryEnd(ls, lastKey.Line-1, lastKey.Column-1) + 1
	case pkey != nil: // leerer Wert hinter einem Schlüssel
		out = clearValue(ls, pkey, parent)
		ind = pkey.Column - 1 + step
		at = pkey.Line
	default: // leere Datei
		ind, at = 0, len(ls)
	}
	var add []string
	rest := segs[i:]
	for j, s := range rest {
		pad := strings.Repeat(" ", ind+j*step)
		k := yamlKey(s)
		if j == len(rest)-1 {
			if asList {
				add = append(add, pad+k+":", pad+strings.Repeat(" ", step)+"- "+value)
			} else {
				add = append(add, pad+k+": "+value)
			}
		} else {
			add = append(add, pad+k+":")
		}
	}
	return insertLines(out, at, add), nil
}

// yamlKey setzt Schlüssel mit Sonderzeichen in Anführungszeichen.
func yamlKey(s string) string {
	if strings.ContainsAny(s, ":#{}[],&*!|>'\"%@`") || strings.TrimSpace(s) != s {
		return strconv.Quote(s)
	}
	return s
}

// replaceScalar ersetzt genau die Spanne eines einzeiligen Skalars; Anführungszeichen des
// Originals bleiben erhalten.
func replaceScalar(ls []string, n *yaml.Node, value string) ([]string, error) {
	if n.Style&(yaml.LiteralStyle|yaml.FoldedStyle) != 0 {
		return ls, fmt.Errorf("mehrzeiliger Wert (| oder >) wird nicht ersetzt")
	}
	li := n.Line - 1
	if li < 0 || li >= len(ls) {
		return ls, fmt.Errorf("Position nicht gefunden")
	}
	l := ls[li]
	start := n.Column - 1
	if start < 0 || start > len(l) {
		return ls, fmt.Errorf("Position nicht gefunden")
	}
	end := len(l)
	switch {
	case n.Style&yaml.DoubleQuotedStyle != 0:
		end = closingQuote(l, start, '"')
	case n.Style&yaml.SingleQuotedStyle != 0:
		end = closingQuote(l, start, '\'')
	default:
		if i := strings.Index(l[start:], " #"); i >= 0 {
			end = start + i
		}
		for end > start && l[end-1] == ' ' {
			end--
		}
	}
	if end < 0 {
		return ls, fmt.Errorf("Wert erstreckt sich über mehrere Zeilen")
	}
	nv := value
	plain := !(strings.HasPrefix(value, `"`) || strings.HasPrefix(value, "'"))
	switch {
	case plain && n.Style&yaml.DoubleQuotedStyle != 0:
		nv = strconv.Quote(value)
	case plain && n.Style&yaml.SingleQuotedStyle != 0:
		nv = "'" + strings.ReplaceAll(value, "'", "''") + "'"
	}
	out := append([]string{}, ls...)
	out[li] = l[:start] + nv + l[end:]
	return out, nil
}

// closingQuote liefert die Position hinter dem schließenden Anführungszeichen (-1 = keins).
func closingQuote(l string, start int, q byte) int {
	for i := start + 1; i < len(l); i++ {
		switch {
		case q == '"' && l[i] == '\\':
			i++
		case l[i] == q:
			if q == '\'' && i+1 < len(l) && l[i+1] == '\'' {
				i++
				continue
			}
			return i + 1
		}
	}
	return -1
}
