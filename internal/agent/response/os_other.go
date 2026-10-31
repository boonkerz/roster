//go:build !linux && !windows && !darwin

package response

import (
	"context"
	"fmt"
	"net"
)

func blockOS(_ context.Context, _ net.IP, _ int) (string, error) {
	return "none", fmt.Errorf("Active Response auf dieser Plattform nicht unterstützt")
}
func unblockOS(_ context.Context, _ net.IP) error { return nil }
func lockUserOS(_ context.Context, _ string, _ bool) error {
	return fmt.Errorf("Active Response auf dieser Plattform nicht unterstützt")
}
