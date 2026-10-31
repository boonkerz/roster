//go:build !linux && !windows && !darwin

package collect

import (
	"context"

	"github.com/boonkerz/roster/internal/shared"
)

func reposOS(_ context.Context) []shared.PackageRepo                  { return nil }
func autoUpdatesOS(_ context.Context) string                          { return "" }
func osDetailsOS(_ context.Context) (build, codename, edition string) { return "", "", "" }
func kernelInstalled() string                                         { return "" }
