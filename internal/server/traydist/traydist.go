// Package traydist bettet die Taskleisten-App (roster-tray) in den Server ein, damit
// Operatoren sie in den Einstellungen herunterladen können – wie den Viewer, ohne
// selbst bauen zu müssen (cgo-frei, SDL3 wird zur Laufzeit geladen).
//
// Die Pakete werden vor dem Server-Build erzeugt (Makefile-Targets `tray-embed*`).
// Ohne diesen Schritt enthält das Verzeichnis nur .gitkeep; Downloads liefern dann
// 404 und Available() ist leer.
package traydist

import (
	"embed"

	"github.com/boonkerz/roster/internal/server/clientdist"
)

//go:embed all:bin
var binFS embed.FS

var set = clientdist.Set{
	FS: binFS,
	// platform-Schlüssel ("<os>-<arch>") -> eingebetteter Dateiname.
	Files: map[string]string{
		"linux-amd64":   "bin/roster-tray-linux-amd64",
		"linux-arm64":   "bin/roster-tray-linux-arm64",
		"windows-amd64": "bin/roster-tray-windows-amd64.zip", // .exe + SDL3.dll
		"darwin-arm64":  "bin/roster-tray-darwin-arm64.zip",  // Binary + libSDL3.dylib
	},
	// Dateiname, unter dem der Client speichert.
	Names: map[string]string{
		"linux-amd64":   "roster-tray",
		"linux-arm64":   "roster-tray",
		"windows-amd64": "roster-tray-windows.zip",
		"darwin-arm64":  "roster-tray-macos.zip",
	},
}

// Entry beschreibt ein eingebettetes Tray-Paket für die Download-Übersicht.
type Entry = clientdist.Entry

// Read liefert das Paket einer Plattform sowie den vorgeschlagenen Dateinamen.
func Read(platform string) (data []byte, filename string, ok bool) { return set.Read(platform) }

// List liefert die eingebetteten Pakete mit Dateiname und Größe, nach Plattform sortiert.
func List() []Entry { return set.List() }

// Available listet die tatsächlich eingebetteten (gebauten) Plattformen, sortiert.
func Available() []string { return set.Available() }
