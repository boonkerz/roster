//go:build linux

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// registerScheme registriert pcinv-viewer als Handler für pcinv://-Links, sodass der
// Browser-Button „Im Viewer öffnen" den Viewer direkt mit dem Startcode startet.
// Schreibt einen .desktop-Eintrag ins Nutzerverzeichnis (kein root nötig).
func registerScheme() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	dir := filepath.Join(homeDir(), ".local", "share", "applications")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	desktop := fmt.Sprintf(`[Desktop Entry]
Type=Application
Name=PC-Inventory Fernsteuerung
Exec=%s %%u
Terminal=false
NoDisplay=true
MimeType=x-scheme-handler/pcinv;
`, exe)
	path := filepath.Join(dir, "pcinv-viewer.desktop")
	if err := os.WriteFile(path, []byte(desktop), 0o644); err != nil {
		return err
	}
	// Best-effort: MIME-Datenbank aktualisieren und als Default setzen.
	_ = exec.Command("update-desktop-database", dir).Run()
	_ = exec.Command("xdg-mime", "default", "pcinv-viewer.desktop", "x-scheme-handler/pcinv").Run()
	return nil
}

func homeDir() string {
	if h, err := os.UserHomeDir(); err == nil {
		return h
	}
	return os.Getenv("HOME")
}
