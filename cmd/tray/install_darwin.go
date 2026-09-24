package main

import "fmt"

// installTray ist auf macOS noch nicht umgesetzt (dort wäre ein .app-Bundle nötig,
// damit Dock und Login-Objekte die App kennen).
func installTray(string, bool) error {
	return fmt.Errorf("--install wird auf macOS noch nicht unterstützt – Binary und libSDL3.dylib zusammen z. B. nach ~/Applications/Roster/ legen")
}

func uninstallTray(string) error {
	return fmt.Errorf("--uninstall wird auf macOS noch nicht unterstützt")
}
