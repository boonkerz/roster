// Package fsops liest die Operationen des Ablauf-Bausteins „Dateisystem“:
//
//	mkdir PFAD [mode=0775] [owner=benutzer:gruppe]
//	chmod PFAD MODUS
//	chown PFAD BENUTZER[:GRUPPE]
//	file  PFAD = INHALT            (nur wenn die Datei fehlt; "…" mit \n erlaubt)
//
// Pfade mit Leerzeichen stehen in Anführungszeichen; # am Zeilenanfang ist ein Kommentar.
package fsops

import (
	"fmt"
	"strconv"
	"strings"
)

// Op ist eine Operation.
type Op struct {
	Op      string            // mkdir | chmod | chown | file
	Path    string            // roh (vor {a,b}-Erweiterung)
	Arg     string            // chmod: Modus, chown: Besitzer
	Opts    map[string]string // mkdir: mode, owner
	Content string            // file
	Line    int
}

// ParseOps liest alle Operationen (eine je Zeile).
func ParseOps(text string) ([]Op, error) {
	var ops []Op
	for i, raw := range strings.Split(text, "\n") {
		l := strings.TrimSpace(raw)
		if l == "" || strings.HasPrefix(l, "#") {
			continue
		}
		op, err := parseLine(l)
		if err != nil {
			return nil, fmt.Errorf("Zeile %d: %v", i+1, err)
		}
		op.Line = i + 1
		ops = append(ops, op)
	}
	if len(ops) == 0 {
		return nil, fmt.Errorf("keine Operationen")
	}
	return ops, nil
}

func parseLine(l string) (Op, error) {
	name, rest, _ := strings.Cut(l, " ")
	op := Op{Op: strings.ToLower(name), Opts: map[string]string{}}
	rest = strings.TrimSpace(rest)
	if op.Op == "file" {
		p, content, ok := cutUnquoted(rest, " = ")
		if !ok {
			return op, fmt.Errorf("„file pfad = inhalt“ erwartet")
		}
		toks, err := tokens(p)
		if err != nil || len(toks) != 1 {
			return op, fmt.Errorf("genau ein Pfad erwartet")
		}
		op.Path = toks[0]
		c := strings.TrimSpace(content)
		if strings.HasPrefix(c, `"`) {
			u, err := strconv.Unquote(c)
			if err != nil {
				return op, fmt.Errorf("Inhalt in Anführungszeichen ungültig: %v", err)
			}
			c = u
		}
		op.Content = c
		return op, nil
	}
	toks, err := tokens(rest)
	if err != nil {
		return op, err
	}
	if len(toks) == 0 {
		return op, fmt.Errorf("Pfad fehlt")
	}
	op.Path = toks[0]
	args := toks[1:]
	switch op.Op {
	case "mkdir":
		for _, a := range args {
			k, v, ok := strings.Cut(a, "=")
			if !ok || (k != "mode" && k != "owner") {
				return op, fmt.Errorf("unbekannte Angabe %q (mode=…, owner=…)", a)
			}
			op.Opts[k] = v
		}
		if m, ok := op.Opts["mode"]; ok {
			if _, err := ParseMode(m); err != nil {
				return op, err
			}
		}
	case "chmod":
		if len(args) != 1 {
			return op, fmt.Errorf("„chmod pfad modus“ erwartet")
		}
		if _, err := ParseMode(args[0]); err != nil {
			return op, err
		}
		op.Arg = args[0]
	case "chown":
		if len(args) != 1 {
			return op, fmt.Errorf("„chown pfad benutzer[:gruppe]“ erwartet")
		}
		op.Arg = args[0]
	default:
		return op, fmt.Errorf("unbekannte Operation %q (mkdir, chmod, chown, file)", name)
	}
	return op, nil
}

// ParseMode liest einen oktalen Modus ("0775", "775").
func ParseMode(s string) (uint32, error) {
	v, err := strconv.ParseUint(strings.TrimSpace(s), 8, 32)
	if err != nil || v > 0o7777 {
		return 0, fmt.Errorf("ungültiger Modus %q (oktal, z. B. 0775)", s)
	}
	return uint32(v), nil
}

// tokens trennt an Leerzeichen; "…" fasst zusammen.
func tokens(s string) ([]string, error) {
	var out []string
	var cur strings.Builder
	inQ, has := false, false
	for _, r := range s {
		switch {
		case r == '"':
			inQ, has = !inQ, true
		case (r == ' ' || r == '\t') && !inQ:
			if has {
				out = append(out, cur.String())
				cur.Reset()
				has = false
			}
		default:
			cur.WriteRune(r)
			has = true
		}
	}
	if inQ {
		return nil, fmt.Errorf("Anführungszeichen nicht geschlossen")
	}
	if has {
		out = append(out, cur.String())
	}
	return out, nil
}

// cutUnquoted trennt am ersten sep außerhalb von Anführungszeichen.
func cutUnquoted(s, sep string) (string, string, bool) {
	inQ := false
	for i := 0; i+len(sep) <= len(s); i++ {
		if s[i] == '"' {
			inQ = !inQ
		}
		if !inQ && s[i:i+len(sep)] == sep {
			return s[:i], s[i+len(sep):], true
		}
	}
	return s, "", false
}
