//go:build !linux && !windows

package main

import "fmt"

// registerScheme ist auf anderen Plattformen (z. B. macOS) noch nicht implementiert.
func registerScheme() error {
	return fmt.Errorf("--register wird auf dieser Plattform nicht unterstützt")
}
