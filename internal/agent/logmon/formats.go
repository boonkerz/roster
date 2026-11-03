package logmon

import (
	"encoding/json"
	"regexp"
	"strings"
	"time"

	"github.com/boonkerz/roster/internal/shared"
)

// Weitere Log-Formate: HAProxy (HTTP-Log), Traefik (JSON oder CLF), MySQL/MariaDB-
// Fehlerprotokoll, Firewall-Drops (iptables/nft/ufw im Kernel-Log, Windows pfirewall.log).

var (
	// HAProxy HTTP-Log: "haproxy[123]: 1.2.3.4:51234 [03/Nov/2026:10:00:00.123] fe be/srv 0/0/0/5/5 404 1234 - - ---- 1/1/0/0/0 0/0 "GET /x HTTP/1.1""
	reHAProxy = regexp.MustCompile(`(?:^|\s)(?P<ip>(?:\d{1,3}\.){3}\d{1,3}|[0-9a-fA-F:]{3,}):\d+ \[(?P<time>[^\]]+)\] \S+ \S+ [\d/+-]+ (?P<status>-?\d{1,3}) \d+ \S+ \S+ (?P<flags>[\w-]{4}) .*?"(?P<req>[^"]*)"`)
	// MySQL/MariaDB: "Access denied for user 'root'@'203.0.113.5' (using password: YES)"
	reMySQLDenied  = regexp.MustCompile(`Access denied for user '(?P<user>[^']*)'@'(?P<ip>[^']+)'`)
	reMySQLHost    = regexp.MustCompile(`Host '(?P<ip>[^']+)' is not allowed to connect`)
	reMySQLAborted = regexp.MustCompile(`(?i)Aborted connection \d+ to db: '[^']*' user: '(?P<user>[^']*)' host: '(?P<ip>[^']+)' \((?P<why>[^)]*)\)`)
	// Linux-Firewall (iptables LOG, nft log prefix, ufw): "... [UFW BLOCK] IN=eth0 OUT= ... SRC=203.0.113.5 DST=10.0.0.2 ... PROTO=TCP SPT=4444 DPT=22 ..."
	reFWLinux = regexp.MustCompile(`IN=(?P<in>\S*) OUT=(?P<out>\S*) .*?SRC=(?P<ip>\S+) DST=\S+ .*?PROTO=(?P<proto>\w+)(?: SPT=\d+)? DPT=(?P<port>\d+)`)
	// Windows pfirewall.log: "2026-11-03 10:00:00 DROP TCP 203.0.113.5 10.0.0.2 4444 3389 52 S 1 0 0 - - - RECEIVE"
	reFWWindows = regexp.MustCompile(`^(?P<date>\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}) DROP (?P<proto>TCP|UDP|ICMP\w*|\d+) (?P<ip>\S+) \S+ \S+ (?P<port>\d+|-) .* RECEIVE\s*$`)
)

func parseHAProxy(line string, ts time.Time) (shared.AuthEvent, bool) {
	m := reHAProxy.FindStringSubmatch(line)
	if m == nil {
		return shared.AuthEvent{}, false
	}
	ip, when, st, flags, req := m[1], m[2], m[3], m[4], m[5]
	if t, err := time.Parse("02/Jan/2006:15:04:05.000", when); err == nil {
		ts = t
	}
	status := atoi(st)
	ev := shared.AuthEvent{Time: ts, SourceIP: ip}
	// Termination-State "PR": Anfrage von HAProxy abgewiesen (kaputt/zu groß) → Scanner.
	if strings.HasPrefix(flags, "PR") || status == -1 {
		ev.Kind, ev.Detail = "probe", "rejected by proxy"
		if req != "" && req != "<BADREQ>" {
			ev.Detail = req
		}
		return ev, true
	}
	return classifyHTTP(ev, "", req, status)
}

// traefikLine ist das JSON-Zugriffsformat von Traefik.
type traefikLine struct {
	ClientHost       string `json:"ClientHost"`
	ClientAddr       string `json:"ClientAddr"`
	RequestMethod    string `json:"RequestMethod"`
	RequestPath      string `json:"RequestPath"`
	RequestHost      string `json:"RequestHost"`
	DownstreamStatus int    `json:"DownstreamStatus"`
	OriginStatus     int    `json:"OriginStatus"`
	StartUTC         string `json:"StartUTC"`
}

func parseTraefik(line string, ts time.Time) (shared.AuthEvent, bool) {
	t := strings.TrimSpace(line)
	if !strings.HasPrefix(t, "{") {
		return parseAccess(line, ts) // CLF/Combined
	}
	var l traefikLine
	if json.Unmarshal([]byte(t), &l) != nil {
		return shared.AuthEvent{}, false
	}
	ip := l.ClientHost
	if ip == "" {
		ip = l.ClientAddr
		if i := strings.LastIndexByte(ip, ':'); i > 0 && strings.Count(ip, ":") == 1 {
			ip = ip[:i]
		}
	}
	if ip == "" {
		return shared.AuthEvent{}, false
	}
	if st, err := time.Parse(time.RFC3339Nano, l.StartUTC); err == nil {
		ts = st
	}
	status := l.DownstreamStatus
	if status == 0 {
		status = l.OriginStatus
	}
	req := strings.TrimSpace(l.RequestMethod + " " + l.RequestPath)
	return classifyHTTP(shared.AuthEvent{Time: ts, SourceIP: ip}, l.RequestHost, req, status)
}

func parseMySQL(line string, ts time.Time) (shared.AuthEvent, bool) {
	if m := reMySQLDenied.FindStringSubmatch(line); m != nil {
		return shared.AuthEvent{Time: ts, Kind: "login_failed", User: m[1], SourceIP: m[2], Detail: "Access denied"}, true
	}
	if m := reMySQLHost.FindStringSubmatch(line); m != nil {
		return shared.AuthEvent{Time: ts, Kind: "login_failed", SourceIP: m[1], Detail: "host not allowed"}, true
	}
	if m := reMySQLAborted.FindStringSubmatch(line); m != nil {
		why := strings.ToLower(m[3])
		if strings.Contains(why, "authorization") || strings.Contains(why, "handshake") {
			return shared.AuthEvent{Time: ts, Kind: "probe", User: m[1], SourceIP: m[2], Detail: m[3]}, true
		}
	}
	return shared.AuthEvent{}, false
}

// parseFirewall liefert fw_drop für verworfene eingehende Pakete (Detail "tcp/22").
func parseFirewall(line string, ts time.Time) (shared.AuthEvent, bool) {
	if m := reFWLinux.FindStringSubmatch(line); m != nil {
		in, out, ip, proto, port := m[1], m[2], m[3], m[4], m[5]
		if in == "" && out != "" {
			return shared.AuthEvent{}, false // ausgehend
		}
		if t, ok := syslogTime(line, ts); ok {
			ts = t
		}
		return shared.AuthEvent{Time: ts, Kind: "fw_drop", SourceIP: ip, Detail: strings.ToLower(proto) + "/" + port}, true
	}
	if m := reFWWindows.FindStringSubmatch(line); m != nil {
		if t, err := time.ParseInLocation("2006-01-02 15:04:05", m[1], time.Local); err == nil {
			ts = t
		}
		port := m[4]
		if port == "-" {
			port = "0"
		}
		return shared.AuthEvent{Time: ts, Kind: "fw_drop", SourceIP: m[3], Detail: strings.ToLower(m[2]) + "/" + port}, true
	}
	return shared.AuthEvent{}, false
}

var reSyslogTS = regexp.MustCompile(`^(?:(\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:\d{2}))|([A-Z][a-z]{2} {1,2}\d{1,2} \d{2}:\d{2}:\d{2}))`)

// syslogTime liest einen führenden Syslog-/ISO-Zeitstempel (kern.log, journal -o short).
func syslogTime(line string, now time.Time) (time.Time, bool) {
	m := reSyslogTS.FindStringSubmatch(line)
	if m == nil {
		return now, false
	}
	if m[1] != "" {
		if t, err := time.Parse(time.RFC3339Nano, m[1]); err == nil {
			return t, true
		}
		return now, false
	}
	if t, err := time.ParseInLocation("Jan _2 15:04:05", m[2], time.Local); err == nil {
		t = t.AddDate(now.Year(), 0, 0)
		if t.After(now.Add(24 * time.Hour)) {
			t = t.AddDate(-1, 0, 0)
		}
		return t, true
	}
	return now, false
}
