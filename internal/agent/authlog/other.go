//go:build !linux && !windows && !darwin

package authlog

import (
	"context"

	"github.com/boonkerz/roster/internal/shared"
)

func Collect(_ context.Context, _ string) []shared.AuthEvent { return nil }
