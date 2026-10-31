package collect

import (
	"net/url"
	"strconv"
	"strings"

	"github.com/boonkerz/roster/internal/shared"
)

// Reine Parser für Paketquellen, Auto-Update-Status und Kernelversionen (plattform-
// neutral, unit-getestet). Die OS-Dateien lesen nur Dateien/Befehle und reichen durch.

// distroHosts sind Hosts der Distributionen selbst; alles andere gilt als Fremd-Repo.
var distroHosts = []string{
	"ubuntu.com", "canonical.com", "debian.org", "raspbian.org", "raspberrypi.com", "linuxmint.com", "kali.org",
	"fedoraproject.org", "redhat.com", "centos.org", "rockylinux.org", "almalinux.org", "oracle.com", "amazonaws.com",
	"archlinux.org", "cachyos.org", "alpinelinux.org", "opensuse.org", "suse.com", "proxmox.com",
}

// isThirdPartyURL meldet, ob die Quelle nicht von der Distribution stammt.
func isThirdPartyURL(raw string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" {
		return false // lokale Pfade/cdrom nicht als fremd zählen
	}
	host := strings.ToLower(u.Hostname())
	for _, d := range distroHosts {
		if host == d || strings.HasSuffix(host, "."+d) {
			return false
		}
	}
	return true
}

// parseAptSourcesList liest das einzeilige apt-Format (sources.list, *.list):
//
//	deb [arch=amd64 signed-by=/usr/share/keyrings/docker.gpg] https://download.docker.com/linux/ubuntu jammy stable
//
// Auskommentierte deb-Zeilen („# deb …“) gelten als deaktiviert; deb-src wird übersprungen.
func parseAptSourcesList(text, file string) []shared.PackageRepo {
	var out []shared.PackageRepo
	for i, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(raw)
		enabled := true
		if strings.HasPrefix(line, "#") {
			line = strings.TrimSpace(strings.TrimLeft(line, "# "))
			enabled = false
		}
		if !strings.HasPrefix(line, "deb ") && !strings.HasPrefix(line, "deb\t") {
			continue
		}
		rest := strings.TrimSpace(line[4:])
		trusted := false
		if strings.HasPrefix(rest, "[") {
			end := strings.Index(rest, "]")
			if end < 0 {
				continue
			}
			trusted = strings.Contains(rest[:end], "trusted=yes")
			rest = strings.TrimSpace(rest[end+1:])
		}
		f := strings.Fields(rest)
		if len(f) < 2 {
			continue
		}
		suite := strings.Join(f[1:], " ")
		out = append(out, shared.PackageRepo{
			ID: file + ":" + strconv.Itoa(i+1), Name: f[1], URL: f[0], Type: "apt",
			Enabled: enabled, GPGCheck: !trusted, Suite: suite, ThirdParty: isThirdPartyURL(f[0]),
		})
	}
	return out
}

// parseDeb822Sources liest das deb822-Format (*.sources): Absätze mit Types/URIs/Suites/
// Components/Enabled/Trusted. Mehrere URIs ergeben mehrere Einträge.
func parseDeb822Sources(text, file string) []shared.PackageRepo {
	var out []shared.PackageRepo
	flush := func(st map[string]string, n int) {
		if st == nil || !strings.Contains(" "+st["types"]+" ", " deb ") {
			return
		}
		enabled := !strings.EqualFold(strings.TrimSpace(st["enabled"]), "no")
		trusted := strings.EqualFold(strings.TrimSpace(st["trusted"]), "yes")
		suite := strings.TrimSpace(st["suites"] + " " + st["components"])
		for _, uri := range strings.Fields(st["uris"]) {
			out = append(out, shared.PackageRepo{
				ID: file + "#" + strconv.Itoa(n), Name: strings.Fields(st["suites"] + " ?")[0], URL: uri, Type: "apt",
				Enabled: enabled, GPGCheck: !trusted, Suite: suite, ThirdParty: isThirdPartyURL(uri),
			})
		}
	}
	var st map[string]string
	n := 0
	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimRight(raw, " \t\r")
		if strings.TrimSpace(line) == "" {
			flush(st, n)
			st = nil
			continue
		}
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		if st == nil {
			st = map[string]string{}
			n++
		}
		st[strings.ToLower(strings.TrimSpace(k))] = strings.TrimSpace(v)
	}
	flush(st, n)
	return out
}

// parseIniRepos liest yum/dnf- und zypper-Repo-Dateien (INI: [id] name= baseurl= enabled= gpgcheck=).
func parseIniRepos(text, typ string) []shared.PackageRepo {
	var out []shared.PackageRepo
	var cur *shared.PackageRepo
	flush := func() {
		if cur != nil && cur.ID != "" {
			if cur.Name == "" {
				cur.Name = cur.ID
			}
			cur.ThirdParty = isThirdPartyURL(cur.URL)
			out = append(out, *cur)
		}
		cur = nil
	}
	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			flush()
			cur = &shared.PackageRepo{ID: line[1 : len(line)-1], Type: typ, Enabled: true, GPGCheck: true}
			continue
		}
		if cur == nil {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k, v = strings.TrimSpace(strings.ToLower(k)), strings.TrimSpace(v)
		switch k {
		case "name":
			cur.Name = v
		case "baseurl":
			if cur.URL == "" {
				cur.URL = strings.Fields(v + " ")[0]
			}
		case "mirrorlist", "metalink":
			if cur.URL == "" {
				cur.URL = v
			}
		case "enabled":
			cur.Enabled = v == "1" || strings.EqualFold(v, "true") || strings.EqualFold(v, "yes")
		case "gpgcheck":
			cur.GPGCheck = v == "1" || strings.EqualFold(v, "true") || strings.EqualFold(v, "yes")
		}
	}
	flush()
	return out
}

// pacmanOfficial sind die Repos der Distribution (Arch/CachyOS); alles andere ist fremd.
var pacmanOfficial = map[string]bool{
	"core": true, "extra": true, "multilib": true, "community": true, "testing": true, "core-testing": true,
	"extra-testing": true, "multilib-testing": true, "cachyos": true, "cachyos-v3": true, "cachyos-v4": true,
	"cachyos-core-v3": true, "cachyos-extra-v3": true, "cachyos-core-v4": true, "cachyos-extra-v4": true,
	"cachyos-znver4": true, "cachyos-core-znver4": true, "cachyos-extra-znver4": true,
}

// parsePacmanConf liest /etc/pacman.conf: jede Sektion außer [options] ist ein Repo;
// Server = URL direkt, Include = Mirrorlist (readInclude liefert deren ersten Server).
func parsePacmanConf(text string, readInclude func(path string) string) []shared.PackageRepo {
	var out []shared.PackageRepo
	var cur *shared.PackageRepo
	flush := func() {
		if cur != nil {
			cur.ThirdParty = !pacmanOfficial[strings.ToLower(cur.ID)]
			out = append(out, *cur)
		}
		cur = nil
	}
	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(raw)
		enabled := true
		if strings.HasPrefix(line, "#") {
			t := strings.TrimSpace(strings.TrimLeft(line, "# "))
			// Auskommentierte Sektion (z. B. #[multilib]) = deaktiviertes Repo; die
			// Beispiele der Stock-pacman.conf (#[repo-name], #[custom]) sind keine.
			if strings.HasPrefix(t, "[") && strings.HasSuffix(t, "]") && t != "[options]" && t != "[repo-name]" && t != "[custom]" {
				line, enabled = t, false
			} else {
				continue
			}
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			flush()
			name := line[1 : len(line)-1]
			if name == "options" {
				cur = nil
				continue
			}
			cur = &shared.PackageRepo{ID: name, Name: name, Type: "pacman", Enabled: enabled, GPGCheck: true}
			continue
		}
		if cur == nil {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k, v = strings.TrimSpace(k), strings.TrimSpace(v)
		switch k {
		case "Server":
			if cur.URL == "" {
				cur.URL = v
			}
		case "Include":
			if cur.URL == "" && readInclude != nil {
				cur.URL = firstPacmanServer(readInclude(v))
			}
			if cur.URL == "" {
				cur.URL = "include:" + v
			}
		case "SigLevel":
			if strings.Contains(v, "Never") {
				cur.GPGCheck = false
			}
		}
	}
	flush()
	return out
}

func firstPacmanServer(text string) string {
	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(raw)
		if strings.HasPrefix(line, "Server") {
			if _, v, ok := strings.Cut(line, "="); ok {
				return strings.TrimSpace(v)
			}
		}
	}
	return ""
}

// parseApkRepositories liest /etc/apk/repositories (eine URL je Zeile, „#“ = deaktiviert,
// „@tag url“ = getaggtes Repo).
func parseApkRepositories(text string) []shared.PackageRepo {
	var out []shared.PackageRepo
	for i, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		enabled := true
		if strings.HasPrefix(line, "#") {
			line = strings.TrimSpace(strings.TrimLeft(line, "# "))
			enabled = false
		}
		if strings.HasPrefix(line, "@") {
			if _, rest, ok := strings.Cut(line, " "); ok {
				line = strings.TrimSpace(rest)
			}
		}
		if !strings.Contains(line, "://") && !strings.HasPrefix(line, "/") {
			continue
		}
		name := line
		if i := strings.LastIndex(strings.TrimSuffix(line, "/"), "/"); i >= 0 {
			name = strings.TrimSuffix(line, "/")[i+1:]
		}
		out = append(out, shared.PackageRepo{ID: "apk:" + strconv.Itoa(i+1), Name: name, URL: line, Type: "apk",
			Enabled: enabled, GPGCheck: true, ThirdParty: isThirdPartyURL(line)})
	}
	return out
}

// parseWingetSources liest `winget source list`:
//
//	Name    Argument
//	-----------------------------------------
//	msstore https://storeedgefd.dsx.mp.microsoft.com/v9.0
//	winget  https://cdn.winget.microsoft.com/cache
func parseWingetSources(text string) []shared.PackageRepo {
	var out []shared.PackageRepo
	seenHeader := false
	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(raw)
		if strings.HasPrefix(line, "---") {
			seenHeader = true
			continue
		}
		if !seenHeader {
			continue
		}
		f := strings.Fields(line)
		if len(f) < 2 || !strings.Contains(f[1], "://") {
			continue
		}
		out = append(out, shared.PackageRepo{ID: f[0], Name: f[0], URL: f[1], Type: "winget", Enabled: true, GPGCheck: true,
			ThirdParty: f[0] != "winget" && f[0] != "msstore"})
	}
	return out
}

// aptAutoUpdateStatus leitet den Status aus /etc/apt/apt.conf.d/20auto-upgrades
// (APT::Periodic::Unattended-Upgrade "1") und 50unattended-upgrades (Origins) ab;
// timerEnabled = apt-daily-upgrade.timer bzw. unattended-upgrades aktiv.
func aptAutoUpdateStatus(auto, unattended string, timerEnabled bool) string {
	if !timerEnabled || !aptPeriodicOn(auto, "APT::Periodic::Unattended-Upgrade") {
		return "disabled"
	}
	// Standard: nur Security-Origins; ist „-updates“ unkommentiert erlaubt → alles.
	for _, raw := range strings.Split(unattended, "\n") {
		line := strings.TrimSpace(raw)
		if strings.HasPrefix(line, "//") {
			continue
		}
		if strings.Contains(line, "-updates\"") || strings.Contains(line, "a=stable\"") || strings.Contains(line, "n=${distro_codename}\"") {
			return "enabled"
		}
	}
	return "security"
}

func aptPeriodicOn(conf, key string) bool {
	for _, raw := range strings.Split(conf, "\n") {
		line := strings.TrimSpace(raw)
		if strings.HasPrefix(line, key) {
			return strings.Contains(line, "\"1\"")
		}
	}
	return false
}

// dnfAutomaticStatus leitet den Status aus dem Timer und /etc/dnf/automatic.conf ab.
func dnfAutomaticStatus(conf string, timerEnabled bool) string {
	if !timerEnabled {
		return "disabled"
	}
	get := func(key string) string {
		for _, raw := range strings.Split(conf, "\n") {
			k, v, ok := strings.Cut(strings.TrimSpace(raw), "=")
			if ok && strings.TrimSpace(k) == key {
				return strings.ToLower(strings.TrimSpace(v))
			}
		}
		return ""
	}
	switch {
	case get("apply_updates") == "yes" && get("upgrade_type") == "security":
		return "security"
	case get("apply_updates") == "yes":
		return "enabled"
	case get("download_updates") == "yes":
		return "download"
	}
	return "notify"
}

// winAutoUpdateStatus: NotificationLevel 1 = aus, 2 = benachrichtigen, 3 = herunterladen,
// 4 = automatisch installieren; Richtlinie NoAutoUpdate=1 schlägt alles.
func winAutoUpdateStatus(level int, noAutoUpdate bool) string {
	if noAutoUpdate {
		return "disabled"
	}
	switch level {
	case 4:
		return "enabled"
	case 3:
		return "download"
	case 2:
		return "notify"
	case 1:
		return "disabled"
	}
	return ""
}

// kernelVersionLess vergleicht Kernelversionen segmentweise numerisch
// („6.8.0-45-generic“ < „6.8.0-47-generic“, „6.17.5-2-cachyos“ > „6.9.3-arch1-1“).
func kernelVersionLess(a, b string) bool {
	as, bs := kernelSegments(a), kernelSegments(b)
	for i := 0; i < len(as) || i < len(bs); i++ {
		var x, y int
		if i < len(as) {
			x = as[i]
		}
		if i < len(bs) {
			y = bs[i]
		}
		if x != y {
			return x < y
		}
	}
	return false
}

func kernelSegments(v string) []int {
	var out []int
	cur := -1
	for _, r := range v {
		if r >= '0' && r <= '9' {
			if cur < 0 {
				cur = 0
			}
			cur = cur*10 + int(r-'0')
			continue
		}
		if cur >= 0 {
			out = append(out, cur)
			cur = -1
		}
	}
	if cur >= 0 {
		out = append(out, cur)
	}
	return out
}

// newestKernel liefert die höchste Version aus einer Liste (z. B. /lib/modules/*).
func newestKernel(versions []string) string {
	best := ""
	for _, v := range versions {
		v = strings.TrimSpace(v)
		if v == "" || len(kernelSegments(v)) == 0 {
			continue
		}
		if best == "" || kernelVersionLess(best, v) {
			best = v
		}
	}
	return best
}

// kernelUpgradePending: ein neuerer Kernel ist installiert als der laufende.
func kernelUpgradePending(running, installed string) bool {
	return running != "" && installed != "" && running != installed && kernelVersionLess(running, installed)
}

// osReleaseField liest einen Schlüssel aus /etc/os-release (Anführungszeichen entfernt).
func osReleaseField(text, key string) string {
	for _, raw := range strings.Split(text, "\n") {
		k, v, ok := strings.Cut(strings.TrimSpace(raw), "=")
		if ok && k == key {
			return strings.Trim(strings.TrimSpace(v), `"'`)
		}
	}
	return ""
}
