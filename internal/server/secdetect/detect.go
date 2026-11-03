// Package secdetect bewertet Anmelde-/Kontoereignisse und leitet Sicherheitsvorfälle ab
// (Brute-Force, Erfolg nach Brute-Force, Anmeldung von neuer IP, Kontoänderungen,
// Sperrungen). Reines Paket ohne Datenbank: der Aufrufer liefert Fenster-Zustand und
// bekannte IPs, damit die Regeln unit-testbar bleiben.
package secdetect

import (
	"fmt"
	"net"
	"sort"
	"strings"
	"time"

	"github.com/boonkerz/roster/internal/shared"
)

// Config sind die Schwellwerte (Einstellungen → Sicherheitsüberwachung).
type Config struct {
	BFThreshold     int           // Fehlversuche je Quell-IP im Fenster (Standard 10)
	BFWindow        time.Duration // Fenster (Standard 10 min)
	NewIPAlert      bool          // Anmeldung von neuer IP melden
	IgnorePrivateIP bool          // private Quell-IPs bei „neue IP“ ignorieren
	LearnDays       int           // Lernphase: so lange nach dem ersten Monitoring nur sammeln
	AdminGroups     []string      // Gruppen, deren Änderung als Warnung gilt
	// InvalidUserMax: ab so vielen Versuchen mit unbekanntem Benutzernamen je IP innerhalb
	// von 24 h gilt die IP als Brute-Force, unabhängig vom Zeitfenster (0 = aus). Fängt
	// verteilte, langsame Angriffe, die den Schwellwert je 10 min nie erreichen.
	InvalidUserMax int
	// ScanThreshold: verdächtige Anfragen (Kind probe, z. B. Web-Scanner) je IP im Fenster
	// bis zum Vorfall „scan“ (Standard 20).
	ScanThreshold int
	// ThreatAlert: Quell-IPs aus Blocklisten (Input.ThreatLookup) beim ersten Fehlversuch/Probe
	// als Vorfall „threat_listed“ melden – ohne Schwellwert.
	ThreatAlert bool
	// PortscanPorts: ab so vielen verschiedenen Zielports (Firewall-Drops) je Quell-IP im
	// Fenster gilt die IP als Portscan (Standard 10).
	PortscanPorts int
}

// InvalidUserWindow ist das Fenster der Regel „unbekannte Benutzer“.
const InvalidUserWindow = 24 * time.Hour

// DefaultConfig sind die Voreinstellungen.
func DefaultConfig() Config {
	return Config{BFThreshold: 10, BFWindow: 10 * time.Minute, NewIPAlert: true, LearnDays: 7, InvalidUserMax: 5, ScanThreshold: 20,
		AdminGroups: []string{"sudo", "wheel", "admin", "root", "administrators", "administratoren", "domain admins", "domänen-admins", "remote desktop users"}}
}

// Input ist der Zustand für einen Erkennungslauf (ein Gerät, ein Checkin).
type Input struct {
	Now    time.Time
	Events []shared.AuthEvent // neue Ereignisse, chronologisch
	// PriorFails: Fehlversuche je Quell-IP aus der Datenbank innerhalb des Fensters vor Now.
	PriorFails map[string][]time.Time
	// PriorInvalid: Versuche mit unbekanntem Benutzer je Quell-IP der letzten 24 h (ohne die neuen).
	PriorInvalid map[string][]time.Time
	// PriorProbes: verdächtige Anfragen je Quell-IP innerhalb des Fensters (ohne die neuen).
	PriorProbes map[string][]time.Time
	// RecentBF: Quell-IPs, für die in den letzten 60 min bereits ein Brute-Force-Vorfall existiert.
	RecentBF map[string]time.Time
	// RecentScan: Quell-IPs, für die in den letzten 60 min bereits ein Scan-Vorfall existiert.
	RecentScan map[string]time.Time
	// KnownIP meldet, ob (Benutzer, IP) für dieses Gerät schon einmal erfolgreich war.
	KnownIP func(user, ip string) bool
	// MonitoringSince ist der Beginn der Überwachung des Geräts (Lernphase für „neue IP“).
	MonitoringSince time.Time
	// ThreatLookup liefert die Blocklisten, in denen eine IP steht (nil = nicht gelistet).
	ThreatLookup func(ip string) []string
	// RecentThreat: Quell-IPs, für die in den letzten 24 h bereits ein threat_listed-Vorfall existiert.
	RecentThreat map[string]time.Time
	// PriorDrops: Firewall-Drops je Quell-IP innerhalb des Fensters (ohne die neuen).
	PriorDrops map[string][]PortHit
	// RecentPortscan: Quell-IPs mit Portscan-Vorfall in den letzten 60 min.
	RecentPortscan map[string]time.Time
	// RecentTitles: "kind|title|detail" → Zeit jüngerer Vorfälle (24 h) für zustandsartige
	// Arten (state_change, persistence, rootkit, malware) – verhindert Wiederholungen.
	RecentTitles map[string]time.Time
}

// PortHit ist ein Firewall-Drop (Zeit, Ziel "tcp/22").
type PortHit struct {
	Time time.Time
	Port string
}

// Incident ist ein abgeleiteter Vorfall.
type Incident struct {
	Time     time.Time
	Kind     string // brute_force | brute_force_success | new_source_ip | account_change | lockout | scan
	Severity string // info | warning | critical
	SourceIP string
	User     string
	Title    string
	Detail   string
}

func isFail(k string) bool { return k == "login_failed" || k == "invalid_user" }

// Detect wertet die Ereignisse aus und liefert die neuen Vorfälle.
func Detect(in Input, cfg Config) []Incident {
	if cfg.BFThreshold <= 0 {
		cfg.BFThreshold = 10
	}
	if cfg.BFWindow <= 0 {
		cfg.BFWindow = 10 * time.Minute
	}
	if cfg.ScanThreshold <= 0 {
		cfg.ScanThreshold = 20
	}
	if cfg.PortscanPorts <= 0 {
		cfg.PortscanPorts = 10
	}
	drops := map[string][]PortHit{}
	for ip, h := range in.PriorDrops {
		drops[ip] = append(drops[ip], h...)
	}
	portscanAt := map[string]time.Time{}
	for ip, t := range in.RecentPortscan {
		portscanAt[ip] = t
	}
	seenExec := map[string]bool{}
	fails := map[string][]time.Time{}
	invalid := map[string][]time.Time{}
	probes := map[string][]time.Time{}
	users := map[string]map[string]bool{}
	paths := map[string]map[string]bool{}
	for ip, ts := range in.PriorFails {
		fails[ip] = append(fails[ip], ts...)
	}
	for ip, ts := range in.PriorInvalid {
		invalid[ip] = append(invalid[ip], ts...)
	}
	for ip, ts := range in.PriorProbes {
		probes[ip] = append(probes[ip], ts...)
	}
	bfAt := map[string]time.Time{}
	for ip, t := range in.RecentBF {
		bfAt[ip] = t
	}
	scanAt := map[string]time.Time{}
	for ip, t := range in.RecentScan {
		scanAt[ip] = t
	}
	var out []Incident
	seenNewIP := map[string]bool{}
	seenProc := map[string]bool{}
	threatAt := map[string]time.Time{}
	for ip, t := range in.RecentThreat {
		threatAt[ip] = t
	}
	for _, e := range in.Events {
		ip := strings.TrimSpace(e.SourceIP)
		// Bekannte Angriffsquelle: gelistete IP beim ersten feindlichen Ereignis melden (ein
		// Vorfall je IP und 24 h); die übrigen Regeln laufen zusätzlich weiter.
		if cfg.ThreatAlert && in.ThreatLookup != nil && ip != "" && (isFail(e.Kind) || e.Kind == "probe" || e.Kind == "fw_drop") {
			if last, ok := threatAt[ip]; !ok || e.Time.Sub(last) >= 24*time.Hour {
				if feeds := in.ThreatLookup(ip); len(feeds) > 0 {
					threatAt[ip] = e.Time
					out = append(out, Incident{Time: e.Time, Kind: "threat_listed", Severity: "warning", SourceIP: ip, User: e.User,
						Title:  "Bekannte Angriffsquelle: " + ip,
						Detail: "Gelistet in: " + strings.Join(feeds, ", ") + " · erstes Ereignis: " + e.Kind + " über " + e.Service})
				}
			}
		}
		switch {
		case isFail(e.Kind) && ip != "":
			fails[ip] = append(fails[ip], e.Time)
			if users[ip] == nil {
				users[ip] = map[string]bool{}
			}
			if e.User != "" {
				users[ip][e.User] = true
			}
			if e.Kind == "invalid_user" {
				invalid[ip] = append(invalid[ip], e.Time)
			}
			n := countWithin(fails[ip], e.Time, cfg.BFWindow)
			ni := countWithin(invalid[ip], e.Time, InvalidUserWindow)
			if n >= cfg.BFThreshold || (cfg.InvalidUserMax > 0 && ni >= cfg.InvalidUserMax) {
				if last, ok := bfAt[ip]; ok && e.Time.Sub(last) < time.Hour {
					continue // schon gemeldet
				}
				bfAt[ip] = e.Time
				detail := fmt.Sprintf("%d fehlgeschlagene Anmeldungen innerhalb von %s über %s (Benutzer: %s)", n, cfg.BFWindow, e.Service, userList(users[ip]))
				if n < cfg.BFThreshold {
					detail = fmt.Sprintf("%d Anmeldeversuche mit unbekannten Benutzernamen innerhalb von 24 h über %s (Benutzer: %s)", ni, e.Service, userList(users[ip]))
				}
				out = append(out, Incident{Time: e.Time, Kind: "brute_force", Severity: "warning", SourceIP: ip,
					Title: fmt.Sprintf("Brute-Force von %s", ip), Detail: detail})
			}
		case e.Kind == "probe" && ip != "":
			probes[ip] = append(probes[ip], e.Time)
			if paths[ip] == nil {
				paths[ip] = map[string]bool{}
			}
			if e.Detail != "" {
				paths[ip][e.Detail] = true
			}
			n := countWithin(probes[ip], e.Time, cfg.BFWindow)
			if n >= cfg.ScanThreshold {
				if last, ok := scanAt[ip]; ok && e.Time.Sub(last) < time.Hour {
					continue
				}
				scanAt[ip] = e.Time
				out = append(out, Incident{Time: e.Time, Kind: "scan", Severity: "warning", SourceIP: ip,
					Title:  fmt.Sprintf("Angriffsmuster von %s", ip),
					Detail: fmt.Sprintf("%d verdächtige Anfragen innerhalb von %s über %s (%s)", n, cfg.BFWindow, e.Service, userList(paths[ip]))})
			}
		case e.Kind == "login_ok":
			if ip != "" {
				if last, ok := bfAt[ip]; ok && e.Time.Sub(last) < time.Hour || countWithin(fails[ip], e.Time, time.Hour) >= cfg.BFThreshold {
					out = append(out, Incident{Time: e.Time, Kind: "brute_force_success", Severity: "critical", SourceIP: ip, User: e.User,
						Title:  fmt.Sprintf("Anmeldung nach Brute-Force: %s von %s", e.User, ip),
						Detail: fmt.Sprintf("Erfolgreiche %s-Anmeldung von %s nach %d Fehlversuchen in der letzten Stunde.", e.Service, ip, countWithin(fails[ip], e.Time, time.Hour))})
					continue
				}
				// Lernphase: erst nach LearnDays Tagen Überwachung melden (0 = sofort, auch im ersten Import).
				learned := cfg.LearnDays <= 0 || in.MonitoringSince.IsZero() || e.Time.Sub(in.MonitoringSince) >= time.Duration(cfg.LearnDays)*24*time.Hour
				if cfg.NewIPAlert && in.KnownIP != nil && !isLoopback(ip) && !(cfg.IgnorePrivateIP && isPrivate(ip)) && learned {
					key := e.User + "@" + ip
					if !seenNewIP[key] && !in.KnownIP(e.User, ip) {
						seenNewIP[key] = true
						out = append(out, Incident{Time: e.Time, Kind: "new_source_ip", Severity: "warning", SourceIP: ip, User: e.User,
							Title:  fmt.Sprintf("Neue Quell-IP: %s von %s", e.User, ip),
							Detail: fmt.Sprintf("Erste erfolgreiche %s-Anmeldung von %s für %s auf diesem Gerät.", e.Service, ip, e.User)})
					}
				}
			}
		case e.Kind == "lockout":
			out = append(out, Incident{Time: e.Time, Kind: "lockout", Severity: "warning", SourceIP: ip, User: e.User,
				Title: fmt.Sprintf("Konto gesperrt: %s", e.User), Detail: e.Detail})
		case e.Kind == "user_added":
			out = append(out, Incident{Time: e.Time, Kind: "account_change", Severity: "warning", User: e.User,
				Title: fmt.Sprintf("Neues Benutzerkonto: %s", e.User), Detail: e.Detail})
		case e.Kind == "user_removed":
			out = append(out, Incident{Time: e.Time, Kind: "account_change", Severity: "warning", User: e.User,
				Title: fmt.Sprintf("Benutzerkonto gelöscht: %s", e.User), Detail: e.Detail})
		case e.Kind == "group_changed":
			sev := "info"
			if isAdminGroup(e.Detail, cfg.AdminGroups) {
				sev = "warning"
			}
			out = append(out, Incident{Time: e.Time, Kind: "account_change", Severity: sev, User: e.User,
				Title: fmt.Sprintf("Gruppenänderung: %s", strings.TrimSpace(e.User+" "+e.Detail)), Detail: e.Detail})
		case e.Kind == "fw_drop" && ip != "":
			drops[ip] = append(drops[ip], PortHit{Time: e.Time, Port: e.Detail})
			if last, ok := portscanAt[ip]; ok && e.Time.Sub(last) < time.Hour {
				continue
			}
			ports := map[string]bool{}
			var first []string
			for _, h := range drops[ip] {
				if e.Time.Sub(h.Time) <= cfg.BFWindow && !ports[h.Port] {
					ports[h.Port] = true
					if len(first) < 10 {
						first = append(first, h.Port)
					}
				}
			}
			if len(ports) >= cfg.PortscanPorts {
				portscanAt[ip] = e.Time
				out = append(out, Incident{Time: e.Time, Kind: "portscan", Severity: "warning", SourceIP: ip,
					Title:  "Portscan von " + ip,
					Detail: fmt.Sprintf("%d verschiedene Ports in %s: %s", len(ports), cfg.BFWindow, strings.Join(first, ", "))})
			}
		case e.Kind == "docker_exec":
			name, cmd, _ := strings.Cut(e.Detail, ": ")
			if seenExec[name] {
				continue
			}
			seenExec[name] = true
			out = append(out, Incident{Time: e.Time, Kind: "container_exec", Severity: "warning", User: e.User,
				Title: "Befehl im Container " + name, Detail: cmd})
		case e.Kind == "state_change":
			sev := "warning"
			if (e.Service == "uid0" || e.Service == "admins") && strings.Contains(e.Detail, "+ ") {
				sev = "critical"
			}
			inc := Incident{Time: e.Time, Kind: "state_change", Severity: sev, Title: "Systemzustand: " + stateLabel(e.Service) + " geändert", Detail: e.Detail}
			if dup(in, inc) {
				continue
			}
			out = append(out, inc)
		case e.Kind == "rootkit":
			inc := Incident{Time: e.Time, Kind: "rootkit", Severity: "critical", Title: "Rootkit-Hinweis: " + firstField(e.Detail), Detail: e.Detail}
			if dup(in, inc) {
				continue
			}
			out = append(out, inc)
		case e.Kind == "malware":
			inc := Incident{Time: e.Time, Kind: "malware", Severity: "critical", Title: "Malware gefunden: " + firstField(e.Detail), Detail: e.Detail + " (" + e.Service + ")"}
			if dup(in, inc) {
				continue
			}
			out = append(out, inc)
		case e.Kind == "service_installed":
			out = append(out, Incident{Time: e.Time, Kind: "persistence", Severity: "warning", User: e.User,
				Title: "Dienst installiert: " + firstField(e.Detail), Detail: e.Detail})
		case e.Kind == "task_created":
			out = append(out, Incident{Time: e.Time, Kind: "persistence", Severity: "warning", User: e.User,
				Title: "Geplante Aufgabe angelegt: " + firstField(e.Detail), Detail: e.Detail})
		case e.Kind == "audit_cleared":
			out = append(out, Incident{Time: e.Time, Kind: "audit_cleared", Severity: "critical", User: e.User,
				Title: "Sicherheitsprotokoll gelöscht von " + orDash(e.User), Detail: e.Detail})
		case e.Kind == "suspicious_process" || e.Kind == "powershell_suspicious":
			key := e.User + "|" + e.Detail
			if seenProc[key] {
				continue
			}
			seenProc[key] = true
			title := "Verdächtiger Prozess"
			if e.Kind == "powershell_suspicious" {
				title = "Verdächtiges PowerShell-Skript"
			}
			out = append(out, Incident{Time: e.Time, Kind: "suspicious_process", Severity: "warning", User: e.User,
				Title: title + ": " + firstField(e.Detail), Detail: e.Detail})
		case e.Kind == "password_changed" || e.Kind == "user_modified":
			out = append(out, Incident{Time: e.Time, Kind: "account_change", Severity: "info", User: e.User,
				Title: fmt.Sprintf("Kontoänderung: %s", e.User), Detail: strings.TrimSpace(e.Kind + " " + e.Detail)})
		}
	}
	return out
}

func countWithin(ts []time.Time, at time.Time, window time.Duration) int {
	n := 0
	for _, t := range ts {
		if !t.After(at) && at.Sub(t) <= window {
			n++
		}
	}
	return n
}

func userList(m map[string]bool) string {
	if len(m) == 0 {
		return "–"
	}
	var us []string
	for u := range m {
		us = append(us, u)
	}
	sort.Strings(us)
	if len(us) > 5 {
		return strings.Join(us[:5], ", ") + fmt.Sprintf(" … (+%d)", len(us)-5)
	}
	return strings.Join(us, ", ")
}

func isAdminGroup(detail string, groups []string) bool {
	d := strings.ToLower(detail)
	for _, g := range groups {
		g = strings.ToLower(strings.TrimSpace(g))
		if g != "" && (strings.Contains(d, "group "+g) || strings.HasSuffix(d, " "+g) || d == g) {
			return true
		}
	}
	return false
}

func isLoopback(ip string) bool {
	p := net.ParseIP(ip)
	return p != nil && p.IsLoopback()
}

// isPrivate: RFC1918, ULA, Link-local.
func isPrivate(ip string) bool {
	p := net.ParseIP(ip)
	return p != nil && (p.IsPrivate() || p.IsLinkLocalUnicast())
}

// Private meldet, ob eine IP privat/lokal ist (für Einstellungen/Active Response).
func Private(ip string) bool { return isPrivate(ip) || isLoopback(ip) }

// firstField liefert den Teil eines Details vor dem ersten " → " bzw. ": " (Name des
// Dienstes/der Aufgabe, Grund beim Prozess) – für kurze Vorfallstitel.
func firstField(detail string) string {
	d := detail
	for _, sep := range []string{" → ", ": "} {
		if i := strings.Index(d, sep); i > 0 {
			d = d[:i]
		}
	}
	if len(d) > 80 {
		d = d[:80] + "…"
	}
	return d
}

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

// stateLabel übersetzt die Kategorie des Systemzustands in den Vorfallstitel.
func stateLabel(cat string) string {
	switch cat {
	case "ports":
		return "lauschende Ports"
	case "modules":
		return "Kernelmodule"
	case "cron":
		return "Cron-Einträge"
	case "uid0":
		return "UID-0-Konten"
	case "admins":
		return "Administratorkonten"
	case "units", "services":
		return "Autostart-Dienste"
	case "tasks":
		return "geplante Aufgaben"
	case "launchd":
		return "launchd-Dienste"
	}
	return cat
}

// dup meldet, ob ein gleicher Vorfall (Art, Titel, Detail) in RecentTitles bekannt ist.
func dup(in Input, inc Incident) bool {
	if in.RecentTitles == nil {
		return false
	}
	_, ok := in.RecentTitles[inc.Kind+"|"+inc.Title+"|"+inc.Detail]
	return ok
}
