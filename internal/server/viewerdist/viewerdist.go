// Package viewerdist bettet das native Fernsteuerungs-Viewer-Binary (roster-viewer)
// in den Server ein, damit Operatoren es direkt vom Inventory-Server herunterladen
// können – ohne selbst bauen zu müssen (cgo-frei, SDL3 wird zur Laufzeit geladen).
//
// Die Pakete werden vor dem Server-Build erzeugt (Makefile-Targets `viewer-embed*`).
// Ohne diesen Schritt enthält das Verzeichnis nur .gitkeep; Downloads liefern dann
// 404 und Available() ist leer.
package viewerdist

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
		"linux-amd64":   "bin/roster-viewer-linux-amd64",
		"linux-arm64":   "bin/roster-viewer-linux-arm64",
		"windows-amd64": "bin/roster-viewer-windows-amd64.zip", // .exe + SDL3.dll
		"darwin-arm64":  "bin/roster-viewer-darwin-arm64.zip",  // Binary + libSDL3.dylib
	},
	// Dateiname, unter dem der Client speichert.
	Names: map[string]string{
		"linux-amd64":   "roster-viewer",
		"linux-arm64":   "roster-viewer",
		"windows-amd64": "roster-viewer-windows.zip",
		"darwin-arm64":  "roster-viewer-macos.zip",
	},
}

// Entry beschreibt ein eingebettetes Viewer-Paket für die Download-Übersicht.
type Entry = clientdist.Entry

// Read liefert das Binary einer Plattform sowie den vorgeschlagenen Dateinamen.
func Read(platform string) (data []byte, filename string, ok bool) { return set.Read(platform) }

// List liefert die eingebetteten Pakete mit Dateiname und Größe, nach Plattform sortiert.
func List() []Entry { return set.List() }

// Available listet die tatsächlich eingebetteten (gebauten) Plattformen, sortiert.
func Available() []string { return set.Available() }
