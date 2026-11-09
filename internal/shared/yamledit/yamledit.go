package yamledit

import (
	"bufio"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// Package yamledit liest und ändert YAML-Dateien positionsgenau (für die Abläufe: Agent-
// Bausteine „Wert lesen“/„YAML ändern“ und die Vorschau im Editor). Änderungen werden als
// Textänderung an der Fundstelle angewendet – Kommentare, Anführungszeichen und Einrückung der
// übrigen Datei bleiben unverändert.

// --- Pfade ---

// SplitPath zerlegt "services.\"a.b\".image" in Segmente.
func SplitPath(p string) ([]string, error) {
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

// ExpandBraces erweitert {a,b,c} (auch mehrfach) zu einzelnen Pfaden.
func ExpandBraces(p string) []string {
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
		out = append(out, ExpandBraces(p[:i]+alt+p[j+1:])...)
	}
	return out
}

func docRoot(n *yaml.Node) *yaml.Node {
	if n != nil && n.Kind == yaml.DocumentNode && len(n.Content) > 0 {
		return n.Content[0]
	}
	return n
}

// Lookup folgt dem Pfad. depth = Zahl der aufgelösten Segmente; key = Schlüssel-Node des
// letzten aufgelösten Map-Eintrags (nil bei Listenindex/Wurzel).
func Lookup(root *yaml.Node, segs []string) (node, key *yaml.Node, depth int) {
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

func Parse(data []byte) (*yaml.Node, error) {
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

func IsYAMLFile(p string) bool {
	e := strings.ToLower(filepath.Ext(p))
	return e == ".yml" || e == ".yaml"
}

func IsEnvFile(p string) bool {
	b := strings.ToLower(filepath.Base(p))
	return b == ".env" || strings.HasPrefix(b, ".env.") || strings.HasSuffix(b, ".env")
}

// EnvLookup liest KEY aus einer .env-Datei (export-Präfix, Anführungszeichen, Kommentare).
func EnvLookup(data []byte, key string) (string, bool) {
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

// ImageTag liefert das Tag einer Image-Referenz ("latest", wenn keins angegeben ist).
func ImageTag(ref string) string {
	ref = strings.TrimSpace(ref)
	if i := strings.Index(ref, "@"); i >= 0 {
		ref = ref[:i]
	}
	if i := strings.LastIndex(ref, ":"); i > strings.LastIndex(ref, "/") {
		return ref[i+1:]
	}
	return "latest"
}

func Extract(v, mode string) string {
	switch mode {
	case "image_tag":
		return ImageTag(v)
	case "image_repo":
		return ImageRepo(v)
	}
	return v
}

// ApplyOp wendet eine Operation auf den Text an und liefert den neuen Text (rein, getestet).
func ApplyOp(text, op string, segs []string, value string) (string, string, error) {
	root, err := Parse([]byte(text))
	if err != nil {
		return text, "", fmt.Errorf("kein gültiges YAML: %v", err)
	}
	ls := strings.Split(text, "\n")
	node, key, depth := Lookup(root, segs)
	full := depth == len(segs)
	if full && strings.Contains(value, "{{value") {
		cur := ""
		if node != nil && node.Kind == yaml.ScalarNode {
			cur = node.Value
		}
		value = strings.NewReplacer("{{value.repo}}", ImageRepo(cur), "{{value.tag}}", ImageTag(cur), "{{value.suffix}}", TagSuffix(cur), "{{value}}", cur).Replace(value)
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
		n, k, d := Lookup(parent, segs[i:i+1])
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
			switch {
			case asList:
				add = append(add, pad+k+":", pad+strings.Repeat(" ", step)+"- "+value)
			default:
				if block, ok := blockLines(value, ind+j*step+step, step); ok {
					// {a: b, c: {d: e}} bzw. [x, y] als Block schreiben, wie man es von Hand täte.
					add = append(add, pad+k+":")
					add = append(add, block...)
				} else {
					add = append(add, pad+k+": "+value)
				}
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

// ImageRepo schneidet Tag und Digest ab ("nginx:1.27@sha256:…" -> "nginx").
func ImageRepo(img string) string {
	img = strings.TrimSpace(img)
	if i := strings.Index(img, "@"); i >= 0 {
		img = img[:i]
	}
	if i := strings.LastIndex(img, ":"); i > strings.LastIndex(img, "/") {
		img = img[:i]
	}
	return img
}

// OpResult ist das Ergebnis einer (erweiterten) Operation.
type OpResult struct {
	Line    int    `json:"line"`
	Op      string `json:"op"`
	Path    string `json:"path"`
	Message string `json:"message"`
	Changed bool   `json:"changed"`
	Error   string `json:"error,omitempty"`
}

// ApplyAll wendet Operationen (mit {a,b}-Erweiterung) nacheinander auf einen Text an. Bei
// einem Fehler wird abgebrochen; results enthält dann die bis dahin ausgeführten und die
// fehlerhafte Operation.
func ApplyAll(text string, ops []Op) (string, []OpResult, error) {
	var results []OpResult
	for _, op := range ops {
		for _, p := range ExpandBraces(op.Path) {
			res := OpResult{Line: op.Line, Op: op.Op, Path: p}
			segs, err := SplitPath(p)
			if err == nil {
				var nt, msg string
				nt, msg, err = ApplyOp(text, op.Op, segs, op.Value)
				if err == nil {
					res.Message, res.Changed = msg, nt != text
					text = nt
				}
			}
			if err != nil {
				res.Error = err.Error()
				results = append(results, res)
				return text, results, fmt.Errorf("Zeile %d (%s %s): %v", op.Line, op.Op, p, err)
			}
			results = append(results, res)
		}
	}
	if _, err := Parse([]byte(text)); err != nil {
		return text, results, fmt.Errorf("Ergebnis wäre kein gültiges YAML: %v", err)
	}
	return text, results, nil
}

// Op ist eine Operation (siehe ParseOps).
type Op struct {
	Op    string
	Path  string
	Value string
	Line  int
}

// ParseOps liest die Operationen (eine je Zeile: "set pfad = wert"); leere Zeilen und
// Zeilen mit # am Anfang werden übersprungen.
func ParseOps(text string) ([]Op, error) {
	var ops []Op
	for i, l := range strings.Split(text, "\n") {
		l = strings.TrimSpace(l)
		if l == "" || strings.HasPrefix(l, "#") {
			continue
		}
		op, rest, _ := strings.Cut(l, " ")
		op = strings.ToLower(strings.TrimSpace(op))
		path, val, hasVal := strings.Cut(rest, "=")
		path, val = strings.TrimSpace(path), strings.TrimSpace(val)
		switch op {
		case "set", "append", "ensure":
			if !hasVal {
				return nil, fmt.Errorf("Zeile %d: „%s pfad = wert“ erwartet", i+1, op)
			}
		case "delete":
		default:
			return nil, fmt.Errorf("Zeile %d: unbekannte Operation %q (set, append, ensure, delete)", i+1, op)
		}
		if path == "" {
			return nil, fmt.Errorf("Zeile %d: Pfad fehlt", i+1)
		}
		ops = append(ops, Op{Op: op, Path: path, Value: val, Line: i + 1})
	}
	if len(ops) == 0 {
		return nil, fmt.Errorf("keine Operationen")
	}
	return ops, nil
}

// blockLines schreibt einen nicht leeren Kurzform-Wert ({a: b} oder [x, y]) als eingerückten
// Block; ok=false für Skalare und leere Werte ({} / []), die bleiben, wie sie sind.
func blockLines(value string, indent, step int) ([]string, bool) {
	v := strings.TrimSpace(value)
	if !(strings.HasPrefix(v, "{") || strings.HasPrefix(v, "[")) {
		return nil, false
	}
	var doc yaml.Node
	if yaml.Unmarshal([]byte(v), &doc) != nil {
		return nil, false
	}
	n := docRoot(&doc)
	if n == nil || (n.Kind != yaml.MappingNode && n.Kind != yaml.SequenceNode) || len(n.Content) == 0 {
		return nil, false
	}
	var clear func(*yaml.Node)
	clear = func(x *yaml.Node) {
		x.Style &^= yaml.FlowStyle
		for _, c := range x.Content {
			clear(c)
		}
	}
	clear(n)
	var b strings.Builder
	enc := yaml.NewEncoder(&b)
	enc.SetIndent(step)
	if enc.Encode(n) != nil {
		return nil, false
	}
	_ = enc.Close()
	pad := strings.Repeat(" ", indent)
	var out []string
	for _, l := range strings.Split(strings.TrimRight(b.String(), "\n"), "\n") {
		out = append(out, pad+l)
	}
	return out, true
}

// TagSuffix liefert das Varianten-Anhängsel eines Image-Tags ab dem ersten "_" ("2.4.0_tp" -> "_tp").
func TagSuffix(ref string) string {
	t := ImageTag(ref)
	if i := strings.Index(t, "_"); i >= 0 {
		return t[i:]
	}
	return ""
}
