//go:build linux

package collect

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/boonkerz/roster/internal/shared"
)

// reposOS liest apt (sources.list + sources.list.d, einzeilig und deb822), dnf/yum,
// zypper, pacman und apk.
func reposOS(_ context.Context) []shared.PackageRepo {
	var out []shared.PackageRepo
	if b := readFile("/etc/apt/sources.list"); b != "" {
		out = append(out, parseAptSourcesList(b, "sources.list")...)
	}
	files, _ := filepath.Glob("/etc/apt/sources.list.d/*")
	sort.Strings(files)
	for _, f := range files {
		switch filepath.Ext(f) {
		case ".list":
			out = append(out, parseAptSourcesList(readFile(f), filepath.Base(f))...)
		case ".sources":
			out = append(out, parseDeb822Sources(readFile(f), filepath.Base(f))...)
		}
	}
	for _, pat := range []struct{ glob, typ string }{{"/etc/yum.repos.d/*.repo", "dnf"}, {"/etc/zypp/repos.d/*.repo", "zypper"}} {
		files, _ := filepath.Glob(pat.glob)
		sort.Strings(files)
		for _, f := range files {
			out = append(out, parseIniRepos(readFile(f), pat.typ)...)
		}
	}
	if b := readFile("/etc/pacman.conf"); b != "" {
		out = append(out, parsePacmanConf(b, readFile)...)
	}
	if b := readFile("/etc/apk/repositories"); b != "" {
		out = append(out, parseApkRepositories(b)...)
	}
	return out
}

// autoUpdatesOS ermittelt unattended-upgrades (apt) bzw. dnf-automatic.
func autoUpdatesOS(ctx context.Context) string {
	switch {
	case have("apt-get"):
		timer := systemctlEnabled(ctx, "apt-daily-upgrade.timer") || systemctlEnabled(ctx, "unattended-upgrades.service")
		return aptAutoUpdateStatus(readFile("/etc/apt/apt.conf.d/20auto-upgrades"),
			readFile("/etc/apt/apt.conf.d/50unattended-upgrades"), timer)
	case have("dnf"):
		timer := systemctlEnabled(ctx, "dnf-automatic.timer") || systemctlEnabled(ctx, "dnf-automatic-install.timer") ||
			systemctlEnabled(ctx, "dnf5-automatic.timer")
		return dnfAutomaticStatus(readFile("/etc/dnf/automatic.conf"), timer)
	}
	return "" // pacman/apk/zypper: kein verbreiteter Standard-Mechanismus
}

func systemctlEnabled(ctx context.Context, unit string) bool {
	if !have("systemctl") {
		return false
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return strings.TrimSpace(run(ctx, "systemctl", "is-enabled", unit)) == "enabled"
}

// osDetailsOS liest Codename und Variante aus /etc/os-release.
func osDetailsOS(_ context.Context) (build, codename, edition string) {
	rel := readFile("/etc/os-release")
	codename = osReleaseField(rel, "VERSION_CODENAME")
	if codename == "" {
		codename = osReleaseField(rel, "UBUNTU_CODENAME")
	}
	return "", codename, osReleaseField(rel, "VARIANT")
}

// kernelInstalled liefert den neuesten installierten Kernel (Verzeichnisse unter /lib/modules).
func kernelInstalled() string {
	entries, err := os.ReadDir("/lib/modules")
	if err != nil {
		return ""
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	return newestKernel(names)
}
