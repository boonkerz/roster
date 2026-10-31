//go:build darwin

package collect

import (
	"context"

	"github.com/boonkerz/roster/internal/shared"
)

// osUpdateList wertet `softwareupdate -l` aus (Label, Version, Sicherheits-Kennung).
func osUpdateList(ctx context.Context) ([]shared.UpdateItem, bool) {
	out := run(ctx, "softwareupdate", "-l")
	if out == "" {
		return nil, true // kein Output -> i.d.R. keine Updates
	}
	return parseSoftwareUpdateList(out), true
}
