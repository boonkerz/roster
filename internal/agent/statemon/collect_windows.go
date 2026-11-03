//go:build windows

package statemon

import (
	"context"
	"os/exec"
	"strings"
	"time"
)

func collectors() map[string]func(context.Context) []string {
	return map[string]func(context.Context) []string{
		"ports":    collectPorts,
		"admins":   collectAdmins,
		"services": collectAutoServices,
		"tasks":    collectTasks,
	}
}

func run(ctx context.Context, name string, args ...string) string {
	cctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	out, _ := exec.CommandContext(cctx, name, args...).Output()
	return string(out)
}

// collectAdmins: `net localgroup Administrators` (PS3-/Server-2012-sicher; DE-Systeme kennen
// den englischen Namen ebenfalls, da die Gruppe über die SID aufgelöst wird).
func collectAdmins(ctx context.Context) []string {
	out := run(ctx, "net", "localgroup", "Administrators")
	if strings.TrimSpace(out) == "" {
		out = run(ctx, "net", "localgroup", "Administratoren")
	}
	return ParseNetLocalgroup(out)
}

// collectAutoServices: automatisch startende Dienste mit Pfad (WMI, ohne Get-Service -StartType).
func collectAutoServices(ctx context.Context) []string {
	out := run(ctx, "powershell", "-NoProfile", "-NonInteractive", "-Command",
		`$ProgressPreference='SilentlyContinue'; Get-WmiObject Win32_Service | Where-Object { $_.StartMode -eq 'Auto' } | ForEach-Object { $_.Name + '=' + $_.PathName }`)
	var names []string
	for _, l := range strings.Split(out, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			names = append(names, l)
		}
	}
	return names
}

func collectTasks(ctx context.Context) []string {
	return ParseSchtasksCSV(run(ctx, "schtasks", "/query", "/fo", "csv", "/nh"))
}
