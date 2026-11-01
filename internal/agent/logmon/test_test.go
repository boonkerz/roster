package logmon

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/boonkerz/roster/internal/shared"
)

func TestTestFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "access.log")
	lines := []string{
		`203.0.113.9 - - [01/Nov/2026:10:00:00 +0000] "GET / HTTP/1.1" 200 512 "-" "Mozilla"`,
		`203.0.113.9 - - [01/Nov/2026:10:00:01 +0000] "GET /wp-login.php HTTP/1.1" 404 0 "-" "Mozilla"`,
		`203.0.113.10 - - [01/Nov/2026:10:00:02 +0000] "GET /admin HTTP/1.1" 401 0 "-" "curl"`,
	}
	os.WriteFile(p, []byte(strings.Join(lines, "\n")+"\n"), 0o644)
	src := shared.LogSource{ID: "x", Name: "web", Type: "file", Target: filepath.Join(dir, "*.log"), Format: "nginx_access"}
	res := Test(t.Context(), src, 100)
	if res.Error != "" || len(res.Targets) != 1 || res.Lines != 3 || res.Matched != 2 || len(res.Events) != 2 {
		t.Fatalf("unerwartet: %+v", res)
	}
	// Nur die letzten n Zeilen
	res = Test(t.Context(), src, 1)
	if res.Lines != 1 || res.Matched != 1 || res.Events[0].SourceIP != "203.0.113.10" {
		t.Fatalf("tail: %+v", res)
	}
	// Fehlende Datei
	res = Test(t.Context(), shared.LogSource{Type: "file", Target: filepath.Join(dir, "nope.log"), Format: "nginx_access"}, 10)
	if res.Error == "" {
		t.Fatal("Fehler erwartet")
	}
	// Collect meldet Status
	_, st := Collect(t.Context(), dir, []shared.LogSource{src, {ID: "y", Type: "file", Target: filepath.Join(dir, "nope.log"), Format: "sshd"}})
	if len(st) != 2 || st[0].Targets != 1 || st[0].Error != "" || st[1].Targets != 0 || st[1].Error == "" {
		t.Fatalf("status: %+v", st)
	}
}
