//go:build windows

package main

import (
	"fmt"
	"os"
	"os/exec"
)

// registerScheme registriert pcinv-viewer als Handler für pcinv://-Links in der
// Windows-Registry (HKCU, kein Admin nötig), sodass der Browser-Button „Im Viewer
// öffnen" den Viewer direkt mit dem Startcode startet.
func registerScheme() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	const root = `HKCU\Software\Classes\pcinv`
	cmds := [][]string{
		{"add", root, "/ve", "/d", "URL:PC-Inventory Fernsteuerung", "/f"},
		{"add", root, "/v", "URL Protocol", "/d", "", "/f"},
		{"add", root + `\shell\open\command`, "/ve", "/d", fmt.Sprintf(`"%s" "%%1"`, exe), "/f"},
	}
	for _, c := range cmds {
		if out, err := exec.Command("reg", c...).CombinedOutput(); err != nil {
			return fmt.Errorf("reg %v: %v: %s", c, err, out)
		}
	}
	return nil
}
