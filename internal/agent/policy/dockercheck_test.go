package policy

import (
	"testing"

	"github.com/boonkerz/roster/internal/shared"
)

func TestEvalDockerMode(t *testing.T) {
	cs := []shared.DockerContainer{
		{Name: "web", State: "running", Health: "healthy"},
		{Name: "shop-db-1", State: "running", Health: "unhealthy"},
		{Name: "old", State: "exited"},
	}
	spec := func(cfg map[string]any) shared.CheckSpec {
		return shared.CheckSpec{ID: "1", Type: "docker", Config: cfg}
	}

	cases := []struct {
		name string
		cfg  map[string]any
		want string
	}{
		{"running default min1 -> passing (2 laufen)", map[string]any{"mode": "running"}, "passing"},
		{"running min3 -> failing (nur 2)", map[string]any{"mode": "running", "min": 3.0}, "failing"},
		{"stopped max0 -> failing (1 gestoppt)", map[string]any{"mode": "stopped"}, "failing"},
		{"stopped max1 -> passing", map[string]any{"mode": "stopped", "max": 1.0}, "passing"},
		{"container web laufend -> passing", map[string]any{"mode": "container", "name": "web"}, "passing"},
		{"container old gestoppt -> failing", map[string]any{"mode": "container", "name": "old"}, "failing"},
		{"container fehlt -> failing", map[string]any{"mode": "container", "name": "gibtsnicht"}, "failing"},
		{"container substring (compose) -> passing", map[string]any{"mode": "container", "name": "shop-db"}, "passing"},
		{"unhealthy -> failing (1)", map[string]any{"mode": "unhealthy"}, "failing"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if r := evalDockerMode(spec(tc.cfg), cs); r.Status != tc.want {
				t.Fatalf("Status=%q, erwartet %q (Output: %s)", r.Status, tc.want, r.Output)
			}
		})
	}

	// unhealthy ohne kranke Container -> passing
	healthy := []shared.DockerContainer{{Name: "a", State: "running", Health: "healthy"}}
	if r := evalDockerMode(spec(map[string]any{"mode": "unhealthy"}), healthy); r.Status != "passing" {
		t.Fatalf("unhealthy healthy-only sollte passing sein: %+v", r)
	}
	// unbekannter Modus -> unknown
	if r := evalDockerMode(spec(map[string]any{"mode": "quatsch"}), cs); r.Status != "unknown" {
		t.Fatalf("unbekannter Modus sollte unknown sein: %+v", r)
	}
}
