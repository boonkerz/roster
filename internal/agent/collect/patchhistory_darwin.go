//go:build darwin

package collect

import (
	"context"
	"time"

	"github.com/boonkerz/roster/internal/shared"
)

// patchHistoryOS liest `softwareupdate --history` (installierte macOS-/Apple-Updates).
func patchHistoryOS(ctx context.Context, since time.Time) []shared.PatchEvent {
	return parseSoftwareUpdateHistory(run(ctx, "softwareupdate", "--history"), since)
}
