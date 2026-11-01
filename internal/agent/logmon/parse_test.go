package logmon

import (
	"testing"
	"time"

	"github.com/boonkerz/roster/internal/shared"
)

func TestUnwrap(t *testing.T) {
	l := Unwrap(`{"log":"nginx.1    | shop.example.com 203.0.113.5 - - [01/Nov/2026:09:12:33 +0000] \"GET /.env HTTP/1.1\" 404 153 \"-\" \"curl\"\n","stream":"stdout","time":"2026-11-01T09:12:33.123456789Z"}`)
	if l.Text != `shop.example.com 203.0.113.5 - - [01/Nov/2026:09:12:33 +0000] "GET /.env HTTP/1.1" 404 153 "-" "curl"` {
		t.Fatalf("text: %q", l.Text)
	}
	if l.Time.IsZero() || l.Time.Year() != 2026 {
		t.Errorf("time: %v", l.Time)
	}
	l = Unwrap("2026-11-01T09:12:34.000000000Z Failed password for invalid user admin from 203.0.113.9 port 1 ssh2\n")
	if l.Text != "Failed password for invalid user admin from 203.0.113.9 port 1 ssh2" || l.Time.IsZero() {
		t.Fatalf("docker logs: %+v", l)
	}
}

func TestNginxAccess(t *testing.T) {
	src := shared.LogSource{Name: "nginx-proxy", Format: "nginx_access"}
	now := time.Now()
	cases := []struct {
		line, kind, ip, detail string
	}{
		{`shop.example.com 203.0.113.5 - - [01/Nov/2026:09:12:33 +0000] "GET /wp-login.php HTTP/1.1" 404 153 "-" "Mozilla/5.0" "10.0.0.2:80"`, "probe", "203.0.113.5", "/wp-login.php"},
		{`203.0.113.6 - - [01/Nov/2026:09:12:34 +0000] "GET /.env HTTP/1.1" 404 153 "-" "curl"`, "probe", "203.0.113.6", "/.env"},
		{`203.0.113.7 - admin [01/Nov/2026:09:12:35 +0000] "GET /admin/ HTTP/1.1" 401 0 "-" "x"`, "login_failed", "203.0.113.7", "/admin/"},
		{`203.0.113.8 - - [01/Nov/2026:09:12:36 +0000] "\x16\x03\x01" 400 0 "-" "-"`, "probe", "203.0.113.8", `\x16\x03\x01`},
		{`203.0.113.9 - - [01/Nov/2026:09:12:37 +0000] "GET /index.php?id=1%20union%20select%201 HTTP/1.1" 200 10 "-" "-"`, "probe", "203.0.113.9", ""},
		{`203.0.113.10 - - [01/Nov/2026:09:12:38 +0000] "GET /old/site.php HTTP/1.1" 404 10 "-" "-"`, "probe", "203.0.113.10", "/old/site.php"},
		{`2001:db8::7 - - [01/Nov/2026:09:12:39 +0000] "GET /xmlrpc.php HTTP/1.1" 405 10 "-" "-"`, "probe", "2001:db8::7", "/xmlrpc.php"},
	}
	for _, c := range cases {
		ev, ok := Parse(src, Line{Text: c.line}, now)
		if !ok {
			t.Errorf("not parsed: %s", c.line)
			continue
		}
		if ev.Kind != c.kind || ev.SourceIP != c.ip || ev.Service != "nginx-proxy" || (c.detail != "" && ev.Detail != c.detail) {
			t.Errorf("%s → %+v", c.line, ev)
		}
		if ev.Time.Year() != 2026 || ev.Time.Hour() != 9 {
			t.Errorf("time not from log: %v", ev.Time)
		}
	}
	if ev, ok := Parse(src, Line{Text: `203.0.113.7 - admin [01/Nov/2026:09:12:35 +0000] "GET /admin/ HTTP/1.1" 401 0 "-" "x"`}, now); !ok || ev.User != "admin" {
		t.Errorf("user: %+v", ev)
	}
	// Normale Zugriffe und 404 auf gewöhnliche Pfade sind kein Ereignis.
	for _, line := range []string{
		`198.51.100.1 - - [01/Nov/2026:09:12:40 +0000] "GET /products/shirt HTTP/1.1" 200 5000 "-" "Mozilla"`,
		`198.51.100.1 - - [01/Nov/2026:09:12:41 +0000] "GET /images/missing.png HTTP/1.1" 404 0 "-" "Mozilla"`,
		`198.51.100.1 - - [01/Nov/2026:09:12:42 +0000] "POST /checkout HTTP/2.0" 302 0 "-" "Mozilla"`,
		`198.51.100.1 - - [01/Nov/2026:09:12:43 +0000] "GET /.well-known/acme-challenge/abc HTTP/1.1" 200 87 "-" "Let's Encrypt"`,
	} {
		if ev, ok := Parse(src, Line{Text: line}, now); ok {
			t.Errorf("false positive: %s → %+v", line, ev)
		}
	}
}

func TestNginxError(t *testing.T) {
	src := shared.LogSource{Name: "nginx", Format: "nginx_error"}
	ev, ok := Parse(src, Line{Text: `2026/11/01 09:13:01 [error] 29#29: *123 user "admin": password mismatch, client: 203.0.113.5, server: shop.example.com, request: "GET /admin HTTP/1.1", host: "shop.example.com"`}, time.Now())
	if !ok || ev.Kind != "login_failed" || ev.User != "admin" || ev.SourceIP != "203.0.113.5" || ev.Detail != "/admin" {
		t.Fatalf("%+v %v", ev, ok)
	}
	ev, ok = Parse(src, Line{Text: `2026/11/01 09:13:02 [error] 29#29: *124 open() "/usr/share/nginx/html/wp-login.php" failed (2: No such file or directory), client: 203.0.113.6, server: _, request: "GET /wp-login.php HTTP/1.1"`}, time.Now())
	if !ok || ev.Kind != "probe" || ev.SourceIP != "203.0.113.6" {
		t.Fatalf("%+v %v", ev, ok)
	}
	if _, ok = Parse(src, Line{Text: `2026/11/01 09:13:03 [warn] 29#29: *125 an upstream response is buffered, client: 198.51.100.1, server: shop.example.com, request: "GET /big HTTP/1.1"`}, time.Now()); ok {
		t.Fatal("warn should be ignored")
	}
}

func TestSSHDContainer(t *testing.T) {
	src := shared.LogSource{Name: "sftp", Format: "sshd"}
	now := time.Now()
	ev, ok := Parse(src, Line{Text: "Failed password for invalid user admin from 203.0.113.9 port 40212 ssh2"}, now)
	if !ok || ev.Kind != "invalid_user" || ev.User != "admin" || ev.SourceIP != "203.0.113.9" || ev.Service != "sftp" {
		t.Fatalf("%+v %v", ev, ok)
	}
	ev, ok = Parse(src, Line{Text: "Accepted password for shop1 from 198.51.100.4 port 1 ssh2"}, now)
	if !ok || ev.Kind != "login_ok" || ev.User != "shop1" {
		t.Fatalf("%+v %v", ev, ok)
	}
	ev, ok = Parse(src, Line{Text: "Nov  1 09:14:00 host sshd[12]: Invalid user test from 203.0.113.9 port 4"}, now)
	if !ok || ev.Kind != "invalid_user" {
		t.Fatalf("syslog prefix: %+v %v", ev, ok)
	}
	if _, ok = Parse(src, Line{Text: "Connection closed by authenticating user root 203.0.113.9 port 5 [preauth]"}, now); ok {
		t.Fatal("preauth close should be ignored")
	}
	if _, ok = Parse(src, Line{Text: "Server listening on 0.0.0.0 port 22."}, now); ok {
		t.Fatal("noise")
	}
}

func TestFTP(t *testing.T) {
	src := shared.LogSource{Name: "ftp", Format: "ftp"}
	now := time.Now()
	cases := []struct{ line, kind, ip, user string }{
		{`Sat Nov  1 09:14:00 2026 [pid 123] [shop1] FAIL LOGIN: Client "203.0.113.7"`, "login_failed", "203.0.113.7", "shop1"},
		{`Sat Nov  1 09:14:01 2026 [pid 124] [shop1] OK LOGIN: Client "198.51.100.2"`, "login_ok", "198.51.100.2", "shop1"},
		{`2026-11-01 09:15:00,000 host proftpd[123] host (203.0.113.8[203.0.113.8]): USER shop (Login failed): Incorrect password`, "login_failed", "203.0.113.8", "shop"},
		{`2026-11-01 09:15:01,000 host proftpd[123] host (203.0.113.8[203.0.113.8]): USER bob: no such user found from 203.0.113.8 [203.0.113.8] to 10.0.0.1:21`, "invalid_user", "203.0.113.8", "bob"},
		{`pure-ftpd: (?@203.0.113.6) [WARNING] Authentication failed for user [shop]`, "login_failed", "203.0.113.6", "shop"},
		{`pure-ftpd: (?@198.51.100.9) [INFO] shop is now logged in`, "login_ok", "198.51.100.9", "shop"},
	}
	for _, c := range cases {
		ev, ok := Parse(src, Line{Text: c.line}, now)
		if !ok || ev.Kind != c.kind || ev.SourceIP != c.ip || ev.User != c.user {
			t.Errorf("%s → %+v %v", c.line, ev, ok)
		}
	}
}

func TestRegex(t *testing.T) {
	src := shared.LogSource{Name: "app", Format: "regex", Kind: "login_failed", Pattern: `login failed for (?P<user>\S+) from (?P<ip>[0-9.]+)`}
	ev, ok := Parse(src, Line{Text: "2026-11-01 app: login failed for mia from 203.0.113.3"}, time.Now())
	if !ok || ev.Kind != "login_failed" || ev.User != "mia" || ev.SourceIP != "203.0.113.3" || ev.Service != "app" {
		t.Fatalf("%+v %v", ev, ok)
	}
	if _, ok = Parse(src, Line{Text: "nothing"}, time.Now()); ok {
		t.Fatal("no match expected")
	}
	bad := shared.LogSource{Name: "x", Format: "regex", Kind: "probe", Pattern: `(`}
	if _, ok = Parse(bad, Line{Text: "("}, time.Now()); ok {
		t.Fatal("invalid pattern must not match")
	}
}
