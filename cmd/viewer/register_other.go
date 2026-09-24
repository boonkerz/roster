//go:build !linux && !windows

package main

import "fmt"

// registerScheme ist auf anderen Plattformen (z. B. macOS) noch nicht implementiert.
func registerScheme(string) error {
	return fmt.Errorf("--register/--install wird auf dieser Plattform nicht unterstützt")
}

func unregisterScheme() error {
	return fmt.Errorf("--uninstall wird auf dieser Plattform nicht unterstützt")
}
