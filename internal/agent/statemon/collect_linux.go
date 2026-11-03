//go:build linux

package statemon

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

var adminGroups = map[string]bool{"sudo": true, "wheel": true, "adm": true, "root": true}

func collectors() map[string]func(context.Context) []string {
	return map[string]func(context.Context) []string{
		"ports":   collectPorts,
		"modules": collectModules,
		"cron":    collectCron,
		"uid0":    func(context.Context) []string { b, _ := os.ReadFile("/etc/passwd"); return ParsePasswdUID0(string(b)) },
		"admins": func(context.Context) []string {
			g, _ := os.ReadFile("/etc/group")
			p, _ := os.ReadFile("/etc/passwd")
			return ParseGroupMembers(string(g), string(p), adminGroups)
		},
		"units": collectUnits,
	}
}

func collectModules(context.Context) []string {
	b, err := os.ReadFile("/proc/modules")
	if err != nil {
		return nil
	}
	var out []string
	for _, l := range strings.Split(string(b), "\n") {
		if f := strings.Fields(l); len(f) > 0 {
			out = append(out, f[0])
		}
	}
	return out
}

func collectCron(context.Context) []string {
	var files []string
	for _, p := range []string{"/etc/crontab"} {
		files = append(files, p)
	}
	for _, pat := range []string{"/etc/cron.d/*", "/var/spool/cron/crontabs/*", "/var/spool/cron/*"} {
		m, _ := filepath.Glob(pat)
		files = append(files, m...)
	}
	var out []string
	for _, f := range files {
		st, err := os.Stat(f)
		if err != nil || st.IsDir() {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		out = append(out, ParseCron(f, string(b))...)
	}
	return out
}

func collectUnits(ctx context.Context) []string {
	cctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	var out []string
	for _, typ := range []string{"service", "timer", "socket"} {
		b, err := exec.CommandContext(cctx, "systemctl", "list-unit-files", "--type="+typ, "--state=enabled", "--no-legend", "--plain").Output()
		if err != nil {
			continue
		}
		out = append(out, ParseUnitFiles(string(b))...)
	}
	return out
}
