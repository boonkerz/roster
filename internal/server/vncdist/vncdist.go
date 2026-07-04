// Package vncdist bettet die (optionalen) nativen VNC-Server-Bundles in den Server
// ein, damit der Agent sie on-demand herunterladen kann – ohne dass am Zielrechner
// etwas manuell installiert werden muss.
//
// Die Bundles (ZIP je Plattform) werden vor dem Server-Build in bin/ abgelegt
// (Make-Target `vnc-embed`); ohne diesen Schritt enthält das Verzeichnis nur
// .gitkeep und Downloads liefern 404 (der Agent fällt dann auf einen im PATH
// installierten VNC-Server zurück).
package vncdist

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"io/fs"
	"sort"
)

//go:embed all:bin
var binFS embed.FS

// platform ("<os>-<arch>") -> eingebetteter ZIP-Dateiname.
var files = map[string]string{
	"windows-amd64": "bin/vnc-windows-amd64.zip",
	"linux-amd64":   "bin/vnc-linux-amd64.zip",
	"linux-arm64":   "bin/vnc-linux-arm64.zip",
}

// Read liefert das VNC-Bundle einer Plattform samt SHA-256 (hex).
func Read(platform string) (data []byte, sha string, ok bool) {
	name, exists := files[platform]
	if !exists {
		return nil, "", false
	}
	b, err := binFS.ReadFile(name)
	if err != nil {
		return nil, "", false
	}
	sum := sha256.Sum256(b)
	return b, hex.EncodeToString(sum[:]), true
}

// Available listet die tatsächlich eingebetteten Plattformen, sortiert.
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
