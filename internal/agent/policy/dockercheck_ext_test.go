package policy

import (
	"strings"
	"testing"
	"time"

	"github.com/boonkerz/roster/internal/shared"
)

func TestEvalDatabases(t *testing.T) {
	dbs := []shared.DockerDatabase{
		{Container: "shop_db", Engine: "mongo", Name: "shop", Collections: 12},
		{Container: "shop_db", Engine: "mongo", Name: "admin", Collections: 1},
		{Container: "stats_db", Engine: "mysql", Error: "Zugriff verweigert"},
	}
	spec := func(cfg map[string]any) shared.CheckSpec { return shared.CheckSpec{ID: "c", Type: "docker", Config: cfg} }
	r := evalDatabases(spec(map[string]any{"mode": "databases"}), dbs)
	if r.Status != "passing" || !strings.Contains(r.Output, "2 Datenbank(en)") || !strings.Contains(r.Output, "stats_db") {
		t.Fatalf("ohne liste: %+v", r)
	}
	r = evalDatabases(spec(map[string]any{"mode": "databases", "expect": "shop, admin, local", "container": "shop"}), dbs)
	if r.Status != "failing" || !strings.Contains(r.Output, "fehlt: local") || strings.Contains(r.Output, "unerwartet") {
		t.Fatalf("fehlend: %+v", r)
	}
	dbs = append(dbs, shared.DockerDatabase{Container: "shop_db", Engine: "mongo", Name: "README", Collections: 1})
	r = evalDatabases(spec(map[string]any{"mode": "databases", "expect": "shop,admin"}), dbs)
	if r.Status != "failing" || !strings.Contains(r.Output, "unerwartet: shop_db/README") {
		t.Fatalf("unerwartet: %+v", r)
	}
	r = evalDatabases(spec(map[string]any{"mode": "databases", "container": "stats"}), dbs)
	if r.Status != "unknown" {
		t.Fatalf("nur fehler: %+v", r)
	}
	if r := evalDatabases(spec(map[string]any{"mode": "databases"}), nil); r.Status != "unknown" {
		t.Fatalf("keine container: %+v", r)
	}
}

func TestEvalImageAge(t *testing.T) {
	now := time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)
	images := []shared.DockerImage{
		{Repository: "atmoz/sftp", Tag: "latest", ImageID: "aaa111", CreatedAt: "2024-10-01 10:00:00 +0000 UTC"},
		{Repository: "nginx", Tag: "1.27-alpine", ImageID: "bbb222", CreatedAt: "2026-10-20 10:00:00 +0000 UTC"},
	}
	containers := []shared.DockerContainer{
		{Name: "sftp", Image: "atmoz/sftp", State: "running"},
		{Name: "web", Image: "nginx:1.27-alpine", State: "running"},
		{Name: "old_stopped", Image: "atmoz/sftp", State: "exited"},
	}
	spec := shared.CheckSpec{ID: "c", Type: "docker", Config: map[string]any{"mode": "image_age"}}
	r := evalImageAge(spec, containers, images, now)
	if r.Status != "failing" || !strings.Contains(r.Output, "sftp (atmoz/sftp") || strings.Contains(r.Output, "old_stopped") {
		t.Fatalf("365: %+v", r)
	}
	spec.Config["max_days"] = 1000.0
	if r := evalImageAge(spec, containers, images, now); r.Status != "passing" {
		t.Fatalf("1000: %+v", r)
	}
}
