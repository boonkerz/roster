package flow

import "github.com/boonkerz/roster/internal/shared/textdiff"

func unifiedDiff(name, a, b string) string { return textdiff.Unified(name, a, b) }
