//go:build windows

package fim

import "io/fs"

// ownerOf: Besitzer-Ermittlung über ACLs ist teuer; unter Windows bleibt das Feld leer.
func ownerOf(_ fs.FileInfo, _ map[string]string) string { return "" }
