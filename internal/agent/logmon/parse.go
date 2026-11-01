// Package logmon liest zusätzliche Protokolle (Dateien, Docker-Container) inkrementell und
// normalisiert Anmeldeversuche und Angriffsmuster zu shared.AuthEvent: nginx-Zugriffs-
// und Fehlerprotokolle (401 → Fehlversuch, Scanner-Pfade → probe), sshd-Ausgaben von
// SFTP-Containern, FTP-Server (vsftpd, proftpd, pure-ftpd) und eigene Muster. Die Parser
// sind rein und plattformneutral testbar.
package logmon

import (
	"encoding/json"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/boonkerz/roster/internal/agent/authlog"
	"github.com/boonkerz/roster/internal/shared"
)

var (
	// Docker json-file: {"log":"…","stream":"stdout","time":"2026-11-01T09:12:33.123Z"}
	reForego   = regexp.MustCompile(`^[A-Za-z0-9_.-]+\.\d+\s+\|\s`) // nginx-proxy/forego: "nginx.1    | "
	reDockerTS = regexp.MustCompile(`^(\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?Z)\s`)
	// Combined-Log (auch nginx-proxy: "$host $remote_addr - $remote_user [...] ...")
	reAccess  = regexp.MustCompile(`(?:^|\s)(?P<ip>(?:\d{1,3}\.){3}\d{1,3}|[0-9a-fA-F:]{3,}) - (?P<user>\S+) \[(?P<time>[^\]]+)\] "(?P<req>[^"]*)" (?P<status>\d{3})`)
	reRequest = regexp.MustCompile(`^(?P<method>[A-Z]{3,10}) (?P<path>\S+)(?: HTTP/[\d.]+)?$`)
	// nginx error.log ("client: 1.2.3.4") und Apache error.log ("[client 1.2.3.4:51234]")
	reErrClient     = regexp.MustCompile(`client:? (?P<ip>(?:\d{1,3}\.){3}\d{1,3}|[0-9a-fA-F:]{3,}?)(?::\d+)?[,\]\s]`)
	reErrUser       = regexp.MustCompile(`user "([^"]*)"`)
	reErrUserApache = regexp.MustCompile(`user (\S+?):? `)
	reErrRequest    = regexp.MustCompile(`request: "([^"]*)"`)
	// FTP
	reVsftpd   = regexp.MustCompile(`(?:\[(?P<user>[^\]]*)\] )?(?P<res>FAIL|OK) LOGIN: Client "(?P<ip>[^"]+)"`)
	reProftpd  = regexp.MustCompile(`\[(?P<ip>[0-9a-fA-F.:]+)\]\)?[: -]+USER (?P<user>\S+?)(?::|\s)[^\n]*?(?P<res>Login failed|no such user|Login successful)`)
	rePureFTPd = regexp.MustCompile(`\(\?@(?P<ip>[0-9a-fA-F.:]+)\) \[(?P<lvl>\w+)\] (?P<msg>.*)$`)
	rePureFail = regexp.MustCompile(`Authentication failed for user \[([^\]]*)\]`)
	rePureOK   = regexp.MustCompile(`^(\S+) is now logged in`)
	// Mail: Postfix SASL, Dovecot auth/imap-login, Exim
	rePostfixSASL = regexp.MustCompile(`warning: [^\[]*\[(?P<ip>[0-9a-fA-F.:]+)\]: SASL (?:\w+ )?authentication failed(?:.*sasl_username=(?P<user>\S+))?`)
	reDovecotFail = regexp.MustCompile(`(?:auth failed|authentication failure|Aborted login).*?(?:rip|remote)=(?P<ip>[0-9a-fA-F.:]+)`)
	reDovecotUser = regexp.MustCompile(`user=<([^>]*)>`)
	reDovecotOK   = regexp.MustCompile(`(?:imap|pop3|submission|managesieve)-login: Login: user=<(?P<user>[^>]*)>.*?rip=(?P<ip>[0-9a-fA-F.:]+)`)
	reEximFail    = regexp.MustCompile(`(?:LOGIN|PLAIN|CRAM-MD5|dovecot_\w+) authenticator failed for [^\[]*\[(?P<ip>[0-9a-fA-F.:]+)\](?:.*?set_id=(?P<user>[^)\s]+))?`)

	webMethods = map[string]bool{"GET": true, "POST": true, "HEAD": true, "PUT": true, "DELETE": true, "OPTIONS": true, "PATCH": true, "PROPFIND": true, "CONNECT": true}
	// Pfade/Teile, die praktisch nur Scanner anfragen (Kleinschreibung).
	probeMarks = []string{
		"wp-login.php", "wp-admin", "xmlrpc.php", "wlwmanifest.xml", "/.env", "/.git", "/.svn", "/.hg", "/.aws", "/.ssh", "/.ds_store",
		"phpmyadmin", "/pma/", "/mysql/", "/adminer", "/cgi-bin/", "/vendor/phpunit", "eval-stdin", "/shell", "/cmd.php", "/c99", "/r57",
		"/actuator", "/solr/", "/console/", "/manager/html", "/jmx-console", "/boaform/", "/hnap1", "/gponform", "/setup.cgi", "/cgi/",
		"/owa/", "/autodiscover/", "/ecp/", "/telescope", "/_profiler", "/debug/default", "/server-status", "/.vscode", "/.idea",
		"/etc/passwd", "../", "..%2f", "%2e%2e", "union select", "union+select", "union%20select", "<script", "%3cscript", "base64_decode",
		"/config.php", "/configuration.php", "/backup", "/dump.sql", "/db.sql", "/.bak", "/web.config", "/phpinfo", "/info.php", "/test.php",
		"/wp-content/", "/wp-includes/", "/joomla", "/administrator/", "/magento", "/downloader/", "/login.action", "/struts", "/jenkins",
	}
	probeExt = []string{".php", ".asp", ".aspx", ".jsp", ".cgi", ".sh", ".bak", ".sql", ".zip", ".tar", ".gz", ".rar", ".7z", ".old", ".orig", ".swp", ".env", ".ini", ".log", ".yml", ".yaml", ".xml"}
)

// Line ist eine zu analysierende Protokollzeile mit bekannter Zeit (Zero = unbekannt).
type Line struct {
	Text string
	Time time.Time
}

// Unwrap entfernt Docker-Umhüllungen: json-file-Format, `docker logs --timestamps`-Präfix und
// forego-Präfix ("nginx.1 | "). Liefert Text und – falls vorhanden – den Zeitstempel.
func Unwrap(raw string) Line {
	l := Line{Text: strings.TrimRight(raw, "\r\n")}
	if strings.HasPrefix(l.Text, `{"log":`) {
		var rec struct {
			Log  string `json:"log"`
			Time string `json:"time"`
		}
		if json.Unmarshal([]byte(l.Text), &rec) == nil {
			l.Text = strings.TrimRight(rec.Log, "\r\n")
			if t, err := time.Parse(time.RFC3339Nano, rec.Time); err == nil {
				l.Time = t
			}
		}
	} else if m := reDockerTS.FindStringSubmatch(l.Text); m != nil {
		if t, err := time.Parse(time.RFC3339Nano, m[1]); err == nil {
			l.Time = t
		}
		l.Text = l.Text[len(m[0]):]
	}
	l.Text = reForego.ReplaceAllString(l.Text, "")
	return l
}

// Parse normalisiert eine Zeile gemäß Format der Quelle. ok=false für uninteressante Zeilen.
func Parse(src shared.LogSource, l Line, now time.Time) (shared.AuthEvent, bool) {
	ts := l.Time
	if ts.IsZero() {
		ts = now
	}
	var ev shared.AuthEvent
	var ok bool
	switch src.Format {
	case "nginx_access":
		ev, ok = parseAccess(l.Text, ts)
	case "nginx_error":
		ev, ok = parseNginxError(l.Text, ts)
	case "sshd":
		ev, ok = parseSSHD(l.Text, ts)
	case "ftp":
		ev, ok = parseFTP(l.Text, ts)
	case "mail":
		ev, ok = parseMail(l.Text, ts)
	case "regex":
		ev, ok = parseRegex(src, l.Text, ts)
	}
	if !ok {
		return ev, false
	}
	ev.Service = src.Name
	if len(ev.Detail) > 200 {
		ev.Detail = ev.Detail[:200]
	}
	return ev, true
}

// suspiciousPath meldet, ob Pfad+Query nach Scanner aussieht.
func suspiciousPath(path string, status int) bool {
	p := strings.ToLower(path)
	for _, m := range probeMarks {
		if strings.Contains(p, m) {
			return true
		}
	}
	if status == 404 {
		clean := p
		if i := strings.IndexAny(clean, "?#"); i >= 0 {
			clean = clean[:i]
		}
		for _, e := range probeExt {
			if strings.HasSuffix(clean, e) {
				return true
			}
		}
	}
	return false
}

func parseAccess(line string, ts time.Time) (shared.AuthEvent, bool) {
	m := reAccess.FindStringSubmatch(line)
	if m == nil {
		return shared.AuthEvent{}, false
	}
	ip, user, when, req, st := m[1], m[2], m[3], m[4], m[5]
	if t, err := time.Parse("02/Jan/2006:15:04:05 -0700", when); err == nil {
		ts = t
	}
	status := atoi(st)
	ev := shared.AuthEvent{Time: ts, SourceIP: ip}
	if user != "-" && user != "" {
		ev.User = user
	}
	rm := reRequest.FindStringSubmatch(req)
	method, path := "", req
	if rm != nil {
		method, path = rm[1], rm[2]
	}
	ev.Detail = path
	switch {
	case status == 401:
		ev.Kind = "login_failed"
		return ev, true
	case status == 400 || status == 444 || status == 497 || (rm == nil && req != "") || (rm != nil && !webMethods[method]):
		ev.Kind = "probe"
		if ev.Detail == "" {
			ev.Detail = "malformed"
		}
		return ev, true
	case status == 403 && (strings.Contains(strings.ToLower(path), "..") || suspiciousPath(path, status)):
		ev.Kind = "probe"
		return ev, true
	case suspiciousPath(path, status):
		ev.Kind = "probe"
		return ev, true
	}
	return ev, false
}

func parseNginxError(line string, ts time.Time) (shared.AuthEvent, bool) {
	m := reErrClient.FindStringSubmatch(line)
	if m == nil {
		return shared.AuthEvent{}, false
	}
	ev := shared.AuthEvent{Time: ts, SourceIP: m[1]}
	if um := reErrUser.FindStringSubmatch(line); um != nil {
		ev.User = um[1]
	}
	if rq := reErrRequest.FindStringSubmatch(line); rq != nil {
		if rm := reRequest.FindStringSubmatch(rq[1]); rm != nil {
			ev.Detail = rm[2]
		} else {
			ev.Detail = rq[1]
		}
	} else if i := strings.LastIndex(line, ": /"); i >= 0 { // Apache: "...: /var/www/html/x.php"
		ev.Detail = strings.TrimSpace(line[i+2:])
	}
	low := strings.ToLower(line)
	switch {
	case strings.Contains(low, "password mismatch"), strings.Contains(low, "no user/password was provided"), strings.Contains(low, "was not found in"),
		strings.Contains(low, "auth_request unexpected status"),
		// Apache mod_auth: "user x: authentication failure", "user x not found", "user x: password mismatch"
		strings.Contains(low, "authentication failure"), strings.Contains(low, "not found:"), strings.Contains(low, "ah01617"), strings.Contains(low, "ah01618"), strings.Contains(low, "ah01797"):
		ev.Kind = "login_failed"
		if ev.User == "" {
			if um := reErrUserApache.FindStringSubmatch(line); um != nil {
				ev.User = strings.Trim(um[1], `"'`)
			}
		}
	case strings.Contains(low, "access forbidden by rule"), strings.Contains(low, "modsecurity: access denied"), strings.Contains(low, "limiting requests"),
		strings.Contains(low, "limiting connections"), strings.Contains(low, "client sent invalid"), strings.Contains(low, "client intended to send too large"),
		strings.Contains(low, "client denied by server configuration"), strings.Contains(low, "invalid uri in request"), strings.Contains(low, "request failed: error reading the headers"):
		ev.Kind = "probe"
	case ev.Detail != "" && suspiciousPath(ev.Detail, 404):
		ev.Kind = "probe"
	default:
		return ev, false
	}
	return ev, true
}

// parseSSHD versteht sshd-Zeilen mit und ohne syslog-Präfix (Container geben sie roh aus).
func parseSSHD(line string, ts time.Time) (shared.AuthEvent, bool) {
	if ev, ok := authlog.ParseSyslogLine(line, ts); ok {
		return ev, true
	}
	msg := line
	if i := strings.Index(msg, "]: "); i >= 0 && i < 60 { // "sshd[123]: …" ohne Datum
		msg = msg[i+3:]
	}
	ev, ok := authlog.ParseMessage("sshd", msg, ts)
	if !ok || (ev.Kind != "login_failed" && ev.Kind != "invalid_user" && ev.Kind != "login_ok") {
		return ev, false
	}
	return ev, true
}

func parseFTP(line string, ts time.Time) (shared.AuthEvent, bool) {
	if m := reVsftpd.FindStringSubmatch(line); m != nil {
		ev := shared.AuthEvent{Time: ts, SourceIP: m[3], User: m[1], Kind: "login_failed"}
		if m[2] == "OK" {
			ev.Kind = "login_ok"
		}
		return ev, true
	}
	if m := reProftpd.FindStringSubmatch(line); m != nil {
		ev := shared.AuthEvent{Time: ts, SourceIP: m[1], User: strings.TrimSuffix(m[2], ":")}
		switch m[3] {
		case "Login successful":
			ev.Kind = "login_ok"
		case "no such user":
			ev.Kind = "invalid_user"
		default:
			ev.Kind = "login_failed"
		}
		return ev, true
	}
	if m := rePureFTPd.FindStringSubmatch(line); m != nil {
		ev := shared.AuthEvent{Time: ts, SourceIP: m[1]}
		if f := rePureFail.FindStringSubmatch(m[3]); f != nil {
			ev.Kind, ev.User = "login_failed", f[1]
			return ev, true
		}
		if o := rePureOK.FindStringSubmatch(m[3]); o != nil {
			ev.Kind, ev.User = "login_ok", o[1]
			return ev, true
		}
	}
	return shared.AuthEvent{}, false
}

// parseMail versteht Postfix (SASL), Dovecot (auth failed / Login) und Exim (authenticator failed).
func parseMail(line string, ts time.Time) (shared.AuthEvent, bool) {
	if m := rePostfixSASL.FindStringSubmatch(line); m != nil {
		return shared.AuthEvent{Time: ts, Kind: "login_failed", SourceIP: m[1], User: m[2]}, true
	}
	if m := reDovecotOK.FindStringSubmatch(line); m != nil {
		return shared.AuthEvent{Time: ts, Kind: "login_ok", User: m[1], SourceIP: m[2]}, true
	}
	if m := reDovecotFail.FindStringSubmatch(line); m != nil {
		ev := shared.AuthEvent{Time: ts, Kind: "login_failed", SourceIP: m[1]}
		if u := reDovecotUser.FindStringSubmatch(line); u != nil {
			ev.User = u[1]
		}
		return ev, true
	}
	if m := reEximFail.FindStringSubmatch(line); m != nil {
		return shared.AuthEvent{Time: ts, Kind: "login_failed", SourceIP: m[1], User: m[2]}, true
	}
	return shared.AuthEvent{}, false
}

var (
	reMu    sync.Mutex
	reCache = map[string]*regexp.Regexp{}
)

func compiled(pattern string) *regexp.Regexp {
	reMu.Lock()
	defer reMu.Unlock()
	if re, ok := reCache[pattern]; ok {
		return re
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		re = nil
	}
	reCache[pattern] = re
	return re
}

func parseRegex(src shared.LogSource, line string, ts time.Time) (shared.AuthEvent, bool) {
	re := compiled(src.Pattern)
	if re == nil {
		return shared.AuthEvent{}, false
	}
	m := re.FindStringSubmatch(line)
	if m == nil {
		return shared.AuthEvent{}, false
	}
	ev := shared.AuthEvent{Time: ts, Kind: src.Kind, Detail: line}
	if i := re.SubexpIndex("ip"); i > 0 && i < len(m) {
		ev.SourceIP = m[i]
	}
	if i := re.SubexpIndex("user"); i > 0 && i < len(m) {
		ev.User = m[i]
	}
	if ev.SourceIP == "" {
		return ev, false
	}
	if ev.Kind == "" {
		ev.Kind = "login_failed"
	}
	return ev, true
}

func atoi(s string) int {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return n
		}
		n = n*10 + int(c-'0')
	}
	return n
}
