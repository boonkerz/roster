package flow

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/boonkerz/roster/internal/shared"
)

func TestFSBlockAndRollback(t *testing.T) {
	base := t.TempDir()
	vol := filepath.Join(base, "volumes", "storefront")
	keep := filepath.Join(vol, ".keep")
	ops := "mkdir " + vol + " mode=0770 owner=" + itoa(os.Getuid()) + "\nfile " + keep + " = \"x\\n\""
	g := shared.FlowGraph{Nodes: []shared.FlowNode{
		node("fs", "fs", map[string]any{"ops": ops}),
		node("bad", "command", map[string]any{"script": "exit 1"}),
	}, Edges: []shared.FlowEdge{{From: "fs", Port: "ok", To: "bad"}}}
	fail := func(ctx context.Context, shell, script string) (int, string) { return 1, "kaputt" }

	// Vorschau ändert nichts.
	res := Run(context.Background(), g, Options{RunID: "p", DryRun: true})
	if res.Status != "dry" || !strings.Contains(res.Nodes[0].Output, "würde") {
		t.Fatalf("%+v", res)
	}
	if _, err := os.Stat(filepath.Join(base, "volumes")); err == nil {
		t.Fatal("Vorschau hat angelegt")
	}
	// Fehler danach -> Rückbau entfernt Datei und neu angelegte Verzeichnisse.
	res = Run(context.Background(), g, Options{RunID: "r", RunScript: fail})
	if res.Status != "rolled_back" || len(res.RolledBack) != 3 {
		t.Fatalf("rollback: %+v", res)
	}
	if _, err := os.Stat(filepath.Join(base, "volumes")); err == nil {
		t.Fatal("Verzeichnis nicht zurückgebaut")
	}
	// Ohne Fehler: angelegt mit Modus; zweiter Lauf: nichts zu tun.
	g.Nodes = g.Nodes[:1]
	g.Edges = nil
	if res := Run(context.Background(), g, Options{RunID: "a"}); res.Status != "ok" {
		t.Fatalf("%+v", res)
	}
	if fi, err := os.Stat(vol); err != nil || fi.Mode().Perm() != 0o770 {
		t.Fatalf("mode: %v %v", fi.Mode(), err)
	}
	if b, _ := os.ReadFile(keep); string(b) != "x\n" {
		t.Fatalf("inhalt %q", b)
	}
	res = Run(context.Background(), g, Options{RunID: "b"})
	if res.Status != "ok" || !strings.HasPrefix(res.Nodes[0].Output, "0 Änderung") {
		t.Fatalf("zweiter lauf: %+v", res)
	}
	// chmod mit Rückbau; vorhandenes, nicht leeres Verzeichnis bleibt bei Rollback.
	g2 := shared.FlowGraph{Nodes: []shared.FlowNode{
		node("fs", "fs", map[string]any{"ops": "chmod " + vol + " 0755"}),
		node("bad", "command", map[string]any{"script": "exit 1"}),
	}, Edges: []shared.FlowEdge{{From: "fs", Port: "ok", To: "bad"}}}
	res = Run(context.Background(), g2, Options{RunID: "c", RunScript: fail})
	if fi, _ := os.Stat(vol); res.Status != "rolled_back" || fi.Mode().Perm() != 0o770 {
		t.Fatalf("chmod rollback: %+v %v", res, fi.Mode())
	}
	// Relative Pfade und Wurzel abgelehnt.
	for _, bad := range []string{"mkdir relativ/x", "mkdir /"} {
		g3 := shared.FlowGraph{Nodes: []shared.FlowNode{node("fs", "fs", map[string]any{"ops": bad})}}
		if res := Run(context.Background(), g3, Options{RunID: "x", DryRun: true}); res.Status != "fail" {
			t.Errorf("%s: %+v", bad, res)
		}
	}
}

func itoa(i int) string { return strconv.Itoa(i) }

func TestExistsSkipNotify(t *testing.T) {
	dir := t.TempDir()
	g := shared.FlowGraph{Nodes: []shared.FlowNode{
		node("ex", "check", map[string]any{"kind": "exists", "path": dir, "path_type": "dir"}),
		node("skip", "container_exec", map[string]any{"command": "{{param.POST_CMD}}", "service": "php-fpm", "only_if": "{{param.POST_CMD}}"}),
		node("msg", "notify", map[string]any{"subject": "Fertig", "message": "Sicherung {{steps.ex.value}}", "severity": "info"}),
	}, Edges: []shared.FlowEdge{{From: "ex", Port: "ok", To: "skip"}, {From: "skip", Port: "ok", To: "msg"}}}
	res := Run(context.Background(), g, Options{RunID: "n"})
	if res.Status != "ok" || res.Nodes[1].Status != "skipped" || len(res.Notifications) != 1 || res.Notifications[0].Message != "Sicherung true" {
		t.Fatalf("%+v", res)
	}
	if res := Run(context.Background(), g, Options{RunID: "n", DryRun: true}); len(res.Notifications) != 0 {
		t.Fatal("Vorschau verschickt Meldungen")
	}
	g.Nodes[0].Config["path"] = filepath.Join(dir, "fehlt")
	if res := Run(context.Background(), g, Options{RunID: "n"}); res.Status != "fail" {
		t.Fatalf("exists fehlt: %+v", res)
	}
}

func TestContainerHelpers(t *testing.T) {
	cs := parsePS("abc123|shop-php-fpm-1|reg/php-fpm-prod:2.4.0\n\ndef456|shop-db-1|mariadb:11.4\n")
	if len(cs) != 2 || cs[1].Name != "shop-db-1" {
		t.Fatal(cs)
	}
	if a := strings.Join(execArgs("abc", "www-data", "/var/www", "bin/console cache:clear"), " "); a != "exec -u www-data -w /var/www abc sh -c bin/console cache:clear" {
		t.Fatal(a)
	}
	now := time.Date(2026, 11, 9, 0, 0, 0, 0, time.UTC)
	imgs := parseImageList(strings.Join([]string{
		"sha256:aaaaaaaaaaaaaaaa|<none>|<none>|2026-10-01 10:00:00 +0000 UTC|120MB",
		"sha256:bbbbbbbbbbbbbbbb|reg/web-prod|2.4.0|2026-10-01 10:00:00 +0000 UTC|700MB",
		"sha256:cccccccccccccccc|reg/web-prod|2.5.0|2026-11-08 10:00:00 +0000 UTC|700MB",
		"sha256:dddddddddddddddd|<none>|<none>|2026-11-08 10:00:00 +0000 UTC|5MB",
	}, "\n"))
	used := map[string]bool{"cccccccccccc": true}
	if c := pruneCandidates(imgs, used, "dangling", 7*24*time.Hour, now); len(c) != 1 || c[0].ID != "aaaaaaaaaaaa" {
		t.Fatalf("dangling: %+v", c)
	}
	if c := pruneCandidates(imgs, used, "unused", 7*24*time.Hour, now); len(c) != 2 {
		t.Fatalf("unused: %+v", c)
	}
	dir := t.TempDir()
	for _, n := range []string{"db-20261101-010101.sql.gz", "db-20261102-010101.sql.gz", "db-20261103-010101.sql.gz", "other-20261101.sql.gz"} {
		os.WriteFile(filepath.Join(dir, n), []byte("x"), 0o600)
	}
	if rm := pruneDumps(dir, "db-", 2); len(rm) != 1 || rm[0] != "db-20261101-010101.sql.gz" {
		t.Fatal(rm)
	}
	if safeName("/shop db/1") != "shop_db_1" {
		t.Fatal(safeName("/shop db/1"))
	}
}

func TestOnlyIf(t *testing.T) {
	for in, want := range map[string]bool{"x": true, "": false, "false": false, "0": false, "!": true, "!x": false, "! ": true} {
		if got, _ := onlyIf(in); got != want {
			t.Errorf("%q: %v", in, got)
		}
	}
}
