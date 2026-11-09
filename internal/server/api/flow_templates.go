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
		Key: "compose_upgrade", Name: "Compose: Version anheben, wenn älter",
		Description: "Liest die Image-Version eines Dienstes aus der Compose-Datei. Ist sie älter als die Zielversion: Datenbank sichern (optional), Volume-Verzeichnis anlegen (optional), Images der Dienste auf die Zielversion setzen (Repository und Anhängsel wie _tp bleiben), Mount und Volume eintragen, pull, up, prüfen, Befehl im Container (optional, z. B. Cache leeren), alte Images aufräumen und Meldung schicken. Bei Fehler zurückrollen und Fehlermeldung schicken. Sonst endet der Ablauf ohne Änderung.",
		Params: []shared.FlowParam{
			{Name: "IMAGE", Label: "Image des Prüf-Dienstes zum Finden – Name genügt (z. B. php-fpm-prod), * als Platzhalter", Required: true},
			{Name: "CHECK_SERVICE", Label: "Dienst, dessen Version geprüft wird (genau einer)", Default: "php-fpm", Required: true},
			{Name: "VERSION", Label: "Zielversion", Default: "2.5.0", Required: true},
			{Name: "SERVICES", Label: "Dienste, deren Image angehoben wird (kommagetrennt)", Default: "web,php-fpm,cron", Required: true},
			{Name: "MOUNT", Label: "Neues Mount (z. B. shop_media:/var/www/media)", Required: true},
			{Name: "MOUNT_SERVICES", Label: "Dienste, die das Mount bekommen (kommagetrennt)", Default: "php-fpm", Required: true},
			{Name: "VOLUME", Label: "Name des neuen Volumes (z. B. shop_media)", Required: true},
			{Name: "VOLUME_DIR", Label: "Verzeichnis für das Volume auf dem Host (leer = einfaches Docker-Volume)", Default: ""},
			{Name: "DB_SERVICE", Label: "Datenbank-Dienst vorher sichern (leer = keine Sicherung)", Default: ""},
			{Name: "POST_SERVICE", Label: "Dienst für den Befehl nach dem Update", Default: "php-fpm"},
			{Name: "POST_CMD", Label: "Befehl nach dem Update (leer = keiner), z. B. bin/console cache:clear", Default: ""},
		},
		Graph: shared.FlowGraph{
			Nodes: []shared.FlowNode{
				fn("find", "find_files", "Compose-Datei finden", 0, 0, map[string]any{"mode": "compose_image", "image": "{{param.IMAGE}}"}),
				fn("ver", "read_value", "Version lesen", 0, 110, map[string]any{"path": "services.{{param.CHECK_SERVICE}}.image", "extract": "image_tag"}),
				fn("cmp", "check", "Älter als Zielversion?", 0, 220, map[string]any{"kind": "expr", "value": "{{steps.ver.value}}", "op": "version_lt", "expect": "{{param.VERSION}}", "false_ends": true}),
				fn("bak", "db_backup", "Datenbank sichern", 0, 330, map[string]any{"service": "{{param.DB_SERVICE}}", "engine": "auto", "keep": 5.0, "only_if": "{{param.DB_SERVICE}}"}),
				fn("dir", "fs", "Volume-Verzeichnis anlegen", 0, 440, map[string]any{"ops": "mkdir {{param.VOLUME_DIR}} mode=0775", "only_if": "{{param.VOLUME_DIR}}"}),
				fn("edit", "yaml", "Compose-Datei anpassen", 0, 550, map[string]any{"ops": "set services.{{{param.SERVICES}}}.image = {{value.repo}}:{{param.VERSION}}{{value.suffix}}\nappend services.{{{param.MOUNT_SERVICES}}}.volumes = {{param.MOUNT}}"}),
				fn("volb", "yaml", "Volume (Verzeichnis) eintragen", 0, 660, map[string]any{"ops": "ensure volumes.{{param.VOLUME}} = {driver: local, driver_opts: {type: none, o: bind, device: {{param.VOLUME_DIR}}}}", "only_if": "{{param.VOLUME_DIR}}", "continue_if_unchanged": true}),
				fn("volp", "yaml", "Volume eintragen", 0, 770, map[string]any{"ops": "ensure volumes.{{param.VOLUME}} = {}", "only_if": "!{{param.VOLUME_DIR}}", "continue_if_unchanged": true}),
				fn("pull", "compose", "Images ziehen", 0, 880, map[string]any{"action": "pull", "all_services": true}),
				fn("up", "compose", "Neu starten", 0, 990, map[string]any{"action": "up", "all_services": true}),
				fn("wait", "wait", "Kurz warten", 0, 1100, map[string]any{"seconds": 20.0}),
				fn("chk", "check", "Dienste laufen", 0, 1210, map[string]any{"kind": "container", "retries": 3.0, "interval_s": 10.0}),
				fn("post", "container_exec", "Befehl im Container", 0, 1320, map[string]any{"service": "{{param.POST_SERVICE}}", "command": "{{param.POST_CMD}}", "only_if": "{{param.POST_CMD}}", "timeout_s": 900.0}),
				fn("prune", "docker_prune", "Alte Images aufräumen", 0, 1430, map[string]any{"mode": "dangling", "older_than_days": 0.0}),
				fn("okmsg", "notify", "Erfolg melden", 0, 1540, map[string]any{"subject": "Update auf {{param.VERSION}} erfolgreich", "message": "Version vorher: {{steps.ver.value}}\nSicherung: {{steps.bak.value}}", "severity": "info"}),
				fn("rb", "rollback", "Zurückrollen", 360, 1150, map[string]any{"compose_up": true}),
				fn("errmsg", "notify", "Fehler melden", 360, 1270, map[string]any{"subject": "Update auf {{param.VERSION}} fehlgeschlagen – zurückgerollt", "message": "Version vorher: {{steps.ver.value}}\nSicherung: {{steps.bak.value}}", "severity": "warning"}),
			},
			Edges: []shared.FlowEdge{
				{From: "find", Port: "ok", To: "ver"}, {From: "ver", Port: "ok", To: "cmp"}, {From: "cmp", Port: "ok", To: "bak"},
				{From: "bak", Port: "ok", To: "dir"}, {From: "dir", Port: "ok", To: "edit"}, {From: "edit", Port: "ok", To: "volb"},
				{From: "volb", Port: "ok", To: "volp"}, {From: "volp", Port: "ok", To: "pull"}, {From: "pull", Port: "ok", To: "up"},
				{From: "up", Port: "ok", To: "wait"}, {From: "wait", Port: "ok", To: "chk"}, {From: "chk", Port: "ok", To: "post"},
				{From: "post", Port: "ok", To: "prune"}, {From: "prune", Port: "ok", To: "okmsg"},
				{From: "pull", Port: "fail", To: "rb"}, {From: "up", Port: "fail", To: "rb"}, {From: "chk", Port: "fail", To: "rb"},
				{From: "post", Port: "fail", To: "rb"}, {From: "rb", Port: "ok", To: "errmsg"},
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
