//go:build linux

package collect

import (
	"context"
	"os/exec"
	"strings"

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
		items := parseAptUpgradable(run(ctx, "apt", "list", "--upgradable"))
		// Ubuntu Phased Updates: die Simulation nennt zurückgestellte Pakete.
		markPhased(items, parseAptPhased(run(ctx, "apt-get", "-s", "-o", "Debug::NoLocking=1", "dist-upgrade")))
		return items, true
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

// parseAptPhased liest aus `apt-get -s dist-upgrade` den Block „The following upgrades have
// been deferred due to phasing:“ (eingerückte Paketnamen bis zur nächsten nicht eingerückten
// Zeile); auch die deutsche Meldung wird erkannt.
func parseAptPhased(out string) map[string]bool {
	phased := map[string]bool{}
	in := false
	for _, l := range strings.Split(out, "\n") {
		low := strings.ToLower(l)
		if strings.Contains(low, "deferred due to phasing") || strings.Contains(low, "aufgrund von phasing zurückgestellt") {
			in = true
			continue
		}
		if in {
			if !strings.HasPrefix(l, " ") || strings.TrimSpace(l) == "" {
				in = false
				continue
			}
			for _, f := range strings.Fields(l) {
				phased[strings.SplitN(f, ":", 2)[0]] = true
			}
		}
	}
	return phased
}

func markPhased(items []shared.UpdateItem, phased map[string]bool) {
	for i := range items {
		if phased[items[i].Name] || phased[strings.SplitN(items[i].Name, ":", 2)[0]] {
			items[i].Phased = true
		}
	}
}
