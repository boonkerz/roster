//go:build linux

package collect

import (
	"context"
	"os/exec"
	"strings"
)

// InstallUpdates installiert ausstehende Updates über den vorhandenen Paketmanager.
// Ist names leer, werden alle Updates installiert, sonst nur die genannten Pakete.
// full=true nutzt bei apt „full-upgrade" (dist-upgrade), das auch Updates mit neuen
// Abhängigkeiten / Paketwechseln durchzieht (z. B. Kernel); false = konservatives
// „upgrade" (entfernt nie etwas). Läuft mit den Rechten des Agent-Dienstes (i.d.R. root).
// phased=true nimmt unter Ubuntu auch per Phasing zurückgestellte Updates mit
// (APT::Get::Always-Include-Phased-Updates).
func InstallUpdates(ctx context.Context, names []string, full, phased bool) (int, string) {
	aptOpt := ""
	if phased {
		aptOpt = " -o APT::Get::Always-Include-Phased-Updates=true"
	}
	pkgs := strings.Join(names, " ")
	specific := len(names) > 0
	var cmd *exec.Cmd
	switch {
	case have("pacman"):
		if specific {
			cmd = exec.CommandContext(ctx, "sh", "-c", "pacman -S --noconfirm "+pkgs)
		} else {
			cmd = exec.CommandContext(ctx, "pacman", "-Syu", "--noconfirm")
		}
	case have("apt-get"):
		if specific {
			cmd = exec.CommandContext(ctx, "sh", "-c",
				"apt-get update && DEBIAN_FRONTEND=noninteractive apt-get"+aptOpt+" install --only-upgrade -y "+pkgs)
		} else {
			mode := "upgrade"
			if full {
				mode = "dist-upgrade"
			}
			cmd = exec.CommandContext(ctx, "sh", "-c",
				"apt-get update && DEBIAN_FRONTEND=noninteractive apt-get"+aptOpt+" -y "+mode)
		}
	case have("dnf"):
		if specific {
			cmd = exec.CommandContext(ctx, "sh", "-c", "dnf -y upgrade "+pkgs)
		} else {
			cmd = exec.CommandContext(ctx, "dnf", "-y", "upgrade")
		}
	case have("apk"):
		if specific {
			// apk upgrade erwartet den bloßen Paketnamen, nicht den voll
			// versionierten Token (z. B. "busybox", nicht "busybox-1.36.0-r9").
			cmd = exec.CommandContext(ctx, "sh", "-c", "apk update && apk upgrade "+apkBaseNames(names))
		} else {
			cmd = exec.CommandContext(ctx, "sh", "-c", "apk update && apk upgrade")
		}
	default:
		return -1, "kein unterstützter Paketmanager gefunden"
	}
	if cmd == nil {
		return -1, "kein unterstützter Paketmanager gefunden"
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return ee.ExitCode(), capOutput(string(out))
		}
		return -1, capOutput(string(out) + err.Error())
	}
	return 0, capOutput(string(out))
}

// apkBaseNames wandelt voll versionierte apk-Tokens (wie sie `apk version -l`
// liefert) in bloße Paketnamen um, getrennt durch Leerzeichen. Das Suffix
// "-<version>-r<rev>" wird entfernt; sieht ein Token nicht danach aus, bleibt es
// unverändert (z. B. wenn schon ein bloßer Name übergeben wurde).
func apkBaseNames(names []string) string {
	out := make([]string, 0, len(names))
	for _, n := range names {
		out = append(out, apkBaseName(n))
	}
	return strings.Join(out, " ")
}

func apkBaseName(s string) string {
	i := strings.LastIndex(s, "-")
	if i < 0 || i == len(s)-1 {
		return s
	}
	rev := s[i+1:] // erwartet "rN"
	if len(rev) < 2 || rev[0] != 'r' || !isDigits(rev[1:]) {
		return s
	}
	j := strings.LastIndex(s[:i], "-")
	if j < 0 || i == j+1 {
		return s
	}
	ver := s[j+1 : i] // erwartet Versionsteil, beginnend mit Ziffer
	if ver == "" || ver[0] < '0' || ver[0] > '9' {
		return s
	}
	return s[:j]
}

func isDigits(s string) bool {
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return s != ""
}
