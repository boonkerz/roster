//go:build !windows && !linux

package remote

import "log/slog"

// newScreenSource: auf macOS/FreeBSD gibt es noch keine Bildschirmaufnahme – statt eines
// Testbilds erscheint ein Hinweis.
func newScreenSource(_ *slog.Logger, _ int) (screenSource, error) {
	return newInfoSource("Bildschirmaufnahme nicht verfügbar", "Fernsteuerung wird auf diesem System noch nicht unterstützt.", "Unterstützt: Windows sowie Linux mit X11 oder wlroots-Wayland."), nil
}

// RunCaptureHelper existiert nur unter Windows (Session-0-Umgehung); anderswo No-op.
func RunCaptureHelper(_ int) {}
