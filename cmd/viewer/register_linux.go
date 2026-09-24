//go:build linux

package main

import (
	"fmt"
	"path/filepath"

	"github.com/boonkerz/roster/internal/clientinstall"
)

const viewerDesktopFile = "roster-viewer.desktop"

// registerScheme registriert das Binary unter exe als Handler für roster://-Links,
// sodass der Browser-Button „Im Viewer öffnen" den Viewer direkt mit dem Startcode
// startet. Schreibt einen .desktop-Eintrag ins Nutzerverzeichnis (kein root nötig).
func registerScheme(exe string) error {
	dir := clientinstall.ApplicationsDir("")
	desktop := fmt.Sprintf(`[Desktop Entry]
Type=Application
Name=Roster Fernsteuerung
Exec=%s %%u
Terminal=false
NoDisplay=true
MimeType=x-scheme-handler/roster;
`, exe)
	if err := clientinstall.WriteFile(filepath.Join(dir, viewerDesktopFile), []byte(desktop), 0o644); err != nil {
		return err
	}
	// Best-effort: MIME-Datenbank aktualisieren und als Default setzen.
	clientinstall.RefreshDesktop("")
	clientinstall.SetDefaultHandler(viewerDesktopFile, "roster")
	return nil
}

// unregisterScheme entfernt den roster://-Handler wieder.
func unregisterScheme() error {
	if err := clientinstall.RemoveIfExists(filepath.Join(clientinstall.ApplicationsDir(""), viewerDesktopFile)); err != nil {
		return err
	}
	clientinstall.RefreshDesktop("")
	return nil
}
