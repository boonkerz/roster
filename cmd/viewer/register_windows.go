//go:build windows

package main

import (
	"fmt"
	"os/exec"
	"strings"
)

const schemeKey = `HKCU\Software\Classes\roster`

// registerScheme registriert das Binary unter exe als Handler für roster://-Links in
// der Windows-Registry (HKCU, kein Admin nötig), sodass der Browser-Button „Im Viewer
// öffnen" den Viewer direkt mit dem Startcode startet.
func registerScheme(exe string) error {
	cmds := [][]string{
		{"add", schemeKey, "/ve", "/d", "URL:Roster Fernsteuerung", "/f"},
		{"add", schemeKey, "/v", "URL Protocol", "/d", "", "/f"},
		{"add", schemeKey + `\shell\open\command`, "/ve", "/d", fmt.Sprintf(`"%s" "%%1"`, exe), "/f"},
	}
	for _, c := range cmds {
		if out, err := exec.Command("reg", c...).CombinedOutput(); err != nil {
			return fmt.Errorf("reg %v: %v: %s", c, err, out)
		}
	}
	return nil
}

// unregisterScheme entfernt den roster://-Handler aus der Registry.
func unregisterScheme() error {
	out, err := exec.Command("reg", "delete", schemeKey, "/f").CombinedOutput()
	if err != nil && !strings.Contains(string(out), "nicht") && !strings.Contains(string(out), "unable to find") {
		return fmt.Errorf("reg delete: %v: %s", err, out)
	}
	return nil
}
