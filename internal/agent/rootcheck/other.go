//go:build !linux

package rootcheck

import "context"

// Rootkit-Prüfungen gibt es nur unter Linux; Windows/macOS bringen Defender/XProtect mit.
func run(_ context.Context) []Finding { return nil }
