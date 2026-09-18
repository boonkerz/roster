// Package viewerdist bettet das native Fernsteuerungs-Viewer-Binary (roster-viewer)
// in den Server ein, damit Operatoren es direkt vom Inventory-Server herunterladen
// können – ohne selbst SDL2/cgo bauen zu müssen.
//
// Das Binary wird vor dem Server-Build erzeugt (Makefile-Target `viewer-embed`).
// Ohne diesen Schritt enthält das Verzeichnis nur .gitkeep; Downloads liefern dann
// 404 und Available() ist leer. Der Viewer ist cgo/SDL2 und nur für Linux-Operator
// gedacht (Wayland/niri-Tastaturerfassung).
package viewerdist

import (
	"embed"
	"io/fs"
	"sort"
)

//go:embed all:bin
var binFS embed.FS

// platform-Schlüssel ("<os>-<arch>") -> eingebetteter Dateiname.
var files = map[string]string{
	"linux-amd64":   "bin/roster-viewer-linux-amd64",
	"linux-arm64":   "bin/roster-viewer-linux-arm64",
	"windows-amd64": "bin/roster-viewer-windows-amd64.zip", // .exe + SDL3.dll
	"darwin-arm64":  "bin/roster-viewer-darwin-arm64.zip",  // Binary + libSDL3.dylib
}

// downloadName ist der Dateiname, unter dem der Client speichert.
var downloadName = map[string]string{
	"linux-amd64":   "roster-viewer",
	"linux-arm64":   "roster-viewer",
	"windows-amd64": "roster-viewer-windows.zip",
	"darwin-arm64":  "roster-viewer-macos.zip",
}

// Read liefert das Binary einer Plattform sowie den vorgeschlagenen Dateinamen.
func Read(platform string) (data []byte, filename string, ok bool) {
	name, exists := files[platform]
	if !exists {
		return nil, "", false
	}
	b, err := binFS.ReadFile(name)
	if err != nil {
		return nil, "", false
	}
	return b, downloadName[platform], true
}

// Entry beschreibt ein eingebettetes Viewer-Paket für die Download-Übersicht.
type Entry struct {
	Platform string `json:"platform"` // <os>-<arch>
	Filename string `json:"filename"` // Dateiname beim Herunterladen
	Size     int64  `json:"size"`     // Bytes
}

// List liefert die eingebetteten Pakete mit Dateiname und Größe, nach Plattform sortiert.
func List() []Entry {
	var out []Entry
	for _, p := range Available() {
		info, err := fs.Stat(binFS, files[p])
		if err != nil {
			continue
		}
		out = append(out, Entry{Platform: p, Filename: downloadName[p], Size: info.Size()})
	}
	return out
}

// Available listet die tatsächlich eingebetteten (gebauten) Plattformen, sortiert.
func Available() []string {
	var out []string
	for key, name := range files {
		if _, err := fs.Stat(binFS, name); err == nil {
			out = append(out, key)
		}
	}
	sort.Strings(out)
	return out
}
