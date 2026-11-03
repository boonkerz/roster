package secdetect

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/boonkerz/roster/internal/shared"
)

func ev(kind, user, ip string, t time.Time) shared.AuthEvent {
	return shared.AuthEvent{Time: t, Kind: kind, User: user, SourceIP: ip, Service: "sshd"}
}

func kinds(in []Incident) []string {
	var out []string
	for _, i := range in {
		out = append(out, i.Kind)
	}
	return out
}

func TestBruteForceAndSuccess(t *testing.T) {
	cfg := DefaultConfig()
	cfg.BFThreshold = 5
	base := time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)
	var events []shared.AuthEvent
	for i := 0; i < 7; i++ {
		events = append(events, ev("login_failed", "root", "203.0.113.5", base.Add(time.Duration(i)*20*time.Second)))
	}
	events = append(events, ev("login_ok", "root", "203.0.113.5", base.Add(5*time.Minute)))
	events = append(events, ev("login_ok", "thomas", "192.168.1.10", base.Add(6*time.Minute)))
	in := Input{Now: base.Add(10 * time.Minute), Events: events, KnownIP: func(u, ip string) bool { return u == "thomas" }, MonitoringSince: base.AddDate(0, -1, 0)}
	inc := Detect(in, cfg)
	got := kinds(inc)
	if len(got) != 2 || got[0] != "brute_force" || got[1] != "brute_force_success" {
		t.Fatalf("got %v: %+v", got, inc)
	}
	if inc[0].Severity != "warning" || inc[1].Severity != "critical" || inc[1].User != "root" {
		t.Errorf("severity: %+v", inc)
	}
	// Dedupe: ein zweiter Lauf mit bekannter BF-IP meldet nicht erneut; weitere Fehlversuche zählen Prior mit.
	prior := map[string][]time.Time{}
	for _, e := range events[:7] {
		prior[e.SourceIP] = append(prior[e.SourceIP], e.Time)
	}
	in2 := Input{Now: base.Add(12 * time.Minute), Events: []shared.AuthEvent{ev("login_failed", "root", "203.0.113.5", base.Add(11*time.Minute))},
		PriorFails: prior, RecentBF: map[string]time.Time{"203.0.113.5": base}, KnownIP: func(string, string) bool { return true }}
	if inc := Detect(in2, cfg); len(inc) != 0 {
		t.Fatalf("dedupe: %+v", inc)
	}
	// Unter der Schwelle: nichts.
	in3 := Input{Now: base, Events: events[:4], KnownIP: func(string, string) bool { return true }}
	if inc := Detect(in3, cfg); len(inc) != 0 {
		t.Fatalf("unter schwelle: %+v", inc)
	}
	// Fenster: 5 Fehlversuche über 2 Stunden verteilt lösen nichts aus.
	var spread []shared.AuthEvent
	for i := 0; i < 5; i++ {
		spread = append(spread, ev("login_failed", "root", "198.51.100.1", base.Add(time.Duration(i)*30*time.Minute)))
	}
	if inc := Detect(Input{Now: base.Add(3 * time.Hour), Events: spread, KnownIP: func(string, string) bool { return true }}, cfg); len(inc) != 0 {
		t.Fatalf("fenster: %+v", inc)
	}
}

func TestNewIPAndAccounts(t *testing.T) {
	cfg := DefaultConfig()
	base := time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)
	known := func(u, ip string) bool { return ip == "192.168.1.10" }
	events := []shared.AuthEvent{
		ev("login_ok", "thomas", "192.168.1.10", base),
		ev("login_ok", "thomas", "203.0.113.77", base.Add(time.Minute)),
		ev("login_ok", "thomas", "203.0.113.77", base.Add(2*time.Minute)), // gleiche IP im Batch nur einmal
		ev("login_ok", "root", "127.0.0.1", base.Add(3*time.Minute)),
		{Time: base.Add(4 * time.Minute), Kind: "user_added", User: "evil", Detail: "new user: name=evil"},
		{Time: base.Add(5 * time.Minute), Kind: "group_changed", User: "evil", Detail: "group sudo"},
		{Time: base.Add(6 * time.Minute), Kind: "group_changed", User: "evil", Detail: "group audio"},
		{Time: base.Add(7 * time.Minute), Kind: "lockout", User: "bob", Detail: "locked"},
		{Time: base.Add(8 * time.Minute), Kind: "sudo", User: "thomas", Detail: "/bin/ls"},
	}
	inc := Detect(Input{Now: base.Add(10 * time.Minute), Events: events, KnownIP: known, MonitoringSince: base.AddDate(0, 0, -30)}, cfg)
	got := kinds(inc)
	want := []string{"new_source_ip", "account_change", "account_change", "account_change", "lockout"}
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v want %v", got, want)
		}
	}
	if inc[0].SourceIP != "203.0.113.77" || inc[2].Severity != "warning" || inc[3].Severity != "info" {
		t.Errorf("details: %+v", inc)
	}
	// Lernphase: Gerät erst seit gestern überwacht → keine Neue-IP-Meldung.
	inc = Detect(Input{Now: base, Events: events[:2], KnownIP: known, MonitoringSince: base.AddDate(0, 0, -1)}, cfg)
	if len(inc) != 0 {
		t.Fatalf("lernphase: %+v", inc)
	}
	// Private IPs ignorieren
	cfg.IgnorePrivateIP = true
	inc = Detect(Input{Now: base, Events: []shared.AuthEvent{ev("login_ok", "x", "10.1.1.1", base)}, KnownIP: func(string, string) bool { return false }}, cfg)
	if len(inc) != 0 {
		t.Fatalf("privat ignorieren: %+v", inc)
	}
	cfg.NewIPAlert = false
	inc = Detect(Input{Now: base, Events: []shared.AuthEvent{ev("login_ok", "x", "203.0.113.1", base)}, KnownIP: func(string, string) bool { return false }}, cfg)
	if len(inc) != 0 {
		t.Fatalf("neue-IP aus: %+v", inc)
	}
}

func TestInvalidUserSlow(t *testing.T) {
	cfg := DefaultConfig()
	cfg.BFThreshold = 10
	cfg.InvalidUserMax = 3
	base := time.Date(2026, 11, 1, 8, 0, 0, 0, time.UTC)
	// Drei Versuche mit unbekannten Benutzern über zwei Stunden verteilt: das 10-min-Fenster
	// greift nie, die 24-h-Regel schon.
	events := []shared.AuthEvent{
		ev("invalid_user", "admin", "198.51.100.7", base),
		ev("invalid_user", "oracle", "198.51.100.7", base.Add(50*time.Minute)),
		ev("login_failed", "root", "198.51.100.9", base.Add(55*time.Minute)),
		ev("invalid_user", "test", "198.51.100.7", base.Add(2*time.Hour)),
	}
	inc := Detect(Input{Now: base.Add(3 * time.Hour), Events: events}, cfg)
	if got := kinds(inc); len(got) != 1 || got[0] != "brute_force" || inc[0].SourceIP != "198.51.100.7" {
		t.Fatalf("got %v: %+v", got, inc)
	}
	if !strings.Contains(inc[0].Detail, "unbekannten Benutzernamen") {
		t.Errorf("detail: %s", inc[0].Detail)
	}
	// Mit Vorgeschichte aus der Datenbank reicht ein einzelnes neues Ereignis.
	prior := map[string][]time.Time{"203.0.113.80": {base.Add(-20 * time.Hour), base.Add(-3 * time.Hour)}}
	inc = Detect(Input{Now: base, Events: []shared.AuthEvent{ev("invalid_user", "pi", "203.0.113.80", base)}, PriorInvalid: prior}, cfg)
	if len(inc) != 1 || inc[0].Kind != "brute_force" {
		t.Fatalf("prior: %+v", inc)
	}
	// Abgeschaltet (0): kein Vorfall.
	cfg.InvalidUserMax = 0
	if inc = Detect(Input{Now: base.Add(3 * time.Hour), Events: events}, cfg); len(inc) != 0 {
		t.Fatalf("disabled: %+v", inc)
	}
}

func TestScanIncident(t *testing.T) {
	cfg := DefaultConfig()
	cfg.ScanThreshold = 5
	base := time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC)
	var events []shared.AuthEvent
	for i, p := range []string{"/wp-login.php", "/.env", "/xmlrpc.php", "/phpmyadmin/", "/.git/config", "/wp-login.php"} {
		events = append(events, shared.AuthEvent{Time: base.Add(time.Duration(i) * 10 * time.Second), Kind: "probe", SourceIP: "203.0.113.44", Service: "nginx-proxy", Detail: p})
	}
	inc := Detect(Input{Now: base.Add(time.Minute), Events: events}, cfg)
	if len(inc) != 1 || inc[0].Kind != "scan" || inc[0].SourceIP != "203.0.113.44" || inc[0].Severity != "warning" {
		t.Fatalf("got %+v", inc)
	}
	if !strings.Contains(inc[0].Detail, "/wp-login.php") || !strings.Contains(inc[0].Detail, "nginx-proxy") {
		t.Errorf("detail: %s", inc[0].Detail)
	}
	// Bereits gemeldet: kein zweiter Vorfall innerhalb einer Stunde.
	inc = Detect(Input{Now: base.Add(time.Minute), Events: events, RecentScan: map[string]time.Time{"203.0.113.44": base.Add(-10 * time.Minute)}}, cfg)
	if len(inc) != 0 {
		t.Fatalf("dedupe: %+v", inc)
	}
}

// TestThreatListed: eine gelistete IP erzeugt beim ersten Fehlversuch genau einen Vorfall
// (24-h-Dedupe), ungelistete keinen; ohne ThreatAlert passiert nichts.
func TestThreatListed(t *testing.T) {
	base := time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)
	lookup := func(ip string) []string {
		if ip == "203.0.113.5" {
			return []string{"Spamhaus DROP"}
		}
		return nil
	}
	cfg := DefaultConfig()
	cfg.ThreatAlert = true
	in := Input{Now: base.Add(time.Minute), ThreatLookup: lookup, Events: []shared.AuthEvent{
		ev("login_failed", "root", "203.0.113.5", base),
		ev("login_failed", "root", "203.0.113.5", base.Add(time.Second)),
		ev("login_failed", "root", "198.51.100.7", base.Add(2*time.Second)),
		ev("login_ok", "bob", "203.0.113.5", base.Add(3*time.Second)),
	}}
	got := Detect(in, cfg)
	if ks := kinds(got); len(ks) != 1 || ks[0] != "threat_listed" || got[0].SourceIP != "203.0.113.5" {
		t.Fatalf("got %v", ks)
	}
	if got[0].Detail == "" || got[0].Title != "Bekannte Angriffsquelle: 203.0.113.5" {
		t.Fatalf("title/detail: %+v", got[0])
	}
	in.RecentThreat = map[string]time.Time{"203.0.113.5": base.Add(-time.Hour)}
	if ks := kinds(Detect(in, cfg)); len(ks) != 0 {
		t.Fatalf("dedupe: %v", ks)
	}
	cfg.ThreatAlert = false
	in.RecentThreat = nil
	if ks := kinds(Detect(in, cfg)); len(ks) != 0 {
		t.Fatalf("aus: %v", ks)
	}
}

// TestWindowsKinds: Dienst/Aufgabe → persistence, gelöschtes Protokoll → critical,
// verdächtige Prozesse → ein Vorfall je Benutzer+Detail im Batch.
func TestWindowsKinds(t *testing.T) {
	base := time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)
	mk := func(kind, user, detail string, i int) shared.AuthEvent {
		return shared.AuthEvent{Time: base.Add(time.Duration(i) * time.Second), Kind: kind, User: user, Detail: detail, Service: "system"}
	}
	in := Input{Now: base.Add(time.Minute), Events: []shared.AuthEvent{
		mk("service_installed", "LocalSystem", "EvilSvc → C:\\e.exe (auto start)", 0),
		mk("task_created", "admin", "\\Updater → C:\\x.exe", 1),
		mk("audit_cleared", "admin", "Sicherheitsprotokoll gelöscht", 2),
		mk("suspicious_process", "bob", "verdächtige Argumente (-urlcache): certutil.exe", 3),
		mk("suspicious_process", "bob", "verdächtige Argumente (-urlcache): certutil.exe", 4),
		mk("powershell_suspicious", "", "downloadstring · IEX (New-Object …)", 5),
	}}
	got := Detect(in, DefaultConfig())
	ks := kinds(got)
	want := []string{"persistence", "persistence", "audit_cleared", "suspicious_process", "suspicious_process"}
	if len(ks) != len(want) {
		t.Fatalf("got %v", ks)
	}
	for i := range want {
		if ks[i] != want[i] {
			t.Fatalf("got %v, want %v", ks, want)
		}
	}
	if got[0].Title != "Dienst installiert: EvilSvc" || got[2].Severity != "critical" || got[3].Title != "Verdächtiger Prozess: verdächtige Argumente (-urlcache)" {
		t.Fatalf("titles: %+v", got)
	}
}

// TestPortscan: ab 10 verschiedenen Zielports einer IP im Fenster ein Vorfall, danach 1 h Ruhe;
// 9 Ports reichen nicht; Vorwissen aus der Datenbank zählt mit.
func TestPortscan(t *testing.T) {
	base := time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)
	drop := func(ip string, port int, i int) shared.AuthEvent {
		return shared.AuthEvent{Time: base.Add(time.Duration(i) * time.Second), Kind: "fw_drop", SourceIP: ip, Detail: "tcp/" + itoa(port), Service: "fw"}
	}
	var evs []shared.AuthEvent
	for p := 1; p <= 9; p++ {
		evs = append(evs, drop("203.0.113.5", p, p))
	}
	cfg := DefaultConfig()
	if ks := kinds(Detect(Input{Now: base.Add(time.Minute), Events: evs}, cfg)); len(ks) != 0 {
		t.Fatalf("9 Ports: %v", ks)
	}
	evs = append(evs, drop("203.0.113.5", 10, 10), drop("203.0.113.5", 11, 11))
	got := Detect(Input{Now: base.Add(time.Minute), Events: evs}, cfg)
	if ks := kinds(got); len(ks) != 1 || ks[0] != "portscan" || got[0].SourceIP != "203.0.113.5" {
		t.Fatalf("10 Ports: %v", ks)
	}
	// Vorwissen: 8 Ports aus der DB + 2 neue.
	prior := map[string][]PortHit{}
	for p := 1; p <= 8; p++ {
		prior["203.0.113.6"] = append(prior["203.0.113.6"], PortHit{Time: base.Add(-time.Minute), Port: "tcp/" + itoa(p)})
	}
	got = Detect(Input{Now: base.Add(time.Minute), Events: []shared.AuthEvent{drop("203.0.113.6", 20, 1), drop("203.0.113.6", 21, 2)}, PriorDrops: prior}, cfg)
	if ks := kinds(got); len(ks) != 1 || ks[0] != "portscan" {
		t.Fatalf("mit Vorwissen: %v", ks)
	}
	got = Detect(Input{Now: base.Add(time.Minute), Events: evs, RecentPortscan: map[string]time.Time{"203.0.113.5": base.Add(-10 * time.Minute)}}, cfg)
	if ks := kinds(got); len(ks) != 0 {
		t.Fatalf("Dedupe: %v", ks)
	}
}

func TestContainerExec(t *testing.T) {
	base := time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)
	evs := []shared.AuthEvent{
		{Time: base, Kind: "docker_exec", Detail: "web-1: /bin/sh -c id", Service: "docker"},
		{Time: base.Add(time.Second), Kind: "docker_exec", Detail: "web-1: /bin/sh -c ls", Service: "docker"},
		{Time: base.Add(2 * time.Second), Kind: "docker_exec", Detail: "db-1: bash", Service: "docker"},
		{Time: base.Add(3 * time.Second), Kind: "docker_event", Detail: "start web-1 (nginx)", Service: "docker"},
	}
	got := Detect(Input{Now: base.Add(time.Minute), Events: evs}, DefaultConfig())
	if ks := kinds(got); len(ks) != 2 || ks[0] != "container_exec" || got[0].Title != "Befehl im Container web-1" || got[1].Title != "Befehl im Container db-1" {
		t.Fatalf("got %v %+v", ks, got)
	}
}

func itoa(n int) string { return fmt.Sprint(n) }

func TestStateChange(t *testing.T) {
	base := time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)
	evs := []shared.AuthEvent{
		{Time: base, Kind: "state_change", Service: "ports", Detail: "+ tcp 0.0.0.0:4444 nc"},
		{Time: base.Add(time.Second), Kind: "state_change", Service: "uid0", Detail: "+ backdoor"},
		{Time: base.Add(2 * time.Second), Kind: "state_change", Service: "admins", Detail: "- sudo: alice"},
	}
	got := Detect(Input{Now: base.Add(time.Minute), Events: evs}, DefaultConfig())
	if len(got) != 3 || got[0].Severity != "warning" || got[1].Severity != "critical" || got[2].Severity != "warning" || got[0].Title != "Systemzustand: lauschende Ports geändert" {
		t.Fatalf("got %+v", got)
	}
	recent := map[string]time.Time{"state_change|Systemzustand: lauschende Ports geändert|+ tcp 0.0.0.0:4444 nc": base.Add(-time.Hour)}
	if got := Detect(Input{Now: base.Add(time.Minute), Events: evs, RecentTitles: recent}, DefaultConfig()); len(got) != 2 {
		t.Fatalf("dedupe: %+v", got)
	}
}

func TestRootkitMalware(t *testing.T) {
	base := time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)
	evs := []shared.AuthEvent{
		{Time: base, Kind: "rootkit", Service: "rootcheck", Detail: "versteckter Prozess: PID 4711"},
		{Time: base.Add(time.Second), Kind: "malware", Service: "clamdscan", Detail: "/var/www/x.php: Php.Malware.Webshell-1"},
	}
	got := Detect(Input{Now: base.Add(time.Minute), Events: evs}, DefaultConfig())
	if len(got) != 2 || got[0].Severity != "critical" || got[0].Title != "Rootkit-Hinweis: versteckter Prozess" || got[1].Title != "Malware gefunden: /var/www/x.php" {
		t.Fatalf("got %+v", got)
	}
	recent := map[string]time.Time{"rootkit|Rootkit-Hinweis: versteckter Prozess|versteckter Prozess: PID 4711": base.Add(-time.Hour)}
	if got := Detect(Input{Now: base.Add(time.Minute), Events: evs, RecentTitles: recent}, DefaultConfig()); len(got) != 1 {
		t.Fatalf("dedupe: %+v", got)
	}
}
