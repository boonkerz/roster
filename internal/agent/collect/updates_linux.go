//go:build linux

package collect

import (
	"context"
	"os/exec"

	"github.com/boonkerz/roster/internal/shared"
)

// osUpdateList ermittelt aktualisierbare Pakete über das vorhandene Paketsystem.
// Es wird bewusst NICHT synchronisiert/aktualisiert (kein pacman -Sy / apt update),
// sondern nur der bereits bekannte Stand ausgewertet. Die Parser liegen in
// updates_parse.go (plattformneutral, getestet).
func osUpdateList(ctx context.Context) ([]shared.UpdateItem, bool) {
	switch {
	case have("checkupdates"): // pacman-contrib: nutzt eine temporäre DB, sicher
		return parsePacmanUpgrades(run(ctx, "checkupdates")), true
	case have("apt"):
		return parseAptUpgradable(run(ctx, "apt", "list", "--upgradable")), true
	case have("dnf"):
		return dnfUpdates(ctx), true
	case have("apk"):
		// --no-cache holt einen frischen Index, ohne ihn zu persistieren.
		return parseApkUpgradable(run(ctx, "apk", "--no-cache", "version", "-l", "<")), true
	case have("pacman"):
		return parsePacmanUpgrades(run(ctx, "pacman", "-Qu")), true
	default:
		return nil, false
	}
}

func dnfUpdates(ctx context.Context) []shared.UpdateItem {
	// check-update beendet sich mit Code 100, wenn Updates vorliegen -> stdout trotzdem nutzen.
	b, _ := exec.CommandContext(ctx, "dnf", "-q", "--cacheonly", "check-update").Output()
	// Sicherheits-Advisories aus den Metadaten (gleicher Cache, kein Netz).
	adv, _ := exec.CommandContext(ctx, "dnf", "-q", "--cacheonly", "updateinfo", "list", "--security").Output()
	return parseDnfCheckUpdate(string(b), parseDnfUpdateinfo(string(adv)))
}
