// Package logmon liest zusätzliche Protokolle (Dateien, Docker-Container) inkrementell und
// normalisiert Anmeldeversuche und Angriffsmuster zu shared.AuthEvent: nginx-Zugriffs-
// und Fehlerprotokolle (401 → Fehlversuch, Scanner-Pfade → probe), sshd-Ausgaben von
// SFTP-Containern, FTP-Server (vsftpd, proftpd, pure-ftpd) und eigene Muster. Die Parser
// sind rein und plattformneutral testbar.
package logmon

import (
	"encoding/json"
	"net"
	"net/url"
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
	reANSI     = regexp.MustCompile(`\x1b\[[0-9;]*[A-Za-z]`)        // Farbcodes (forego färbt den Präfix)
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
		// Befehls-/Code-Injection in Pfad oder Query (zählt unabhängig vom Status, auch bei 200/503)
		"cmd=", "exec=", "=curl", "=wget", ";wget", ";curl", "|wget", "|curl", "%7cwget", "%7ccurl", "chmod+", "chmod%20", "/bin/sh", "/bin/bash",
		"base64_decode", "eval(", "eval%28", "${jndi", "%24%7bjndi", "<script", "%3cscript", "/etc/passwd", "..%2f", "..%5c", "%00",
		"invokefunction", "call_user_func", "auto_prepend_file", "allow_url_include", "think\\app", "phpinfo(",
		// Dateien mit Zugangsdaten/Konfiguration, die Scanner blind abfragen
		"server.key", "/credentials", "credentials.", "/aws/", "aws.env", "/env.", "/.env.", "/config.y", "/settings.json", "/secrets", "id_rsa",
		"/phpinfo", "pm2.config", "next.config", "bitbucket-pipelines", "docker-compose.y", "/.docker", "/.vscode", "/.idea", "/backup.", "/dump.",
		"/server-status", "/server-info", "/sdk/weblanguage", "/.well-known/security", "/owa/", "/ecp/", "/autodiscover/",
		"/proc/self", "%2eenv", "%2e%2e", "/_debugbar", "/dockerfile", "private-key", "/_profiler", "/telescope", "/_ignition",
		// SQL-Injektion in Pfad oder Query (wird zusätzlich gegen die URL-dekodierte Form geprüft, in
		// der + und %20 Leerzeichen und /**/ ein Leerzeichen sind)
		"union select", "union all select", "union distinct select", "information_schema", "sleep(", "benchmark(", "pg_sleep(", "waitfor delay",
		"' or 1=1", "\" or 1=1", "' or '1'='1", "' or \"", "or 1=1--", "or 1=1#", "' and 1=1", "' and sleep", "'--", "')--", "' --", "\"--",
		"load_file(", "into outfile", "into dumpfile", "xp_cmdshell", "@@version", "version()", "/*!", "extractvalue(", "updatexml(",
		"group_concat(", "concat_ws(", "convert(int,", "having 1=1", "order by 1--", "' order by", "admin'--", "admin' #",
		// Weitere Scanner-Ziele (CMS, Backups, Debug-Endpunkte)
		"/debug/default", "../", "union+select", "union%20select", "/config.php", "/configuration.php", "/backup", "/dump.sql",
		"/db.sql", "/.bak", "/web.config", "/info.php", "/test.php", "/wp-content/", "/wp-includes/", "/joomla",
		"/administrator/", "/magento", "/downloader/", "/login.action", "/struts", "/jenkins",
	}
	probeExt = []string{".php", ".asp", ".aspx", ".jsp", ".cgi", ".sh", ".bak", ".sql", ".zip", ".tar", ".gz", ".rar", ".7z", ".old", ".orig", ".swp", ".env", ".ini", ".log", ".yml", ".yaml", ".xml",
		".toml", ".hcl", ".tfvars", ".tfstate", ".prisma", ".properties", ".key", ".pem", ".p12", ".pfx", ".sqlite", ".db", ".conf", ".cfg", ".py", ".rb", ".pl", ".lock"}
	// nur bei 503 (Reverse-Proxy ohne Backend) – bei 404 auf echten Seiten zu häufig legitim
	probeExt503 = []string{".json", ".js", ".txt", ".md"}

	// Pfade, die normale Programme von sich aus abfragen – Mail-Clients beim Einrichten
	// (Autodiscover/Autoconfig), Browser und Betriebssysteme (wpad, Sitemap, Browserconfig,
	// Universal Links) sowie alles unter /.well-known/ (ACME, CalDAV/CardDAV, webfinger …).
	// Ein 404 darauf ist kein Scan: eine Büro-Etage, die Outlook einrichtet, erzeugte sonst
	// den Vorfall „Angriffsmuster“ und eine Flottensperre der Kunden-IP.
	benignPaths = []string{
		"/autodiscover/autodiscover.xml", "/autodiscover/autodiscover.json", "/autodiscover/autodiscover.svc",
		"/.well-known/autoconfig/mail/config-v1.1.xml", "/mail/config-v1.1.xml", "/autoconfig/mail/config-v1.1.xml",
		"/wpad.dat", "/sitemap.xml", "/sitemap_index.xml", "/browserconfig.xml", "/crossdomain.xml",
		"/apple-app-site-association", "/favicon.ico", "/robots.txt", "/humans.txt",
	}
	benignPrefixes = []string{"/.well-known/", "/apple-touch-icon"}
)

// isBenignPath meldet, ob ein (kleingeschriebener) Pfad ohne Query eine harmlose
// Client-Sonde ist, die nie als verdächtig zählt.
func isBenignPath(p string) bool {
	if i := strings.IndexAny(p, "?#"); i >= 0 {
		p = p[:i]
	}
	p = strings.TrimRight(p, "/")
	if p == "" || strings.Contains(p, "..") || strings.Contains(p, "%") {
		return false // Pfad-Spielereien (../, Kodierung) bleiben verdächtig
	}
	for _, b := range benignPaths {
		if p == b {
			return true
		}
	}
	for _, pre := range benignPrefixes {
		if strings.HasPrefix(p, pre) {
			return true
		}
	}
	return false
}

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
	if strings.Contains(l.Text, "\x1b[") {
		l.Text = reANSI.ReplaceAllString(l.Text, "")
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
	case "nginx_access", "apache_access":
		ev, ok = parseAccess(l.Text, ts)
	case "haproxy":
		ev, ok = parseHAProxy(l.Text, ts)
	case "traefik":
		ev, ok = parseTraefik(l.Text, ts)
	case "mysql":
		ev, ok = parseMySQL(l.Text, ts)
	case "firewall":
		ev, ok = parseFirewall(l.Text, ts)
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
	if isBenignPath(p) {
		return false
	}
	for _, m := range probeMarks {
		if strings.Contains(p, m) {
			return true
		}
	}
	// Dekodierte Form (Prozent-Kodierung, + → Leerzeichen, SQL-Kommentar /**/ → Leerzeichen),
	// damit %27%20or%201%3d1 oder union/**/select nicht an der Kodierung vorbeikommen.
	if d := decodedPath(p); d != p {
		for _, m := range probeMarks {
			if strings.Contains(d, m) {
				return true
			}
		}
	}
	// 404 (klassisch) und 503 (Reverse-Proxy ohne passenden Backend-Host, z. B. nginx-proxy)
	// auf typische Datei-Endungen: Konfigurations-/Backup-/Skript-Dateien, die niemand verlinkt.
	if status >= 400 {
		// versteckte Dateien/Ordner (/.git, /.terraform, /.dev.vars …) – außer ACME/.well-known
		if strings.Contains(p, "/.") && !strings.Contains(p, "/.well-known/") {
			return true
		}
	}
	if status == 404 || status == 503 {
		clean := p
		if i := strings.IndexAny(clean, "?#"); i >= 0 {
			clean = clean[:i]
		}
		for _, e := range probeExt {
			if strings.HasSuffix(clean, e) {
				return true
			}
		}
		if status == 503 {
			for _, e := range probeExt503 {
				if strings.HasSuffix(clean, e) {
					return true
				}
			}
		}
	}
	return false
}

// decodedPath liefert die URL-dekodierte, normalisierte Kleinschreibung eines Pfads.
func decodedPath(p string) string {
	d, err := url.QueryUnescape(p)
	if err != nil {
		d = p
	}
	d = strings.ToLower(d)
	d = strings.ReplaceAll(d, "/**/", " ")
	d = strings.Join(strings.Fields(d), " ")
	return d
}

// hostIsLiteral erkennt Anfragen an die nackte IP bzw. den Default-Server („_“) eines
// Reverse-Proxys – echte Besucher kommen über den Domainnamen, Scanner über die IP.
func hostIsLiteral(host string) bool {
	host = strings.TrimSpace(host)
	if host == "" {
		return false
	}
	if host == "_" {
		return true
	}
	if i := strings.LastIndexByte(host, ':'); i > 0 && !strings.Contains(host, "]") && strings.Count(host, ":") == 1 {
		host = host[:i]
	}
	host = strings.Trim(host, "[]")
	return net.ParseIP(host) != nil
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
	host := strings.TrimSpace(line[:strings.Index(line, m[0])])
	return classifyHTTP(ev, host, req, status)
}

// classifyHTTP bewertet eine HTTP-Anfrage (Combined Log, HAProxy, Traefik …): 401 →
// Fehlversuch; kaputte Anfragen, Scanner-Pfade und fehlgeschlagene Anfragen an die nackte
// Server-IP → verdächtige Anfrage. host ist der vorangestellte vhost (leer = unbekannt).
func classifyHTTP(ev shared.AuthEvent, host, req string, status int) (shared.AuthEvent, bool) {
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
	case status >= 400 && host != "" && hostIsLiteral(host) && path != "/" && !isBenignPath(strings.ToLower(path)):
		// Fehlgeschlagene Anfrage an die nackte Server-IP / den Default-Host: Scanner.
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
