//go:build !linux && !windows

package fim

import (
	"context"
	"errors"

	"github.com/boonkerz/roster/internal/shared"
)

// Who-Data gibt es auf dieser Plattform nicht (macOS/FreeBSD: kein auditd-Äquivalent eingebunden).
func EnsureAuditRules(_ context.Context, _ []shared.FIMRule) error {
	return errors.New("Who-Data wird auf dieser Plattform nicht unterstützt")
}

func CollectWho(_ context.Context, _ string) map[string]Who { return nil }

const whoFold = false
