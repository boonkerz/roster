package clientinstall

import (
	"os"
	"path/filepath"
)

// DefaultPrefix ist das Installationsverzeichnis ohne Angabe: %LOCALAPPDATA%\Programs\Roster
// (pro Nutzer, kein Admin nötig). Viewer und Taskleisten-App teilen sich dort SDL3.dll.
func DefaultPrefix() string {
	base := os.Getenv("LOCALAPPDATA")
	if base == "" {
		base = filepath.Join(HomeDir(), "AppData", "Local")
	}
	return filepath.Join(base, "Programs", "Roster")
}

// BinDir liefert das Binary-Verzeichnis: unter Windows das Präfix selbst.
func BinDir(prefix string) string {
	if prefix == "" {
		return DefaultPrefix()
	}
	return prefix
}

func roamingDir() string {
	if d := os.Getenv("APPDATA"); d != "" {
		return d
	}
	return filepath.Join(HomeDir(), "AppData", "Roaming")
}

// StartMenuDir ist der Programme-Ordner des Startmenüs des Nutzers.
func StartMenuDir() string {
	return filepath.Join(roamingDir(), "Microsoft", "Windows", "Start Menu", "Programs")
}

// StartupDir ist der Autostart-Ordner des Nutzers.
func StartupDir() string { return filepath.Join(StartMenuDir(), "Startup") }
