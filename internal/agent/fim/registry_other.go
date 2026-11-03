//go:build !windows

package fim

import (
	"io/fs"

	"github.com/boonkerz/roster/internal/shared"
)

// regValue ist der vom Walker gelieferte Hash eines Registry-Werts (nur Windows).
type regValue struct{ hash string }

// walkRegistry gibt es nur unter Windows; andere Plattformen überspringen Registry-Regeln.
func walkRegistry(_ shared.FIMRule, _ func(r shared.FIMRule, p string, info fs.FileInfo) bool) {}
