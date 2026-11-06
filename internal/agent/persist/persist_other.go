//go:build !freebsd

package persist

import (
	"context"
	"log/slog"
)

// Ensure ist außerhalb von FreeBSD/OPNsense ohne Funktion (Dienste über kardianos/service).
func Ensure(_ context.Context, _ *slog.Logger, _ string) {}
