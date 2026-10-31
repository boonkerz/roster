//go:build windows

package collect

import (
	"context"
	"os/exec"
	"strconv"
	"strings"

	"github.com/boonkerz/roster/internal/shared"
)

// reposOS listet winget-Quellen (falls winget im Dienstkontext verfügbar ist).
func reposOS(ctx context.Context) []shared.PackageRepo {
	if _, err := exec.LookPath("winget"); err != nil {
		return nil
	}
	out, err := exec.CommandContext(ctx, "winget", "source", "list").Output()
	if err != nil {
		return nil
	}
	return parseWingetSources(string(out))
}

// autoUpdatesOS liest den Windows-Update-Automatikmodus (COM AutoUpdate.Settings) und
// die Gruppenrichtlinie NoAutoUpdate.
func autoUpdatesOS(ctx context.Context) string {
	const script = `$ProgressPreference='SilentlyContinue'; ` +
		`try { $l=(New-Object -ComObject Microsoft.Update.AutoUpdate).Settings.NotificationLevel } catch { $l=0 }; ` +
		`$p=(Get-ItemProperty 'HKLM:\SOFTWARE\Policies\Microsoft\Windows\WindowsUpdate\AU' -ErrorAction SilentlyContinue).NoAutoUpdate; ` +
		`"$l;$p"`
	out, err := psOutput(ctx, script)
	if err != nil {
		return ""
	}
	lvl, pol, _ := strings.Cut(strings.TrimSpace(string(out)), ";")
	n, _ := strconv.Atoi(strings.TrimSpace(lvl))
	return winAutoUpdateStatus(n, strings.TrimSpace(pol) == "1")
}

// osDetailsOS liest DisplayVersion, Build (CurrentBuild.UBR) und Edition aus der Registry.
func osDetailsOS(ctx context.Context) (build, codename, edition string) {
	const script = `$k=Get-ItemProperty 'HKLM:\SOFTWARE\Microsoft\Windows NT\CurrentVersion'; ` +
		`"$($k.DisplayVersion)|$($k.CurrentBuild)|$($k.UBR)|$($k.EditionID)|$($k.InstallationType)"`
	out, err := psOutput(ctx, script)
	if err != nil {
		return "", "", ""
	}
	f := strings.Split(strings.TrimSpace(string(out)), "|")
	if len(f) < 5 {
		return "", "", ""
	}
	codename = strings.TrimSpace(f[0])
	build = strings.TrimSpace(f[1])
	if ubr := strings.TrimSpace(f[2]); ubr != "" {
		build += "." + ubr
	}
	if codename != "" {
		build = codename + " (" + build + ")"
	}
	edition = strings.TrimSpace(f[3])
	if strings.EqualFold(strings.TrimSpace(f[4]), "Server") && !strings.HasPrefix(edition, "Server") {
		edition = "Server " + edition
	}
	return build, codename, edition
}

func kernelInstalled() string { return "" }
