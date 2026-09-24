// Package clientdist ist der gemeinsame Unterbau für die im Server eingebetteten
// Client-Programme (Fernsteuerungs-Viewer, Taskleisten-App): ein Set bildet
// Plattform-Schlüssel ("<os>-<arch>") auf Dateien in einem eingebetteten FS ab und
// liefert Download-Daten sowie die Übersicht für die Einstellungen-Seite.
//
// Die Binaries werden vor dem Server-Build erzeugt (Makefile-Targets `*-embed`).
// Fehlt ein Build-Schritt, ist die betreffende Plattform einfach nicht in
// Available()/List() und der Download liefert 404.
package clientdist

import (
	"io/fs"
	"sort"
)

// Set beschreibt die eingebetteten Pakete eines Client-Programms.
type Set struct {
	FS fs.FS
	// Files: Plattform-Schlüssel -> Pfad im FS.
	Files map[string]string
	// Names: Plattform-Schlüssel -> Dateiname, unter dem der Browser speichert.
	Names map[string]string
}

// Entry beschreibt ein eingebettetes Paket für die Download-Übersicht.
type Entry struct {
	Platform string `json:"platform"` // <os>-<arch>
	Filename string `json:"filename"` // Dateiname beim Herunterladen
	Size     int64  `json:"size"`     // Bytes
}

// Read liefert das Paket einer Plattform sowie den vorgeschlagenen Dateinamen.
func (s Set) Read(platform string) (data []byte, filename string, ok bool) {
	name, exists := s.Files[platform]
	if !exists {
		return nil, "", false
	}
	b, err := fs.ReadFile(s.FS, name)
	if err != nil {
		return nil, "", false
	}
	return b, s.Names[platform], true
}

// Available listet die tatsächlich eingebetteten (gebauten) Plattformen, sortiert.
func (s Set) Available() []string {
	var out []string
	for key, name := range s.Files {
		if _, err := fs.Stat(s.FS, name); err == nil {
			out = append(out, key)
		}
	}
	sort.Strings(out)
	return out
}

// List liefert die eingebetteten Pakete mit Dateiname und Größe, nach Plattform
// sortiert. Nie nil, damit die JSON-Antwort ein leeres Array statt null enthält.
func (s Set) List() []Entry {
	out := []Entry{}
	for _, p := range s.Available() {
		info, err := fs.Stat(s.FS, s.Files[p])
		if err != nil {
			continue
		}
		out = append(out, Entry{Platform: p, Filename: s.Names[p], Size: info.Size()})
	}
	return out
}
