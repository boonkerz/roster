//go:build !windows && !darwin

package main

import (
	"fmt"
	"log"
	"path/filepath"

	"github.com/boonkerz/roster/internal/clientinstall"
)

// desktopEntry ist die Vorlage für den Anwendungsmenü-Eintrag. Der Dateiname und
// StartupWMClass müssen zur appID passen (SDL_APP_ID), sonst ordnet die Leiste dem
// Fenster kein Symbol zu. Der Installer trägt den absoluten Exec-Pfad ein.
const desktopEntry = `[Desktop Entry]
Type=Application
Version=1.5
Name=%s
GenericName=Server management
GenericName[de]=Serververwaltung
Comment=Server list with check and task status, remote terminal and remote control
Comment[de]=Serverliste mit Check- und Task-Status, Remote-Terminal und Fernsteuerung
Exec=%s
Icon=%s
Terminal=false
Categories=Network;RemoteAccess;Monitor;
Keywords=roster;rmm;server;inventory;monitoring;remote;terminal;
Keywords[de]=roster;server;inventar;überwachung;fernwartung;terminal;
StartupWMClass=%s
SingleMainWindow=true
`

func desktopPath(prefix string) string {
	return filepath.Join(clientinstall.ApplicationsDir(prefix), appID+".desktop")
}

func autostartPath() string {
	return filepath.Join(clientinstall.AutostartDir(), appID+".desktop")
}

// installTray kopiert das Binary nach <prefix>/bin (Standard ~/.local), legt Symbol
// und Anwendungsmenü-Eintrag an und optional den Autostart-Eintrag (startet mit
// --hidden nur ins Tray). Entspricht `make install-tray`, nur ohne Quellbaum.
func installTray(prefix string, autostart bool) error {
	dst, err := clientinstall.InstallExecutable(clientinstall.BinDir(prefix), "roster-tray")
	if err != nil {
		return err
	}
	for _, p := range clientinstall.IconPaths(prefix, appID) {
		if err := clientinstall.WriteFile(p, nil, 0o644); err != nil {
			return err
		}
		if err := writeIconPNG(p, 256); err != nil {
			return fmt.Errorf("symbol %s: %w", p, err)
		}
	}
	entry := fmt.Sprintf(desktopEntry, "Roster", dst, appID, appID)
	if err := clientinstall.WriteFile(desktopPath(prefix), []byte(entry), 0o644); err != nil {
		return err
	}
	log.Printf("installiert: %s + Anwendungsmenü-Eintrag (%s)", dst, appID)
	if autostart {
		entry := fmt.Sprintf(desktopEntry, "Roster (Taskleiste)", dst+" --hidden", appID, appID)
		if err := clientinstall.WriteFile(autostartPath(), []byte(entry), 0o644); err != nil {
			return err
		}
		log.Printf("Autostart eingerichtet: %s", autostartPath())
	}
	clientinstall.RefreshDesktop(prefix)
	if h := clientinstall.PathHint(clientinstall.BinDir(prefix)); h != "" {
		log.Println(h)
	}
	return nil
}

// uninstallTray entfernt Binary, Menü-Eintrag, Symbole und Autostart wieder.
func uninstallTray(prefix string) error {
	paths := append([]string{desktopPath(prefix), autostartPath()}, clientinstall.IconPaths(prefix, appID)...)
	for _, p := range paths {
		if err := clientinstall.RemoveIfExists(p); err != nil {
			return err
		}
	}
	if err := clientinstall.RemoveExecutable(clientinstall.BinDir(prefix), "roster-tray"); err != nil {
		return err
	}
	clientinstall.RefreshDesktop(prefix)
	log.Println("entfernt: roster-tray, Anwendungsmenü-Eintrag, Symbol und Autostart")
	return nil
}
