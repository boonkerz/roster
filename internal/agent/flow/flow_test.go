package flow

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/boonkerz/roster/internal/shared"
)

func node(id, typ string, cfg map[string]any) shared.FlowNode {
	return shared.FlowNode{ID: id, Type: typ, Config: cfg}
}

func TestValidateFlow(t *testing.T) {
	ok := shared.FlowGraph{Nodes: []shared.FlowNode{node("a", "wait", nil), node("b", "wait", nil)}, Edges: []shared.FlowEdge{{From: "a", Port: "ok", To: "b"}}}
	if err := shared.ValidateFlow(ok); err != nil {
		t.Fatal(err)
	}
	cyc := shared.FlowGraph{Nodes: []shared.FlowNode{node("a", "wait", nil), node("b", "wait", nil), node("c", "wait", nil)},
		Edges: []shared.FlowEdge{{From: "a", Port: "ok", To: "b"}, {From: "b", Port: "ok", To: "c"}, {From: "c", Port: "ok", To: "b"}}}
	if err := shared.ValidateFlow(cyc); err == nil || !strings.Contains(err.Error(), "Kreis") {
		t.Fatalf("zyklus: %v", err)
	}
	two := shared.FlowGraph{Nodes: []shared.FlowNode{node("a", "wait", nil), node("b", "wait", nil)}}
	if err := shared.ValidateFlow(two); err == nil {
		t.Fatal("zwei Starts akzeptiert")
	}
	bad := shared.FlowGraph{Nodes: []shared.FlowNode{node("a", "rm_rf", nil)}}
	if err := shared.ValidateFlow(bad); err == nil {
		t.Fatal("unbekannter Typ akzeptiert")
	}
	dup := shared.FlowGraph{Nodes: []shared.FlowNode{node("a", "wait", nil), node("b", "wait", nil), node("c", "wait", nil)},
		Edges: []shared.FlowEdge{{From: "a", Port: "ok", To: "b"}, {From: "a", Port: "ok", To: "c"}}}
	if err := shared.ValidateFlow(dup); err == nil {
		t.Fatal("doppelter Ausgang akzeptiert")
	}
}

func TestReplaceDryRunAndApply(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "docker-compose.yml")
	orig := "services:\n  proxy:\n    image: nginxproxy/nginx-proxy\n  companion:\n    image: jrcs/letsencrypt-nginx-proxy-companion\n    environment:\n      - NOTE=jrcs/letsencrypt-nginx-proxy-companion\n    ports:\n      - \"8000:8000\"\n"
	os.WriteFile(f, []byte(orig), 0o640)
	g := shared.FlowGraph{Nodes: []shared.FlowNode{
		node("find", "find_files", map[string]any{"mode": "paths", "paths": filepath.Join(dir, "*.yml")}),
		node("img", "replace", map[string]any{"search": "{{param.FROM}}", "replace": "{{param.TO}}", "line_filter": "image:"}),
		node("port", "replace", map[string]any{"files": f, "search": `"8000:8000"`, "replace": `"80:8000"`}),
	}, Edges: []shared.FlowEdge{{From: "find", Port: "ok", To: "img"}, {From: "img", Port: "ok", To: "port"}}}
	params := map[string]string{"FROM": "jrcs/letsencrypt-nginx-proxy-companion", "TO": "nginxproxy/acme-companion"}

	res := Run(context.Background(), g, Options{RunID: "r1", DryRun: true, Params: params})
	if res.Status != "dry" || len(res.Nodes) != 3 {
		t.Fatalf("dry: %+v", res)
	}
	if !strings.Contains(res.Nodes[1].Diff, "-    image: jrcs/letsencrypt-nginx-proxy-companion") || !strings.Contains(res.Nodes[1].Diff, "+    image: nginxproxy/acme-companion") {
		t.Fatalf("diff: %s", res.Nodes[1].Diff)
	}
	if strings.Contains(res.Nodes[1].Diff, "+      - NOTE=nginxproxy") {
		t.Fatal("Zeilenfilter ignoriert")
	}
	if b, _ := os.ReadFile(f); string(b) != orig {
		t.Fatal("Vorschau hat Datei geändert")
	}

	res = Run(context.Background(), g, Options{RunID: "r2", Params: params})
	if res.Status != "ok" {
		t.Fatalf("apply: %+v", res)
	}
	b, _ := os.ReadFile(f)
	got := string(b)
	if !strings.Contains(got, "image: nginxproxy/acme-companion") || !strings.Contains(got, "NOTE=jrcs/letsencrypt") || !strings.Contains(got, `"80:8000"`) {
		t.Fatalf("inhalt: %s", got)
	}
	if fi, _ := os.Stat(f); fi.Mode().Perm() != 0o640 {
		t.Fatalf("rechte: %v", fi.Mode())
	}
	if bk, err := os.ReadFile(f + ".roster-bak-r2"); err != nil || string(bk) != orig {
		t.Fatalf("backup muss den Originalstand enthalten: %v %q", err, bk)
	}
}

func TestAutoRollbackOnFailure(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "app.conf")
	os.WriteFile(f, []byte("listen 8000\n"), 0o644)
	g := shared.FlowGraph{Nodes: []shared.FlowNode{
		node("repl", "replace", map[string]any{"files": f, "search": `listen (\d+)`, "replace": "listen 80", "regex": true}),
		node("cmd", "command", map[string]any{"script": "exit 3"}),
	}, Edges: []shared.FlowEdge{{From: "repl", Port: "ok", To: "cmd"}}}
	run := func(ctx context.Context, shell, script string) (int, string) { return 3, "kaputt" }
	res := Run(context.Background(), g, Options{RunID: "r3", RunScript: run})
	if res.Status != "rolled_back" || len(res.RolledBack) != 1 {
		t.Fatalf("%+v", res)
	}
	if b, _ := os.ReadFile(f); string(b) != "listen 8000\n" {
		t.Fatalf("nicht zurückgerollt: %q", b)
	}
	// Mit fail-Zweig auf einen Rollback-Baustein: Status rolled_back, Zweig ausgeführt.
	g.Nodes = append(g.Nodes, node("rb", "rollback", nil))
	g.Edges = append(g.Edges, shared.FlowEdge{From: "cmd", Port: "fail", To: "rb"})
	res = Run(context.Background(), g, Options{RunID: "r4", RunScript: run})
	if res.Status != "rolled_back" || len(res.Nodes) != 3 || res.Nodes[2].Status != "ok" {
		t.Fatalf("zweig: %+v", res)
	}
}

func TestFindEmptyMeansNothing(t *testing.T) {
	g := shared.FlowGraph{Nodes: []shared.FlowNode{node("find", "find_files", map[string]any{"paths": "/nonexistent/*.yml"}), node("w", "wait", nil)},
		Edges: []shared.FlowEdge{{From: "find", Port: "ok", To: "w"}}}
	if res := Run(context.Background(), g, Options{RunID: "x"}); res.Status != "nothing" || len(res.Nodes) != 1 {
		t.Fatalf("%+v", res)
	}
}

func TestHTTPValueIntoReplace(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"data":{"releases":[{"tag":"2.4.1"}]}}`))
	}))
	defer srv.Close()
	dir := t.TempDir()
	f := filepath.Join(dir, ".env")
	os.WriteFile(f, []byte("APP_TAG=2.4.0\n"), 0o644)
	g := shared.FlowGraph{Nodes: []shared.FlowNode{
		node("api", "http", map[string]any{"url": srv.URL, "json_path": "data.releases.0.tag"}),
		node("chk", "check", map[string]any{"kind": "expr", "value": "{{steps.api.value}}", "op": "notempty"}),
		node("rep", "replace", map[string]any{"files": f, "search": `APP_TAG=.*`, "replace": "APP_TAG={{steps.api.json.data.releases.0.tag}}", "regex": true}),
	}, Edges: []shared.FlowEdge{{From: "api", Port: "ok", To: "chk"}, {From: "chk", Port: "ok", To: "rep"}}}
	res := Run(context.Background(), g, Options{RunID: "h", DryRun: true})
	if res.Status != "dry" || !strings.Contains(res.Nodes[2].Diff, "+APP_TAG=2.4.1") {
		t.Fatalf("%+v", res)
	}
}

func TestImageMatchingAndInspect(t *testing.T) {
	cases := []struct {
		ref, pat string
		want     bool
	}{
		{"jrcs/letsencrypt-nginx-proxy-companion", "jrcs/letsencrypt-nginx-proxy-companion", true},
		{"jrcs/letsencrypt-nginx-proxy-companion:latest", "jrcs/letsencrypt-nginx-proxy-companion", true},
		{"docker.io/library/nginx:1.25", "nginx", true},
		{"nginx:1.25", "nginx:1.27", false},
		{"nginx", "nginx:latest", true},
		{"registry:5000/app:1", "registry:5000/app", true},
		{"nginxproxy/nginx-proxy", "nginx", false},
		{"registry.thomas-peterson.de/printshopcreator/docker/php-fpm-prod:2.4.0_tp", "php-fpm-prod", true},
		{"registry.thomas-peterson.de/printshopcreator/docker/php-fpm-prod:2.4.0", "printshopcreator/docker/php-fpm-prod", false},
		{"registry.thomas-peterson.de/printshopcreator/docker/php-fpm-prod:2.4.0", "*/php-fpm-prod", true},
		{"registry.thomas-peterson.de/printshopcreator/docker/php-fpm-prod:2.4.0", "php-fpm-*", true},
		{"registry.thomas-peterson.de/printshopcreator/docker/php-cron-prod:2.4.0", "php-fpm-prod", false},
		{"registry.x/a/php-fpm-prod:2.4.0", "php-fpm-prod:2.4.0", true},
		{"registry.x/a/php-fpm-prod:2.4.0_tp", "php-fpm-prod:2.4.0", false},
	}
	for _, c := range cases {
		if got := imageMatches(c.ref, c.pat); got != c.want {
			t.Errorf("%s ~ %s: %v", c.ref, c.pat, got)
		}
	}
	out := "jrcs/letsencrypt-nginx-proxy-companion|psc|/opt/psc|/opt/psc/docker-compose.yml,override.yml|companion\nnginxproxy/nginx-proxy|psc|/opt/psc|/opt/psc/docker-compose.yml|proxy\njrcs/letsencrypt-nginx-proxy-companion||||\n"
	ps := groupProjects(parseInspect(out), "jrcs/letsencrypt-nginx-proxy-companion")
	if len(ps) != 1 || ps[0].Project != "psc" || len(ps[0].Files) != 2 || ps[0].Files[1] != "/opt/psc/override.yml" || len(ps[0].Services) != 1 || ps[0].Services[0] != "companion" {
		t.Fatalf("%+v", ps)
	}
	args := strings.Join(composeArgs(ps[0], "up", nil, false), " ")
	if args != "-p psc -f /opt/psc/docker-compose.yml -f /opt/psc/override.yml --project-directory /opt/psc up -d companion" {
		t.Fatal(args)
	}
}

func TestChecksAndHelpers(t *testing.T) {
	if ok, _ := containerState("psc-companion-1|running|Up 2 hours\npsc-web-1|running|Up 2 hours (unhealthy)", "companion", true); !ok {
		t.Fatal("companion")
	}
	if ok, _ := containerState("psc-web-1|running|Up 2 hours (unhealthy)", "web", true); ok {
		t.Fatal("unhealthy")
	}
	if ok, _ := containerState("psc-web-1|exited|Exited (1)", "web", false); ok {
		t.Fatal("exited")
	}
	for code, exp := range map[int]string{200: "", 301: "200,301", 404: "4xx"} {
		if !statusOK(code, exp) {
			t.Errorf("%d %q", code, exp)
		}
	}
	if statusOK(500, "") {
		t.Fatal("500")
	}
	if ok, _ := compare("10", "gt", "9"); !ok {
		t.Fatal("gt")
	}
	d := unifiedDiff("f", "a\nb\nc\nd\ne\nf\ng\nh\ni\nj\n", "a\nb\nc\nd\nX\nf\ng\nh\ni\nj\n")
	if !strings.Contains(d, "-e\n+X\n") || strings.Contains(d, " a\n") {
		t.Fatalf("diff: %s", d)
	}
}

func TestProjectsFromFiles(t *testing.T) {
	labels := []containerInfo{{Image: "jrcs/letsencrypt-nginx-proxy-companion", Project: "letsencrypt", Dir: "/data/composer/letsencrypt", Service: "letsencrypt"}}
	ps := projectsFromFiles([]string{"/data/composer/letsencrypt/docker-compose.yml", "/data/composer/letsencrypt/.env", "/srv/app/compose.yaml", "/srv/app/app.conf"}, labels)
	if len(ps) != 2 || ps[0].Project != "letsencrypt" || ps[0].Dir != "/data/composer/letsencrypt" || len(ps[0].Files) != 1 || ps[1].Project != "" || ps[1].Dir != "/srv/app" {
		t.Fatalf("%+v", ps)
	}
	args := strings.Join(composeArgs(ps[0], "pull", nil, false), " ")
	if args != "-p letsencrypt -f /data/composer/letsencrypt/docker-compose.yml --project-directory /data/composer/letsencrypt pull" {
		t.Fatal(args)
	}
	if !isComposeFile("/x/docker-compose.prod.yml") || isComposeFile("/x/app.yml") {
		t.Fatal("isComposeFile")
	}
}

// Pfad-Modus: replace auf einer Compose-Datei, danach compose (Vorschau) ohne Verzeichnis.
func TestComposeAfterPathFind(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "docker-compose.yml")
	os.WriteFile(f, []byte("services:\n  le:\n    image: jrcs/letsencrypt-nginx-proxy-companion\n"), 0o644)
	g := shared.FlowGraph{Nodes: []shared.FlowNode{
		node("find", "find_files", map[string]any{"mode": "paths", "paths": dir + "/*"}),
		node("repl", "replace", map[string]any{"search": "jrcs/letsencrypt-nginx-proxy-companion", "replace": "nginxproxy/acme-companion", "line_filter": "image:"}),
		node("pull", "compose", map[string]any{"action": "pull"}),
	}, Edges: []shared.FlowEdge{{From: "find", Port: "ok", To: "repl"}, {From: "repl", Port: "ok", To: "pull"}}}
	res := Run(context.Background(), g, Options{RunID: "p", DryRun: true})
	if res.Status != "dry" || !strings.Contains(res.Nodes[2].Output, "--project-directory "+dir+" pull") {
		t.Fatalf("%+v", res)
	}
}

func TestBackupsIgnoredAndUnchangedStops(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "docker-compose.yml")
	os.WriteFile(f, []byte("image: old/img\n"), 0o644)
	g := shared.FlowGraph{Nodes: []shared.FlowNode{
		node("find", "find_files", map[string]any{"mode": "paths", "paths": dir + "/*"}),
		node("repl", "replace", map[string]any{"search": "old/img", "replace": "new/img"}),
		node("cmd", "command", map[string]any{"script": "echo nachher"}),
	}, Edges: []shared.FlowEdge{{From: "find", Port: "ok", To: "repl"}, {From: "repl", Port: "ok", To: "cmd"}}}
	ran := 0
	run := func(ctx context.Context, shell, script string) (int, string) { ran++; return 0, "ok" }
	if res := Run(context.Background(), g, Options{RunID: "a1", RunScript: run}); res.Status != "ok" || ran != 1 {
		t.Fatalf("erster Lauf: %+v", res)
	}
	// Zweiter Lauf: Backup liegt daneben und enthält noch old/img – darf nicht gefunden werden;
	// die eigentliche Datei hat nichts mehr zu ersetzen -> Ablauf endet, Befehl läuft nicht.
	res := Run(context.Background(), g, Options{RunID: "a2", RunScript: run})
	if res.Status != "nothing" || ran != 1 || len(res.Nodes) != 2 {
		t.Fatalf("zweiter Lauf: %+v", res)
	}
	if len(res.Nodes[0].Files) != 1 || res.Nodes[0].Files[0] != f {
		t.Fatalf("backup als Treffer: %v", res.Nodes[0].Files)
	}
	if _, err := os.Stat(f + ".roster-bak-a2"); err == nil {
		t.Fatal("Backup vom Backup angelegt")
	}
	// Mit continue_if_unchanged läuft der Rest trotzdem.
	g.Nodes[1].Config["continue_if_unchanged"] = true
	if res := Run(context.Background(), g, Options{RunID: "a3", RunScript: run}); res.Status != "ok" || ran != 2 {
		t.Fatalf("weiter trotz unverändert: %+v", res)
	}
}
