package logmon

import (
	"testing"
	"time"

	"github.com/boonkerz/roster/internal/shared"
)

func parseFmt(format, line string) (shared.AuthEvent, bool) {
	return Parse(shared.LogSource{ID: "s1", Name: "svc", Type: "file", Target: "/x", Format: format}, Unwrap(line+"\n"), time.Now())
}

func TestHAProxy(t *testing.T) {
	ev, ok := parseFmt("haproxy", `Nov  3 10:00:00 lb haproxy[123]: 203.0.113.5:51234 [03/Nov/2026:10:00:00.123] fe be/srv 0/0/0/5/5 404 1234 - - ---- 1/1/0/0/0 0/0 "GET /wp-login.php HTTP/1.1"`)
	if !ok || ev.Kind != "probe" || ev.SourceIP != "203.0.113.5" || ev.Detail != "/wp-login.php" {
		t.Errorf("scanner: %+v %v", ev, ok)
	}
	ev, ok = parseFmt("haproxy", `203.0.113.6:4444 [03/Nov/2026:10:00:01.000] fe fe/<NOSRV> -1/-1/-1/-1/0 400 187 - - PR-- 1/1/0/0/0 0/0 "<BADREQ>"`)
	if !ok || ev.Kind != "probe" || ev.SourceIP != "203.0.113.6" {
		t.Errorf("badreq: %+v %v", ev, ok)
	}
	if _, ok := parseFmt("haproxy", `203.0.113.7:4444 [03/Nov/2026:10:00:02.000] fe be/srv 0/0/0/5/5 200 1234 - - ---- 1/1/0/0/0 0/0 "GET /index.html HTTP/1.1"`); ok {
		t.Error("200 darf nichts liefern")
	}
	if ev, ok := parseFmt("haproxy", `203.0.113.8:4444 [03/Nov/2026:10:00:03.000] fe be/srv 0/0/0/5/5 401 12 - - ---- 1/1/0/0/0 0/0 "GET /admin HTTP/1.1"`); !ok || ev.Kind != "login_failed" {
		t.Errorf("401: %+v", ev)
	}
}

func TestTraefik(t *testing.T) {
	ev, ok := parseFmt("traefik", `{"ClientAddr":"203.0.113.5:51234","ClientHost":"203.0.113.5","DownstreamStatus":404,"RequestHost":"shop.example","RequestMethod":"GET","RequestPath":"/.env","StartUTC":"2026-11-03T10:00:00.5Z","level":"info"}`)
	if !ok || ev.Kind != "probe" || ev.SourceIP != "203.0.113.5" || ev.Detail != "/.env" || ev.Time.Year() != 2026 {
		t.Errorf("json: %+v %v", ev, ok)
	}
	if _, ok := parseFmt("traefik", `{"ClientHost":"203.0.113.5","DownstreamStatus":200,"RequestMethod":"GET","RequestPath":"/","StartUTC":"2026-11-03T10:00:00Z"}`); ok {
		t.Error("200 darf nichts liefern")
	}
	// CLF fällt auf das Combined-Format zurück.
	if ev, ok := parseFmt("traefik", `203.0.113.9 - - [03/Nov/2026:10:00:00 +0000] "GET /wp-admin/ HTTP/1.1" 404 10 "-" "x" 12 "r@file" "http://b" 5ms`); !ok || ev.Kind != "probe" {
		t.Errorf("clf: %+v %v", ev, ok)
	}
}

func TestMySQL(t *testing.T) {
	ev, ok := parseFmt("mysql", `2026-11-03 10:00:00 12 [Warning] Access denied for user 'root'@'203.0.113.5' (using password: YES)`)
	if !ok || ev.Kind != "login_failed" || ev.User != "root" || ev.SourceIP != "203.0.113.5" {
		t.Errorf("denied: %+v %v", ev, ok)
	}
	if ev, ok := parseFmt("mysql", `2026-11-03T10:00:00.000000Z 13 [Note] Host '203.0.113.6' is not allowed to connect to this MySQL server`); !ok || ev.Kind != "login_failed" || ev.SourceIP != "203.0.113.6" {
		t.Errorf("host: %+v %v", ev, ok)
	}
	if ev, ok := parseFmt("mysql", `2026-11-03 10:00:00 14 [Warning] Aborted connection 14 to db: 'unconnected' user: 'unauthenticated' host: '203.0.113.7' (Got an error reading communication packets before authorization)`); !ok || ev.Kind != "probe" {
		t.Errorf("aborted: %+v %v", ev, ok)
	}
	if _, ok := parseFmt("mysql", `2026-11-03 10:00:00 0 [Note] InnoDB: Buffer pool(s) load completed`); ok {
		t.Error("Rauschen")
	}
}

func TestFirewall(t *testing.T) {
	ev, ok := parseFmt("firewall", `Nov  3 10:00:00 srv kernel: [UFW BLOCK] IN=eth0 OUT= MAC=aa:bb SRC=203.0.113.5 DST=10.0.0.2 LEN=40 TOS=0x00 PREC=0x00 TTL=240 ID=54321 PROTO=TCP SPT=4444 DPT=22 WINDOW=1024 RES=0x00 SYN URGP=0`)
	if !ok || ev.Kind != "fw_drop" || ev.SourceIP != "203.0.113.5" || ev.Detail != "tcp/22" {
		t.Errorf("ufw: %+v %v", ev, ok)
	}
	if ev, ok := parseFmt("firewall", `kernel: roster-drop: IN=ens3 OUT= SRC=203.0.113.6 DST=10.0.0.2 PROTO=UDP SPT=5 DPT=161 LEN=20`); !ok || ev.Detail != "udp/161" {
		t.Errorf("nft: %+v %v", ev, ok)
	}
	if _, ok := parseFmt("firewall", `kernel: OUTBOUND: IN= OUT=eth0 SRC=10.0.0.2 DST=203.0.113.5 PROTO=TCP SPT=1 DPT=443`); ok {
		t.Error("ausgehend darf nichts liefern")
	}
	if ev, ok := parseFmt("firewall", `2026-11-03 10:00:00 DROP TCP 203.0.113.7 10.0.0.2 4444 3389 52 S 1 0 0 - - - RECEIVE`); !ok || ev.Kind != "fw_drop" || ev.SourceIP != "203.0.113.7" || ev.Detail != "tcp/3389" {
		t.Errorf("windows: %+v %v", ev, ok)
	}
	if _, ok := parseFmt("firewall", `2026-11-03 10:00:00 ALLOW TCP 10.0.0.3 10.0.0.2 1 80 0 - 0 0 0 - - - RECEIVE`); ok {
		t.Error("ALLOW darf nichts liefern")
	}
	if _, ok := parseFmt("firewall", `2026-11-03 10:00:00 DROP TCP 10.0.0.2 203.0.113.7 1 80 0 - 0 0 0 - - - SEND`); ok {
		t.Error("SEND darf nichts liefern")
	}
}

func TestApacheAccessAlias(t *testing.T) {
	if ev, ok := parseFmt("apache_access", `203.0.113.5 - - [03/Nov/2026:10:00:00 +0000] "GET /phpmyadmin/ HTTP/1.1" 404 196 "-" "Mozilla"`); !ok || ev.Kind != "probe" {
		t.Errorf("apache: %+v %v", ev, ok)
	}
}
