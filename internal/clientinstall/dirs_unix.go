//go:build !windows

package clientinstall

import "path/filepath"

// DefaultPrefix ist das Installationspräfix ohne Angabe: ~/.local (Binaries in
// ~/.local/bin, Desktop-Einträge und Symbole unter ~/.local/share).
func DefaultPrefix() string { return filepath.Join(HomeDir(), ".local") }

// BinDir liefert das Binary-Verzeichnis zum Präfix.
func BinDir(prefix string) string {
	if prefix == "" {
		prefix = DefaultPrefix()
	}
	return filepath.Join(prefix, "bin")
}

// ApplicationsDir liefert das Verzeichnis für .desktop-Einträge zum Präfix.
func ApplicationsDir(prefix string) string {
	if prefix == "" {
		prefix = DefaultPrefix()
	}
	return filepath.Join(prefix, "share", "applications")
}

// IconPaths liefert die Ablageorte für ein 256×256-Anwendungssymbol (hicolor + pixmaps).
func IconPaths(prefix, appID string) []string {
	if prefix == "" {
		prefix = DefaultPrefix()
	}
	return []string{
		filepath.Join(prefix, "share", "icons", "hicolor", "256x256", "apps", appID+".png"),
		filepath.Join(prefix, "share", "pixmaps", appID+".png"),
	}
}

// AutostartDir ist das XDG-Autostart-Verzeichnis des Nutzers.
func AutostartDir() string { return filepath.Join(HomeDir(), ".config", "autostart") }
