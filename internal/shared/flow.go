package shared

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/boonkerz/roster/internal/shared/yamledit"
)

// Abläufe (Flow-Designer): ein Graph aus Bausteinen, verbunden über die Ausgänge ok/fail.
// Der Server speichert und validiert ihn, der Agent führt ihn aus (Befehl run_flow).

// FlowNode ist ein Baustein. Config enthält typabhängige Einstellungen (Strings, Zahlen, Bools).
type FlowNode struct {
	ID     string         `json:"id"`
	Type   string         `json:"type"`
	Label  string         `json:"label,omitempty"`
	Config map[string]any `json:"config,omitempty"`
	X      float64        `json:"x,omitempty"` // Position im Editor
	Y      float64        `json:"y,omitempty"`
}

// FlowEdge verbindet den Ausgang Port (ok|fail) eines Knotens mit dem nächsten Knoten.
type FlowEdge struct {
	From string `json:"from"`
	Port string `json:"port"` // ok | fail
	To   string `json:"to"`
}

// FlowGraph ist der vollständige Ablauf.
type FlowGraph struct {
	Nodes []FlowNode `json:"nodes"`
	Edges []FlowEdge `json:"edges"`
}

// FlowParam ist ein Parameter, den der Assistent vor dem Start abfragt ({{param.Name}}).
type FlowParam struct {
	Name     string `json:"name"`
	Label    string `json:"label,omitempty"`
	Default  string `json:"default,omitempty"`
	Required bool   `json:"required,omitempty"`
}

// NodeResult ist das Ergebnis eines ausgeführten Knotens.
type NodeResult struct {
	ID         string   `json:"id"`
	Type       string   `json:"type"`
	Label      string   `json:"label,omitempty"`
	Status     string   `json:"status"` // ok | fail | dry | empty | skipped
	Output     string   `json:"output,omitempty"`
	Diff       string   `json:"diff,omitempty"`
	Files      []string `json:"files,omitempty"`
	DurationMs int64    `json:"duration_ms"`
}

// FlowResult ist das Gesamtergebnis eines Laufs auf einem Gerät.
type FlowResult struct {
	Status     string       `json:"status"` // ok | fail | rolled_back | dry | nothing
	Nodes      []NodeResult `json:"nodes"`
	RolledBack []string     `json:"rolled_back,omitempty"` // wiederhergestellte Dateien
	Error      string       `json:"error,omitempty"`
}

// FlowNodeTypes sind die bekannten Bausteine.
var FlowNodeTypes = map[string]bool{
	"find_files": true, "replace": true, "compose": true, "command": true,
	"http": true, "check": true, "rollback": true, "wait": true,
	"read_value": true, "yaml": true,
}

// MaxFlowNodes begrenzt die Größe eines Ablaufs.
const MaxFlowNodes = 50

var flowIDRe = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]{0,31}$`)

// FlowStart liefert den Startknoten (der einzige Knoten ohne eingehende Kante).
func FlowStart(g FlowGraph) (string, error) {
	in := map[string]bool{}
	for _, e := range g.Edges {
		in[e.To] = true
	}
	var starts []string
	for _, n := range g.Nodes {
		if !in[n.ID] {
			starts = append(starts, n.ID)
		}
	}
	if len(starts) != 1 {
		return "", fmt.Errorf("ein Ablauf braucht genau einen Startbaustein (ohne eingehende Linie), gefunden: %d", len(starts))
	}
	return starts[0], nil
}

// ValidateFlow prüft Aufbau und Verbindungen (Typen, IDs, Ports, keine Zyklen, ein Start).
func ValidateFlow(g FlowGraph) error {
	if len(g.Nodes) == 0 {
		return fmt.Errorf("der Ablauf ist leer")
	}
	if len(g.Nodes) > MaxFlowNodes {
		return fmt.Errorf("höchstens %d Bausteine", MaxFlowNodes)
	}
	ids := map[string]bool{}
	for _, n := range g.Nodes {
		if !flowIDRe.MatchString(n.ID) {
			return fmt.Errorf("ungültige Baustein-ID %q (Buchstabe, dann Buchstaben/Ziffern/_)", n.ID)
		}
		if ids[n.ID] {
			return fmt.Errorf("Baustein-ID %q doppelt", n.ID)
		}
		if !FlowNodeTypes[n.Type] {
			return fmt.Errorf("unbekannter Baustein %q", n.Type)
		}
		ids[n.ID] = true
	}
	out := map[string]bool{}
	next := map[string][]string{}
	for _, e := range g.Edges {
		if !ids[e.From] || !ids[e.To] {
			return fmt.Errorf("Linie verweist auf unbekannten Baustein")
		}
		if e.Port != "ok" && e.Port != "fail" {
			return fmt.Errorf("ungültiger Ausgang %q (ok oder fail)", e.Port)
		}
		if e.From == e.To {
			return fmt.Errorf("Baustein %q ist mit sich selbst verbunden", e.From)
		}
		k := e.From + "/" + e.Port
		if out[k] {
			return fmt.Errorf("Ausgang %s von %q ist mehrfach verbunden", e.Port, e.From)
		}
		out[k] = true
		next[e.From] = append(next[e.From], e.To)
	}
	// Zyklen per DFS.
	state := map[string]int{}
	var visit func(id string) bool
	visit = func(id string) bool {
		switch state[id] {
		case 1:
			return false
		case 2:
			return true
		}
		state[id] = 1
		for _, n := range next[id] {
			if !visit(n) {
				return false
			}
		}
		state[id] = 2
		return true
	}
	for id := range ids {
		if !visit(id) {
			return fmt.Errorf("der Ablauf enthält einen Kreis – Linien dürfen nicht zurückführen")
		}
	}
	_, err := FlowStart(g)
	return err
}

// FlowEdgeTo liefert das Ziel des Ausgangs port eines Knotens ("" = nicht verbunden).
func FlowEdgeTo(g FlowGraph, from, port string) string {
	for _, e := range g.Edges {
		if e.From == from && e.Port == port {
			return e.To
		}
	}
	return ""
}

// ValidParamName prüft Parameternamen ({{param.Name}}).
func ValidParamName(s string) bool { return flowIDRe.MatchString(strings.TrimSpace(s)) }

// YAMLOp ist eine Operation des Bausteins „YAML ändern“.
type YAMLOp struct {
	Op    string // set | append | ensure | delete
	Path  string
	Value string // roh (YAML), bei delete optional (Listeneintrag mit diesem Wert)
	Line  int
}

// ParseYAMLOps liest die Operationen des Bausteins „YAML ändern“ (siehe yamledit.ParseOps).
func ParseYAMLOps(text string) ([]YAMLOp, error) {
	ops, err := yamledit.ParseOps(text)
	if err != nil {
		return nil, err
	}
	out := make([]YAMLOp, len(ops))
	for i, o := range ops {
		out[i] = YAMLOp{Op: o.Op, Path: o.Path, Value: o.Value, Line: o.Line}
	}
	return out, nil
}
