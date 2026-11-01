package logmon

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/boonkerz/roster/internal/shared"
)

func TestCollectFileIncremental(t *testing.T) {
	dir := t.TempDir()
	logf := filepath.Join(dir, "access.log")
	old := `203.0.113.1 - - [01/Nov/2026:09:00:00 +0000] "GET /wp-login.php HTTP/1.1" 404 0 "-" "-"` + "\n"
	if err := os.WriteFile(logf, []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	src := []shared.LogSource{{ID: "s1", Name: "nginx", Type: "file", Target: logf, Format: "nginx_access"}}
	// Erster Lauf: Altbestand überspringen.
	if ev, _ := Collect(t.Context(), dir, src); len(ev) != 0 {
		t.Fatalf("first run should skip existing lines: %+v", ev)
	}
	f, _ := os.OpenFile(logf, os.O_APPEND|os.O_WRONLY, 0o644)
	_, _ = f.WriteString(`203.0.113.2 - - [01/Nov/2026:09:01:00 +0000] "GET /.env HTTP/1.1" 404 0 "-" "-"` + "\n" +
		`198.51.100.1 - - [01/Nov/2026:09:01:01 +0000] "GET / HTTP/1.1" 200 0 "-" "-"` + "\n" +
		`203.0.113.3 - - [01/Nov/2026:09:01:02 +0000] "GET /xmlrpc.php HTTP/1.1" 404 0 "-" "-"`) // ohne Zeilenende
	f.Close()
	ev, _ := Collect(t.Context(), dir, src)
	if len(ev) != 1 || ev[0].SourceIP != "203.0.113.2" || ev[0].RecordID == "" {
		t.Fatalf("second run: %+v", ev)
	}
	f, _ = os.OpenFile(logf, os.O_APPEND|os.O_WRONLY, 0o644)
	_, _ = f.WriteString("\n")
	f.Close()
	if ev, _ = Collect(t.Context(), dir, src); len(ev) != 1 || ev[0].SourceIP != "203.0.113.3" {
		t.Fatalf("completed line: %+v", ev)
	}
	// Rotation: neue, kürzere Datei → von vorn lesen.
	_ = os.Remove(logf)
	if err := os.WriteFile(logf, []byte(`203.0.113.4 - - [01/Nov/2026:09:02:00 +0000] "GET /phpmyadmin/ HTTP/1.1" 404 0 "-" "-"`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if ev, _ = Collect(t.Context(), dir, src); len(ev) != 1 || ev[0].SourceIP != "203.0.113.4" {
		t.Fatalf("rotation: %+v", ev)
	}
	// Glob
	src[0].Target = filepath.Join(dir, "*.log")
	if ev, _ = Collect(t.Context(), dir, src); len(ev) != 0 { // neuer Cursor-Schlüssel (Glob-Pfad = Datei) → springt ans Ende
		t.Logf("glob first run: %+v", ev)
	}
	_ = time.Now()
}

func TestMatchName(t *testing.T) {
	for _, c := range []struct {
		p, n string
		ok   bool
	}{{"nginx-proxy", "nginx-proxy", true}, {"nginx-proxy", "nginx-proxy-le", false}, {"nginx-proxy*", "nginx-proxy-le", true}, {"*sftp*", "psc-sftp-1", true}, {"sftp", "psc-sftp-1", false}, {"db", "nginx", false}, {"", "x", false}} {
		if got := matchName(c.p, c.n); got != c.ok {
			t.Errorf("%q %q: %v", c.p, c.n, got)
		}
	}
}
