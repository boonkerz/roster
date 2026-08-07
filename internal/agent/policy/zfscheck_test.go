package policy

import (
	"testing"

	"github.com/boonkerz/roster/internal/shared"
)

func TestEvalZFS(t *testing.T) {
	const listing = "tank\tONLINE\t42%\nbackup\tDEGRADED\t91%\n"

	cases := []struct {
		name   string
		config map[string]any
		out    string
		want   string
	}{
		{"health: ein Pool DEGRADED -> failing", nil, listing, "failing"},
		{"health: nur gesunder Pool -> passing", map[string]any{"pool": "tank"}, listing, "passing"},
		{"capacity: über Grenze -> failing", map[string]any{"mode": "capacity", "max": 80, "pool": "backup"}, listing, "failing"},
		{"capacity: unter Grenze -> passing", map[string]any{"mode": "capacity", "max": 80, "pool": "tank"}, listing, "passing"},
		{"unbekannter Pool -> failing", map[string]any{"pool": "nope"}, listing, "failing"},
		{"keine Pools -> unknown", nil, "", "unknown"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := shared.CheckSpec{ID: "x", Type: "zfs", Config: tc.config}
			got := evalZFS(c, tc.out)
			if got.Status != tc.want {
				t.Errorf("Status = %q, erwartet %q (Output: %s)", got.Status, tc.want, got.Output)
			}
		})
	}
}
