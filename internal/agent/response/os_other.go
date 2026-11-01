//go:build !linux && !windows && !darwin

package response

import (
	"context"
	"fmt"
)

func blockOS(_ context.Context, _ Target, _ int) (string, error) {
	return "none", fmt.Errorf("Active Response auf dieser Plattform nicht unterstützt")
}
func unblockOS(_ context.Context, _ Target) error { return nil }
func lockUserOS(_ context.Context, _ string, _ bool) error {
	return fmt.Errorf("Active Response auf dieser Plattform nicht unterstützt")
}
