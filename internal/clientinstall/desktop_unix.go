//go:build !windows

package clientinstall

import (
	"os/exec"
	"path/filepath"
)

// RefreshDesktop aktualisiert die Desktop-Datenbank und den Symbol-Cache unter dem
// Präfix – best effort, die Werkzeuge fehlen auf manchen Systemen.
func RefreshDesktop(prefix string) {
	if prefix == "" {
		prefix = DefaultPrefix()
	}
	_ = exec.Command("update-desktop-database", ApplicationsDir(prefix)).Run()
	_ = exec.Command("gtk-update-icon-cache", "-f", "-t", filepath.Join(prefix, "share", "icons", "hicolor")).Run()
}

// SetDefaultHandler macht den .desktop-Eintrag zum Standard für ein URL-Schema.
func SetDefaultHandler(desktopFile, scheme string) {
	_ = exec.Command("xdg-mime", "default", desktopFile, "x-scheme-handler/"+scheme).Run()
}
