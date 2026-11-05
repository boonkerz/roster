//go:build !linux && !windows && !darwin

package collect

import "context"

func InstallUpdates(_ context.Context, _ []string, _ bool, _ bool) (int, string) {
	return -1, "Update-Installation auf diesem OS nicht unterstützt"
}
