package flow

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/boonkerz/roster/internal/shared"
)

const composeFixture = `# Shop ahrweiler
services:
  web:
    image: registry.example.com/shop/web-prod:2.4.0   # Frontend
    restart: always
  php-fpm:
    image: "registry.example.com/shop/php-fpm-prod:2.4.0"
    volumes:
      - ./data:/var/www/data
      - ./logs:/var/www/logs   # Logs
    environment:
      APP_ENV: prod
  cron:
    image: 'registry.example.com/shop/php-cron-prod:2.4.0'

volumes:
  dbdata: {}
`

func TestVersionCompare(t *testing.T) {
	cases := []struct {
		a, b string
		want int
		ok   bool
	}{
		{"2.4.0", "2.5.0", -1, true}, {"2.10.0", "2.9.1", 1, true}, {"v2.5.0", "2.5.0", 0, true},
		{"2.5.0-rc1", "2.5.0", -1, true}, {"2.5", "2.5.0", 0, true}, {"latest", "2.5.0", 0, false}, {"2.4.0_tp", "2.5.0", -1, true}, {"2.5.0_tp", "2.5.0", 0, true}, {"2.2_tp", "2.5.0", -1, true},
	}
	for _, c := range cases {
		got, ok := compareVersions(c.a, c.b)
		if ok != c.ok || (ok && got != c.want) {
			t.Errorf("%s vs %s: %d %v", c.a, c.b, got, ok)
		}
	}
	if ok, _ := compare("2.4.0", "version_lt", "2.5.0"); !ok {
		t.Fatal("version_lt")
	}
}

func TestPathsAndImage(t *testing.T) {
	if s, _ := splitPath(`services."a.b".image`); len(s) != 3 || s[1] != "a.b" {
		t.Fatal(s)
	}
	if p := expandBraces("services.{web, php-fpm,cron}.image"); len(p) != 3 || p[1] != "services.php-fpm.image" {
		t.Fatal(p)
	}
	if imageTag("host:5000/x/app:1.2") != "1.2" || imageTag("host:5000/x/app") != "latest" || imageRepo("host:5000/x/app:1.2") != "host:5000/x/app" {
		t.Fatal("image")
	}
	if v, ok := envLookup([]byte("# x\nexport APP_TAG=\"2.4.1\"\nOTHER=1 # c\n"), "APP_TAG"); !ok || v != "2.4.1" {
		t.Fatal(v)
	}
}

func apply(t *testing.T, text, op, path, value string) string {
	t.Helper()
	segs, err := splitPath(path)
	if err != nil {
		t.Fatal(err)
	}
	out, _, err := applyOp(text, op, segs, value)
	if err != nil {
		t.Fatalf("%s %s: %v", op, path, err)
	}
	if _, err := parseYAML([]byte(out)); err != nil {
		t.Fatalf("%s %s: kaputtes YAML: %v\n%s", op, path, err, out)
	}
	return out
}

func TestYAMLSurgicalEdits(t *testing.T) {
	txt := composeFixture
	// set: nur der Wert ändert sich, Kommentar und Anführungszeichen bleiben.
	txt = apply(t, txt, "set", "services.web.image", "{{value.repo}}:2.5.0")
	txt = apply(t, txt, "set", "services.php-fpm.image", "{{value.repo}}:2.5.0")
	txt = apply(t, txt, "set", "services.cron.image", "{{value.repo}}:2.5.0")
	for _, want := range []string{
		"    image: registry.example.com/shop/web-prod:2.5.0   # Frontend",
		`    image: "registry.example.com/shop/php-fpm-prod:2.5.0"`,
		`    image: 'registry.example.com/shop/php-cron-prod:2.5.0'`,
		"# Shop ahrweiler", "      - ./logs:/var/www/logs   # Logs",
	} {
		if !strings.Contains(txt, want) {
			t.Fatalf("fehlt %q:\n%s", want, txt)
		}
	}
	// append an vorhandene Liste (richtige Einrückung), an fehlende Liste (cron), Volume ensure.
	txt = apply(t, txt, "append", "services.php-fpm.volumes", "shop_media:/var/www/media")
	txt = apply(t, txt, "append", "services.cron.volumes", "shop_media:/var/www/media")
	txt = apply(t, txt, "ensure", "volumes.shop_media", "{}")
	for _, want := range []string{
		"      - ./logs:/var/www/logs   # Logs\n      - shop_media:/var/www/media\n    environment:",
		"    image: 'registry.example.com/shop/php-cron-prod:2.5.0'\n    volumes:\n      - shop_media:/var/www/media",
		"volumes:\n  dbdata: {}\n  shop_media: {}",
	} {
		if !strings.Contains(txt, want) {
			t.Fatalf("fehlt %q:\n%s", want, txt)
		}
	}
	// Zweiter Durchgang ändert nichts (idempotent).
	again := txt
	for _, o := range [][3]string{{"set", "services.web.image", "{{value.repo}}:2.5.0"}, {"append", "services.php-fpm.volumes", "shop_media:/var/www/media"}, {"ensure", "volumes.shop_media", "{}"}} {
		again = apply(t, again, o[0], o[1], o[2])
	}
	if again != txt {
		t.Fatalf("nicht idempotent:\n%s", again)
	}
	// delete Map-Eintrag und Listeneintrag.
	d := apply(t, txt, "delete", "services.php-fpm.environment", "")
	if strings.Contains(d, "APP_ENV") || !strings.Contains(d, "  cron:") {
		t.Fatalf("delete:\n%s", d)
	}
	d = apply(t, txt, "delete", "services.php-fpm.volumes", "./data:/var/www/data")
	if strings.Contains(d, "./data:/var/www/data") || !strings.Contains(d, "./logs") {
		t.Fatalf("delete item:\n%s", d)
	}
	// Leerer Wert wird zu Block (volumes: null / {}).
	e := apply(t, "services:\n  app:\n    image: x\n    volumes:\nvolumes: {}\n", "append", "services.app.volumes", "a:/b")
	e = apply(t, e, "ensure", "volumes.data", "{}")
	if !strings.Contains(e, "    volumes:\n      - a:/b\n") || !strings.Contains(e, "volumes:\n  data: {}") {
		t.Fatalf("leer:\n%s", e)
	}
	// Kurzform-Liste wird nicht umgeschrieben.
	if _, _, err := applyOp("a:\n  v: [x, y]\n", "append", []string{"a", "v"}, "z"); err == nil || !strings.Contains(err.Error(), "Kurzform") {
		t.Fatalf("flow: %v", err)
	}
}

// Der Ablauf aus der Anfrage: Version lesen, < 2.5.0 prüfen, sonst ohne Fehler enden.
func TestUpgradeFlow(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "docker-compose.yml")
	os.WriteFile(f, []byte(composeFixture), 0o644)
	g := shared.FlowGraph{Nodes: []shared.FlowNode{
		node("find", "find_files", map[string]any{"mode": "paths", "paths": f}),
		node("ver", "read_value", map[string]any{"path": "services.php-fpm.image", "extract": "image_tag"}),
		node("chk", "check", map[string]any{"kind": "expr", "value": "{{steps.ver.value}}", "op": "version_lt", "expect": "{{param.VERSION}}", "false_ends": true}),
		node("edit", "yaml", map[string]any{"ops": "set services.{{{param.SERVICES}}}.image = {{value.repo}}:{{param.VERSION}}\nappend services.php-fpm.volumes = shop_media:/var/www/media\nensure volumes.shop_media = {}"}),
		node("cmd", "command", map[string]any{"script": "docker compose up -d"}),
	}, Edges: []shared.FlowEdge{{From: "find", Port: "ok", To: "ver"}, {From: "ver", Port: "ok", To: "chk"}, {From: "chk", Port: "ok", To: "edit"}, {From: "edit", Port: "ok", To: "cmd"}}}
	params := map[string]string{"VERSION": "2.5.0", "SERVICES": "web,php-fpm,cron"}
	ran := 0
	run := func(ctx context.Context, shell, script string) (int, string) { ran++; return 0, "" }

	res := Run(context.Background(), g, Options{RunID: "u0", DryRun: true, Params: params})
	if res.Status != "dry" || !strings.Contains(res.Nodes[3].Diff, "+    image: registry.example.com/shop/web-prod:2.5.0") {
		t.Fatalf("vorschau: %+v", res)
	}
	res = Run(context.Background(), g, Options{RunID: "u1", Params: params, RunScript: run})
	if res.Status != "ok" || ran != 1 {
		t.Fatalf("lauf: %+v", res)
	}
	b, _ := os.ReadFile(f)
	if strings.Count(string(b), ":2.5.0") != 3 || !strings.Contains(string(b), "shop_media: {}") {
		t.Fatalf("datei:\n%s", b)
	}
	// Jetzt 2.5.0 -> Bedingung nicht erfüllt -> nicht betroffen, Befehl läuft nicht.
	res = Run(context.Background(), g, Options{RunID: "u2", Params: params, RunScript: run})
	if res.Status != "nothing" || ran != 1 || len(res.Nodes) != 3 {
		t.Fatalf("zweiter lauf: %+v", res)
	}
}

func TestSuffixKeptOnUpgrade(t *testing.T) {
	in := "services:\n  web:\n    image: reg/x/web-prod:2.4.0_tp\n  cron:\n    image: reg/x/php-cron-prod:2.4.0\n"
	out := apply(t, in, "set", "services.web.image", "{{value.repo}}:2.5.0{{value.suffix}}")
	out = apply(t, out, "set", "services.cron.image", "{{value.repo}}:2.5.0{{value.suffix}}")
	if !strings.Contains(out, "reg/x/web-prod:2.5.0_tp") || !strings.Contains(out, "reg/x/php-cron-prod:2.5.0\n") {
		t.Fatal(out)
	}
}
