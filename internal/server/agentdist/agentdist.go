// Package agentdist bettet die cross-kompilierten Agent-Binaries in den Server ein,
// damit Clients den passenden Agent direkt vom Inventory-Server herunterladen können.
//
// Die Binaries werden vor dem Server-Build erzeugt (siehe Makefile-Target `agents-embed`
// bzw. der Server-Dockerfile). Ohne diesen Schritt enthält das Verzeichnis nur .gitkeep;
// Downloads liefern dann 404 und Available() ist leer.
package agentdist

import (
	"embed"
	"io/fs"
	"sort"
)

//go:embed all:bin
var binFS embed.FS

// platform-Schlüssel ("<os>-<arch>") -> eingebetteter Dateiname.
var files = map[string]string{
	"linux-amd64":   "bin/agent-linux-amd64",
	"linux-arm64":   "bin/agent-linux-arm64",
	"darwin-amd64":  "bin/agent-darwin-amd64",
	"darwin-arm64":  "bin/agent-darwin-arm64",
	"windows-amd64": "bin/agent-windows-amd64.exe",
	"freebsd-amd64": "bin/agent-freebsd-amd64", // OPNsense/pfSense & FreeBSD
}

// downloadName ist der Dateiname, unter dem der Client speichert.
var downloadName = map[string]string{
	"linux-amd64":   "roster-agent",
	"linux-arm64":   "roster-agent",
	"darwin-amd64":  "roster-agent",
	"darwin-arm64":  "roster-agent",
	"windows-amd64": "agent.exe",
	"freebsd-amd64": "roster-agent",
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
