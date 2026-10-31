//go:build !linux && !windows && !darwin

package collect

import (
	"context"
	"time"

	"github.com/boonkerz/roster/internal/shared"
)

func patchHistoryOS(_ context.Context, _ time.Time) []shared.PatchEvent { return nil }
