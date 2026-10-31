//go:build darwin

package collect

import (
	"context"
	"os"
	"strings"

	"github.com/boonkerz/roster/internal/shared"
)

// reposOS listet Homebrew-Taps (brew verweigert root; dann leer).
func reposOS(ctx context.Context) []shared.PackageRepo {
	if os.Geteuid() == 0 || !have("brew") {
		return nil
	}
	var out []shared.PackageRepo
	for _, tap := range nonEmptyLines(run(ctx, "brew", "tap")) {
		user, repo, ok := strings.Cut(tap, "/")
		url := ""
		if ok {
			url = "https://github.com/" + user + "/homebrew-" + repo
		}
		out = append(out, shared.PackageRepo{ID: tap, Name: tap, URL: url, Type: "brew", Enabled: true, GPGCheck: false,
			ThirdParty: user != "homebrew"})
	}
	return out
}

// autoUpdatesOS liest die Softwareupdate-Voreinstellungen.
func autoUpdatesOS(ctx context.Context) string {
	get := func(key string) string {
		return strings.TrimSpace(run(ctx, "defaults", "read", "/Library/Preferences/com.apple.SoftwareUpdate", key))
	}
	switch {
	case get("AutomaticallyInstallMacOSUpdates") == "1":
		return "enabled"
	case get("CriticalUpdateInstall") == "1" || get("ConfigDataInstall") == "1":
		return "security"
	case get("AutomaticDownload") == "1":
		return "download"
	case get("AutomaticCheckEnabled") == "1":
		return "notify"
	case get("AutomaticCheckEnabled") == "0":
		return "disabled"
	}
	return ""
}

// osDetailsOS liefert die Build-Nummer (sw_vers).
func osDetailsOS(ctx context.Context) (build, codename, edition string) {
	return strings.TrimSpace(run(ctx, "sw_vers", "-buildVersion")), "", ""
}

func kernelInstalled() string { return "" }
