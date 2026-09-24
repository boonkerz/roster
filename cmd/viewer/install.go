package main

import (
	"fmt"
	"log"
	"os"

	"github.com/boonkerz/roster/internal/clientinstall"
)

// installViewer kopiert den Viewer ins Programmverzeichnis (Linux: <prefix>/bin,
// Standard ~/.local/bin; Windows: %LOCALAPPDATA%\Programs\Roster samt SDL3.dll) und
// registriert die Kopie als roster://-Handler – ein Schritt statt „verschieben,
// chmod, --register".
func installViewer(prefix string) error {
	dir := clientinstall.BinDir(prefix)
	dst, err := clientinstall.InstallExecutable(dir, clientinstall.ExeName("roster-viewer"))
	if err != nil {
		return err
	}
	if err := registerScheme(dst); err != nil {
		return fmt.Errorf("register: %w", err)
	}
	log.Printf("installiert: %s", dst)
	log.Println("roster://-Handler registriert – der Browser-Button „Im Viewer öffnen\" funktioniert jetzt.")
	if h := clientinstall.PathHint(dir); h != "" {
		log.Println(h)
	}
	return nil
}

// uninstallViewer entfernt Handler-Registrierung und Binary wieder.
func uninstallViewer(prefix string) error {
	if err := unregisterScheme(); err != nil {
		return err
	}
	dir := clientinstall.BinDir(prefix)
	if err := clientinstall.RemoveExecutable(dir, clientinstall.ExeName("roster-viewer")); err != nil {
		return err
	}
	log.Printf("entfernt: roster://-Handler und %s", dir)
	return nil
}

// extractPrefix zieht `--prefix <dir>` bzw. `--prefix=<dir>` aus den Argumenten.
func extractPrefix(args []string) (prefix string, rest []string) {
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "--prefix" || args[i] == "-prefix":
			if i+1 < len(args) {
				prefix = args[i+1]
				i++
			}
		case len(args[i]) > 9 && args[i][:9] == "--prefix=":
			prefix = args[i][9:]
		default:
			rest = append(rest, args[i])
		}
	}
	return prefix, rest
}

// selfPath liefert den Pfad des laufenden Binaries für --register.
func selfPath() string {
	if p, err := clientinstall.Self(); err == nil {
		return p
	}
	p, _ := os.Executable()
	return p
}
