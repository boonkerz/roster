//go:build linux || darwin

package collect

import (
	"context"
	"os/exec"
	"strings"

	"github.com/boonkerz/roster/internal/shared"
)

func have(bin string) bool {
	_, err := exec.LookPath(bin)
	return err == nil
}

func run(ctx context.Context, name string, args ...string) string {
	out, err := exec.CommandContext(ctx, name, args...).Output()
	if err != nil {
		return ""
	}
	return string(out)
}

// unixPrinters listet CUPS-Drucker (sofern lpstat vorhanden).
func unixPrinters(ctx context.Context) []shared.Printer {
	if !have("lpstat") {
		return nil
	}
	var ps []shared.Printer
	for _, name := range nonEmptyLines(run(ctx, "lpstat", "-e")) {
		ps = append(ps, shared.Printer{Name: name})
	}
	return ps
}

// unixUsers liefert die eindeutigen, aktuell angemeldeten Benutzer (via who).
func unixUsers(ctx context.Context) []string {
	seen := map[string]bool{}
	var users []string
	for _, line := range nonEmptyLines(run(ctx, "who")) {
		f := strings.Fields(line)
		if len(f) > 0 && !seen[f[0]] {
			seen[f[0]] = true
			users = append(users, f[0])
		}
	}
	return users
}
