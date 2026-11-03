//go:build !linux && !windows && !darwin

package statemon

import "context"

func collectors() map[string]func(context.Context) []string {
	return map[string]func(context.Context) []string{"ports": collectPorts}
}
