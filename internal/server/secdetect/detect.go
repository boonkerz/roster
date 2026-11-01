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
	for _, e := range in.Events {
		ip := strings.TrimSpace(e.SourceIP)
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
