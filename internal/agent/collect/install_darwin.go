//go:build darwin

package collect

import (
	"context"
	"os/exec"
)

// InstallUpdates installiert macOS-Updates (alle, oder die genannten Labels).
func InstallUpdates(ctx context.Context, names []string, _ bool, _ bool) (int, string) {
	args := []string{"-i", "-a"}
	if len(names) > 0 {
		args = append([]string{"-i"}, names...)
	}
	out, err := exec.CommandContext(ctx, "softwareupdate", args...).CombinedOutput()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return ee.ExitCode(), capOutput(string(out))
		}
		return -1, capOutput(string(out) + err.Error())
	}
	return 0, capOutput(string(out))
}
