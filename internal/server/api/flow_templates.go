package api

import "github.com/boonkerz/roster/internal/shared"

// flowTemplate ist eine mitgelieferte Vorlage für den Flow-Designer.
type flowTemplate struct {
	Key         string             `json:"key"`
	Name        string             `json:"name"`
	Description string             `json:"description"`
	Graph       shared.FlowGraph   `json:"graph"`
	Params      []shared.FlowParam `json:"params"`
}

func fn(id, typ, label string, x, y float64, cfg map[string]any) shared.FlowNode {
	return shared.FlowNode{ID: id, Type: typ, Label: label, X: x, Y: y, Config: cfg}
}

var flowTemplates = []flowTemplate{
	{
		Key: "compose_image", Name: "Image in Compose ersetzen",
		Description: "Sucht die Compose-Dateien der Container mit dem alten Image, ersetzt es in der image:-Zeile, zieht das neue Image, startet die betroffenen Dienste neu und prüft, ob sie laufen. Bei Fehler: Dateien zurück und alten Stand wieder hochfahren.",
		Params: []shared.FlowParam{
			{Name: "FROM", Label: "Altes Image (ohne Tag = alle Tags)", Required: true},
			{Name: "TO", Label: "Neues Image (z. B. nginxproxy/acme-companion)", Required: true},
			{Name: "CONTAINER", Label: "Container-Name enthält (für die Prüfung)", Default: ""},
		},
		Graph: shared.FlowGraph{
			Nodes: []shared.FlowNode{
				fn("find", "find_files", "Compose-Dateien mit altem Image", 0, 0, map[string]any{"mode": "compose_image", "image": "{{param.FROM}}"}),
				fn("repl", "replace", "Image ersetzen", 0, 120, map[string]any{"search": "{{param.FROM}}", "replace": "{{param.TO}}", "line_filter": "image:"}),
				fn("pull", "compose", "Neues Image ziehen", 0, 240, map[string]any{"action": "pull"}),
				fn("up", "compose", "Dienste neu starten", 0, 360, map[string]any{"action": "up"}),
				fn("wait", "wait", "Kurz warten", 0, 480, map[string]any{"seconds": 15.0}),
				fn("chk", "check", "Container läuft", 0, 600, map[string]any{"kind": "container", "container": "{{param.CONTAINER}}", "retries": 3.0, "interval_s": 10.0}),
				fn("rb", "rollback", "Zurückrollen", 340, 470, map[string]any{"compose_up": true}),
			},
			Edges: []shared.FlowEdge{
				{From: "find", Port: "ok", To: "repl"}, {From: "repl", Port: "ok", To: "pull"}, {From: "pull", Port: "ok", To: "up"},
				{From: "up", Port: "ok", To: "wait"}, {From: "wait", Port: "ok", To: "chk"},
				{From: "up", Port: "fail", To: "rb"}, {From: "chk", Port: "fail", To: "rb"},
			},
		},
	},
	{
		Key: "replace_restart", Name: "Wert in Datei ersetzen und Dienste neu starten",
		Description: "Ersetzt einen Wert (z. B. Port 8000 → 80) in Dateien, startet das Compose-Projekt im Verzeichnis neu und prüft den Port. Bei Fehler zurück.",
		Params: []shared.FlowParam{
			{Name: "FILES", Label: "Datei(en) bzw. Muster, eine je Zeile", Required: true},
			{Name: "SEARCH", Label: "Suchen", Required: true},
			{Name: "REPLACE", Label: "Ersetzen durch", Required: true},
			{Name: "DIR", Label: "Compose-Verzeichnis", Required: true},
			{Name: "PORT", Label: "Port für die Prüfung", Default: "80"},
		},
		Graph: shared.FlowGraph{
			Nodes: []shared.FlowNode{
				fn("find", "find_files", "Dateien finden", 0, 0, map[string]any{"mode": "paths", "paths": "{{param.FILES}}", "contains": "{{param.SEARCH}}"}),
				fn("repl", "replace", "Wert ersetzen", 0, 120, map[string]any{"search": "{{param.SEARCH}}", "replace": "{{param.REPLACE}}"}),
				fn("up", "compose", "Projekt neu starten", 0, 240, map[string]any{"action": "up", "dir": "{{param.DIR}}", "all_services": true}),
				fn("chk", "check", "Port erreichbar", 0, 360, map[string]any{"kind": "tcp", "host": "127.0.0.1", "port": "{{param.PORT}}", "retries": 5.0, "interval_s": 5.0}),
				fn("rb", "rollback", "Zurückrollen", 320, 300, map[string]any{"compose_up": false}),
				fn("up2", "compose", "Alten Stand starten", 320, 420, map[string]any{"action": "up", "dir": "{{param.DIR}}", "all_services": true}),
			},
			Edges: []shared.FlowEdge{
				{From: "find", Port: "ok", To: "repl"}, {From: "repl", Port: "ok", To: "up"}, {From: "up", Port: "ok", To: "chk"},
				{From: "up", Port: "fail", To: "rb"}, {From: "chk", Port: "fail", To: "rb"}, {From: "rb", Port: "ok", To: "up2"},
			},
		},
	},
	{
		Key: "api_value", Name: "API abfragen und Wert eintragen",
		Description: "Fragt eine API ab, liest einen Wert per JSON-Pfad und trägt ihn per regulärem Ausdruck in eine Datei ein (z. B. die neueste Version in eine .env).",
		Params: []shared.FlowParam{
			{Name: "URL", Label: "API-URL", Required: true},
			{Name: "JSON_PATH", Label: "JSON-Pfad (z. B. data.0.version)", Required: true},
			{Name: "FILE", Label: "Datei", Required: true},
			{Name: "PATTERN", Label: "Regulärer Ausdruck der zu ersetzenden Stelle (z. B. APP_TAG=.*)", Required: true},
			{Name: "PREFIX", Label: "Text vor dem Wert (z. B. APP_TAG=)", Default: ""},
		},
		Graph: shared.FlowGraph{
			Nodes: []shared.FlowNode{
				fn("api", "http", "API abfragen", 0, 0, map[string]any{"method": "GET", "url": "{{param.URL}}", "json_path": "{{param.JSON_PATH}}", "require_value": true}),
				fn("repl", "replace", "Wert eintragen", 0, 120, map[string]any{"files": "{{param.FILE}}", "search": "{{param.PATTERN}}", "replace": "{{param.PREFIX}}{{steps.api.value}}", "regex": true}),
			},
			Edges: []shared.FlowEdge{{From: "api", Port: "ok", To: "repl"}},
		},
	},
}
