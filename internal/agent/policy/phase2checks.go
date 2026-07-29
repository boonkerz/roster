package policy

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/shirou/gopsutil/v4/host"

	"github.com/boonkerz/roster/internal/agent/collect"
	"github.com/boonkerz/roster/internal/shared"
)

// --- Zertifikat-Ablauf (cert) ---
// Config: host (oder url), port (Standard 443), min_days (Standard 14).
func certCheck(ctx context.Context, c shared.CheckSpec) shared.CheckResult {
	hn := strConfig(c, "host")
	if hn == "" {
		hn = strConfig(c, "url")
	}
	hn = strings.TrimPrefix(strings.TrimPrefix(hn, "https://"), "http://")
	if i := strings.IndexByte(hn, '/'); i >= 0 {
		hn = hn[:i] // Pfad abschneiden
	}
	if hn == "" {
		return unknownCheck(c, "Host (config.host) erforderlich")
	}
	port := 443
	if h, p, err := net.SplitHostPort(hn); err == nil {
		hn = h
		if n, e := strconv.Atoi(p); e == nil {
			port = n
		}
	}
	if v, ok := numConfig(c, "port"); ok && v > 0 {
		port = int(v)
	}
	minDays := 14.0
	if v, ok := numConfig(c, "min_days"); ok && v > 0 {
		minDays = v
	}

	d := &net.Dialer{Timeout: timeoutDur(c, 10*time.Second)}
	// InsecureSkipVerify: uns interessiert der Ablauf, nicht die Vertrauenskette
	// (funktioniert so auch für selbstsignierte Zertifikate).
	conn, err := tls.DialWithDialer(d, "tcp", net.JoinHostPort(hn, strconv.Itoa(port)),
		&tls.Config{ServerName: hn, InsecureSkipVerify: true}) //nolint:gosec
	if err != nil {
		return failCheck(c, 0, fmt.Sprintf("TLS-Verbindung zu %s:%d fehlgeschlagen: %v", hn, port, err))
	}
	defer conn.Close()
	certs := conn.ConnectionState().PeerCertificates
	if len(certs) == 0 {
		return failCheck(c, 0, "kein Zertifikat erhalten")
	}
	return evalCertDays(c, hn, time.Until(certs[0].NotAfter).Hours()/24, minDays)
}

func evalCertDays(c shared.CheckSpec, host string, days, minDays float64) shared.CheckResult {
	switch {
	case days < 0:
		return failCheck(c, days, fmt.Sprintf("Zertifikat %s ist seit %.0f Tagen abgelaufen", host, -days))
	case days < minDays:
		return failCheck(c, days, fmt.Sprintf("Zertifikat %s läuft in %.0f Tagen ab (min %.0f)", host, days, minDays))
	default:
		return passCheck(c, days, fmt.Sprintf("Zertifikat %s gültig, noch %.0f Tage", host, days))
	}
}

// --- Dienst läuft (service) ---
func serviceCheck(ctx context.Context, c shared.CheckSpec) shared.CheckResult {
	return evalServiceRunning(c, collect.ListServices(ctx))
}

func evalServiceRunning(c shared.CheckSpec, js string) shared.CheckResult {
	name := strConfig(c, "name")
	if name == "" {
		return unknownCheck(c, "Dienst-Name (config.name) erforderlich")
	}
	var d struct {
		Services []collect.ServiceInfo `json:"services"`
		Error    string                `json:"error"`
	}
	if json.Unmarshal([]byte(js), &d) != nil || d.Error != "" {
		return unknownCheck(c, "Dienstliste nicht lesbar")
	}
	low := strings.ToLower(name)
	running, found := false, false
	for _, s := range d.Services {
		if strings.Contains(strings.ToLower(s.Name), low) || strings.Contains(strings.ToLower(s.Display), low) {
			found = true
			if s.Running {
				running = true
			}
		}
	}
	switch {
	case running:
		return passCheck(c, 1, fmt.Sprintf("Dienst %q läuft", name))
	case found:
		return failCheck(c, 0, fmt.Sprintf("Dienst %q ist gestoppt", name))
	default:
		return failCheck(c, 0, fmt.Sprintf("Dienst %q nicht gefunden", name))
	}
}

// --- Prozess läuft (process) ---
func processCheck(ctx context.Context, c shared.CheckSpec) shared.CheckResult {
	return evalProcessRunning(c, collect.ListProcesses(ctx))
}

func evalProcessRunning(c shared.CheckSpec, js string) shared.CheckResult {
	name := strConfig(c, "name")
	if name == "" {
		return unknownCheck(c, "Prozess-Name (config.name) erforderlich")
	}
	var d struct {
		Processes []collect.ProcInfo `json:"processes"`
		Error     string             `json:"error"`
	}
	if json.Unmarshal([]byte(js), &d) != nil || d.Error != "" {
		return unknownCheck(c, "Prozessliste nicht lesbar")
	}
	low := strings.ToLower(name)
	n := 0
	for _, p := range d.Processes {
		if strings.Contains(strings.ToLower(p.Name), low) {
			n++
		}
	}
	if n > 0 {
		return passCheck(c, float64(n), fmt.Sprintf("Prozess %q läuft (%d)", name, n))
	}
	return failCheck(c, 0, fmt.Sprintf("Prozess %q läuft nicht", name))
}

// --- Uptime (uptime) ---
// Config: max_days (Standard 30) – failing, wenn die Uptime darüber liegt.
func uptimeCheck(ctx context.Context, c shared.CheckSpec) shared.CheckResult {
	up, err := host.UptimeWithContext(ctx)
	if err != nil {
		return unknownCheck(c, "Uptime nicht lesbar")
	}
	maxDays := 30.0
	if v, ok := numConfig(c, "max_days"); ok && v > 0 {
		maxDays = v
	}
	return evalUptime(c, float64(up), maxDays)
}

func evalUptime(c shared.CheckSpec, uptimeSec, maxDays float64) shared.CheckResult {
	days := uptimeSec / 86400
	if days > maxDays {
		return failCheck(c, days, fmt.Sprintf("Uptime %.1f Tage (max %.0f) – Neustart empfohlen", days, maxDays))
	}
	return passCheck(c, days, fmt.Sprintf("Uptime %.1f Tage", days))
}

// --- Neustart ausstehend (reboot) ---
func rebootCheck(_ context.Context, c shared.CheckSpec) shared.CheckResult {
	pending, known := collect.RebootPending()
	if !known {
		return unknownCheck(c, "Reboot-Status auf diesem System nicht ermittelbar")
	}
	if pending {
		return failCheck(c, 1, "Neustart ausstehend")
	}
	return passCheck(c, 0, "kein Neustart ausstehend")
}
