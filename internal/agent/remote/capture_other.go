//go:build !windows

package remote

import "log/slog"

// newScreenSource: echte Bildschirmaufnahme unter Linux/macOS folgt; vorerst ein
// Testbild (Übertragung/Tunnel sind damit verifizierbar).
func newScreenSource(_ *slog.Logger) (screenSource, error) {
	return newSyntheticSource(), nil
}

// RunCaptureHelper existiert nur unter Windows (Session-0-Umgehung); anderswo No-op.
func RunCaptureHelper() {}
