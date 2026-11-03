//go:build darwin

package statemon

import (
	"context"
	"os/exec"
	"strings"
	"time"
)

func collectors() map[string]func(context.Context) []string {
	return map[string]func(context.Context) []string{
		"ports": collectPorts,
		"admins": func(ctx context.Context) []string {
			cctx, cancel := context.WithTimeout(ctx, 20*time.Second)
			defer cancel()
			out, _ := exec.CommandContext(cctx, "dscl", ".", "-read", "/Groups/admin", "GroupMembership").Output()
			_, members, _ := strings.Cut(string(out), ":")
			return strings.Fields(members)
		},
		"launchd": func(ctx context.Context) []string {
			cctx, cancel := context.WithTimeout(ctx, 20*time.Second)
			defer cancel()
			out, _ := exec.CommandContext(cctx, "launchctl", "list").Output()
			return ParseLaunchctl(string(out))
		},
	}
}
