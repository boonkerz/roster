package main

import (
	"fmt"
	"log"
	"path/filepath"

	"github.com/boonkerz/roster/internal/clientinstall"
)

// installTray kopiert roster-tray.exe samt SDL3.dll nach %LOCALAPPDATA%\Programs\Roster
// (oder --prefix), schreibt das Symbol als .ico und legt eine Startmenü-Verknüpfung
// an – optional zusätzlich im Autostart-Ordner (mit --hidden nur ins Tray).
func installTray(prefix string, autostart bool) error {
	dir := clientinstall.BinDir(prefix)
	dst, err := clientinstall.InstallExecutable(dir, "roster-tray.exe")
	if err != nil {
		return err
	}
	ico := filepath.Join(dir, "roster-tray.ico")
	if err := writeIconICO(ico, 256); err != nil {
		return fmt.Errorf("symbol: %w", err)
	}
	if err := clientinstall.CreateShortcut(clientinstall.Shortcut{
		Path: filepath.Join(clientinstall.StartMenuDir(), "Roster.lnk"), Target: dst, Icon: ico,
		Description: "Roster – Serverliste mit Check- und Task-Status",
	}); err != nil {
		return err
	}
	log.Printf("installiert: %s + Startmenü-Eintrag „Roster“", dst)
	if autostart {
		if err := clientinstall.CreateShortcut(clientinstall.Shortcut{
			Path: filepath.Join(clientinstall.StartupDir(), "Roster.lnk"), Target: dst, Args: "--hidden", Icon: ico,
			Description: "Roster (Taskleiste)",
		}); err != nil {
			return err
		}
		log.Println("Autostart eingerichtet (Startmenü → Autostart)")
	}
	return nil
}

// uninstallTray entfernt Verknüpfungen, Symbol und Binary wieder.
func uninstallTray(prefix string) error {
	dir := clientinstall.BinDir(prefix)
	for _, p := range []string{
		filepath.Join(clientinstall.StartMenuDir(), "Roster.lnk"),
		filepath.Join(clientinstall.StartupDir(), "Roster.lnk"),
		filepath.Join(dir, "roster-tray.ico"),
	} {
		if err := clientinstall.RemoveIfExists(p); err != nil {
			return err
		}
	}
	if err := clientinstall.RemoveExecutable(dir, "roster-tray.exe"); err != nil {
		return err
	}
	log.Println("entfernt: roster-tray, Startmenü-Eintrag und Autostart (eine laufende Kopie bleibt bis zum Beenden als .old liegen)")
	return nil
}
