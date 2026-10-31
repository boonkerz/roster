package collect

import (
	"regexp"
	"strings"

	"github.com/boonkerz/roster/internal/shared"
)

// Reine Parser für die Ausgaben der Paketmanager – ohne Build-Tag, damit sie auf
// jeder Plattform unit-getestet werden können. Die OS-spezifischen Dateien rufen
// nur die Befehle auf und reichen die Ausgabe hier durch.

// parseAptUpgradable liest `apt list --upgradable`:
//
//	bash/jammy-updates,jammy-security 5.1-6ubuntu1.1 amd64 [upgradable from: 5.1-6ubuntu1]
//
// Sicherheits-Update = eine der Suites enthält „security“ (Ubuntu: jammy-security,
// Debian: bookworm-security, ESM: jammy-apps-security).
func parseAptUpgradable(out string) []shared.UpdateItem {
	var items []shared.UpdateItem
	for _, l := range nonEmptyLines(out) {
		if strings.HasPrefix(l, "Listing") || !strings.Contains(l, "/") {
			continue
		}
		f := strings.Fields(l)
		name, suites, _ := strings.Cut(f[0], "/")
		if name == "" {
			continue
		}
		it := shared.UpdateItem{Name: name, Severity: "Other", Source: suites}
		if len(f) >= 2 {
			it.Version = f[1]
		}
		if i := strings.Index(l, "upgradable from:"); i >= 0 {
			it.CurrentVersion = strings.Trim(strings.TrimSpace(l[i+len("upgradable from:"):]), "[]")
		}
		for _, s := range strings.Split(suites, ",") {
			if strings.Contains(strings.ToLower(s), "security") {
				it.Security = true
				it.Severity = "Important"
				break
			}
		}
		items = append(items, it)
	}
	return items
}

// dnfAdvisory ist ein Sicherheits-Advisory aus `dnf updateinfo list --security`.
type dnfAdvisory struct {
	ID       string
	Severity string // Critical | Important | Moderate | Low
}

// parseDnfUpdateinfo liest `dnf -q updateinfo list --security`:
//
//	FEDORA-2024-1a2b3c4d Important/Sec. bash-5.2.26-3.fc40.x86_64
//
// und liefert je Paketname das Advisory. Kopfzeilen („Last metadata …“) fallen raus.
func parseDnfUpdateinfo(out string) map[string]dnfAdvisory {
	m := map[string]dnfAdvisory{}
	for _, l := range nonEmptyLines(out) {
		f := strings.Fields(l)
		if len(f) < 3 || !strings.Contains(f[1], "/") {
			continue
		}
		sev, kind, _ := strings.Cut(f[1], "/")
		if !strings.HasPrefix(strings.ToLower(kind), "sec") {
			continue
		}
		name := rpmName(f[2])
		if name == "" {
			continue
		}
		// Höchsten Schweregrad behalten, wenn mehrere Advisories dasselbe Paket betreffen.
		if cur, ok := m[name]; ok && severityRankRPM(cur.Severity) >= severityRankRPM(sev) {
			continue
		}
		m[name] = dnfAdvisory{ID: f[0], Severity: sev}
	}
	return m
}

func severityRankRPM(s string) int {
	switch strings.ToLower(s) {
	case "critical":
		return 4
	case "important":
		return 3
	case "moderate":
		return 2
	case "low":
		return 1
	}
	return 0
}

// rpmName trennt den Paketnamen aus einem NEVRA-String (name-version-release.arch).
func rpmName(nevra string) string {
	// Arch abschneiden (letzter Punkt-Teil), dann die letzten zwei '-'-Segmente
	// (version, release) entfernen.
	if i := strings.LastIndex(nevra, "."); i > 0 {
		nevra = nevra[:i]
	}
	parts := strings.Split(nevra, "-")
	if len(parts) < 3 {
		return nevra
	}
	return strings.Join(parts[:len(parts)-2], "-")
}

// parseDnfCheckUpdate liest `dnf -q check-update`:
//
//	bash.x86_64    5.2.26-3.fc40    updates
//
// Zeilen mit „Obsoleting Packages“ und Fortsetzungszeilen (ohne Punkt im Namen) fallen
// raus. advisories (aus parseDnfUpdateinfo) markiert Sicherheits-Updates.
func parseDnfCheckUpdate(out string, advisories map[string]dnfAdvisory) []shared.UpdateItem {
	var items []shared.UpdateItem
	for _, l := range nonEmptyLines(out) {
		f := strings.Fields(l)
		if len(f) < 3 || !strings.Contains(f[0], ".") || strings.HasPrefix(l, "Obsoleting") {
			continue
		}
		name := f[0]
		if i := strings.LastIndex(name, "."); i > 0 {
			name = name[:i]
		}
		it := shared.UpdateItem{Name: name, Severity: "Other", Version: f[1], Source: f[2]}
		if adv, ok := advisories[name]; ok {
			it.Security = true
			it.Source = f[2] + " " + adv.ID
			switch strings.ToLower(adv.Severity) {
			case "critical":
				it.Severity = "Critical"
			default:
				it.Severity = "Important"
			}
		}
		items = append(items, it)
	}
	return items
}

// parsePacmanUpgrades liest `checkupdates` bzw. `pacman -Qu`:
//
//	bash 5.2.026-2 -> 5.2.026-3
//
// pacman -Qu hängt bei ignorierten Paketen „[ignored]“ an – diese fallen raus.
func parsePacmanUpgrades(out string) []shared.UpdateItem {
	var items []shared.UpdateItem
	for _, l := range nonEmptyLines(out) {
		if strings.Contains(l, "[ignored]") {
			continue
		}
		f := strings.Fields(l)
		if len(f) == 0 {
			continue
		}
		it := shared.UpdateItem{Name: f[0], Severity: "Other"}
		if len(f) >= 4 && f[2] == "->" {
			it.CurrentVersion = f[1]
			it.Version = f[3]
		}
		items = append(items, it)
	}
	return items
}

var apkPkgVer = regexp.MustCompile(`^(.*)-([^-]+-r\d+)$`)

// parseApkUpgradable liest `apk version -l '<'`:
//
//	bash-5.2.26-r0 < 5.2.26-r1
//
// Datenzeilen enthalten den „<“-Operator; Kopf- und fetch-Zeilen nicht.
func parseApkUpgradable(out string) []shared.UpdateItem {
	var items []shared.UpdateItem
	for _, l := range nonEmptyLines(out) {
		f := strings.Fields(l)
		if len(f) < 3 || f[1] != "<" {
			continue
		}
		it := shared.UpdateItem{Name: f[0], Severity: "Other", Version: f[2]}
		if m := apkPkgVer.FindStringSubmatch(f[0]); m != nil {
			it.Name, it.CurrentVersion = m[1], m[2]
		}
		items = append(items, it)
	}
	return items
}

// parseSoftwareUpdateList liest `softwareupdate -l` (macOS):
//
//   - Label: macOS Sonoma 14.6.1-23G93
//     Title: macOS Sonoma 14.6.1, Version: 14.6.1, Size: 1234KiB, Recommended: YES, Action: restart,
//
// Sicherheits-Update = Label/Titel enthält „Security“ (z. B. „Security Response“,
// „Safari … Security“).
func parseSoftwareUpdateList(out string) []shared.UpdateItem {
	var items []shared.UpdateItem
	for _, l := range nonEmptyLines(out) {
		if i := strings.Index(l, "Label:"); i >= 0 {
			items = append(items, shared.UpdateItem{Name: strings.TrimSpace(l[i+len("Label:"):]), Severity: "Other"})
			continue
		}
		if len(items) == 0 || !strings.HasPrefix(l, "Title:") {
			continue
		}
		it := &items[len(items)-1]
		for _, part := range strings.Split(l, ",") {
			k, v, ok := strings.Cut(strings.TrimSpace(part), ":")
			if !ok {
				continue
			}
			v = strings.TrimSpace(v)
			switch strings.TrimSpace(k) {
			case "Version":
				it.Version = v
			case "Recommended":
				if strings.EqualFold(v, "YES") {
					it.Source = "recommended"
				}
			}
		}
	}
	for i := range items {
		if strings.Contains(strings.ToLower(items[i].Name), "security") {
			items[i].Security = true
			items[i].Severity = "Important"
		}
	}
	return items
}

// classifyWindowsUpdate leitet aus den WUA-Kategorien Security/Kind ab:
// „Security Updates“/„Critical Updates“ (oder ein MSRC-Schweregrad) = Sicherheits-
// Update; „Definition Updates“ = definition; „Drivers“ = driver.
func classifyWindowsUpdate(categories []string, msrcSeverity string) (security bool, kind string) {
	for _, c := range categories {
		switch strings.ToLower(strings.TrimSpace(c)) {
		case "security updates", "critical updates":
			security = true
		case "definition updates":
			kind = "definition"
		case "drivers", "driver":
			kind = "driver"
		}
	}
	if msrcSeverity != "" && kind == "" {
		security = true
	}
	return security, kind
}

// nonEmptyLines liefert die getrimmten, nicht-leeren Zeilen eines Textes
// (plattformneutral, auch von den Windows-Collectoren nutzbar).
func nonEmptyLines(s string) []string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if t := strings.TrimSpace(l); t != "" {
			out = append(out, t)
		}
	}
	return out
}

// securityCount zählt die Sicherheits-Updates einer Liste.
func securityCount(items []shared.UpdateItem) int {
	n := 0
	for _, it := range items {
		if it.Security {
			n++
		}
	}
	return n
}
