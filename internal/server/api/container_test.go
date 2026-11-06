package api

import (
	"strings"
	"testing"

	"github.com/boonkerz/roster/internal/server/model"
	"github.com/boonkerz/roster/internal/shared"
)

func TestDiffDockerDBs(t *testing.T) {
	old := []model.DockerDatabase{
		{Container: "shop_db", Engine: "mongo", Name: "shop", Collections: 12},
		{Container: "shop_db", Engine: "mongo", Name: "admin", Collections: 1},
		{Container: "shop_db", Engine: "mongo", Name: "local", Collections: 1},
		{Container: "stats_db", Engine: "mysql", Name: "stats", Collections: 5},
		{Container: "gone_db", Engine: "mysql", Name: "x", Collections: 2},
		{Container: "broken_db", Engine: "mysql", Error: "Zugriff verweigert"},
	}
	// Unverändert (nur System-DB config neu, gone_db gestoppt, broken_db weiter kaputt) -> nichts.
	cur := []shared.DockerDatabase{
		{Container: "shop_db", Engine: "mongo", Name: "shop", Collections: 12},
		{Container: "shop_db", Engine: "mongo", Name: "admin", Collections: 1},
		{Container: "shop_db", Engine: "mongo", Name: "local", Collections: 1},
		{Container: "shop_db", Engine: "mongo", Name: "config", Collections: 1},
		{Container: "stats_db", Engine: "mysql", Name: "stats", Collections: 5},
		{Container: "broken_db", Engine: "mysql", Error: "Zugriff verweigert"},
	}
	if d := diffDockerDBs(old, cur); len(d) != 0 {
		t.Fatalf("unverändert: %+v", d)
	}
	// Erpressungsmuster: shop weg, README_TO_RECOVER da; stats geleert.
	cur = []shared.DockerDatabase{
		{Container: "shop_db", Engine: "mongo", Name: "README_TO_RECOVER", Collections: 1},
		{Container: "shop_db", Engine: "mongo", Name: "admin", Collections: 1},
		{Container: "stats_db", Engine: "mysql", Name: "stats", Collections: 0},
	}
	d := diffDockerDBs(old, cur)
	if len(d) != 2 || d[0].Container != "shop_db" || !d[0].Critical || len(d[0].Added) != 1 || d[0].Added[0] != "README_TO_RECOVER" || len(d[0].Removed) != 1 || d[0].Removed[0] != "shop" {
		t.Fatalf("shop: %+v", d)
	}
	if d[1].Container != "stats_db" || !d[1].Critical || len(d[1].Emptied) != 1 || !strings.Contains(d[1].Emptied[0], "5 → 0") {
		t.Fatalf("stats: %+v", d)
	}
	// Nur eine neue, harmlos benannte Datenbank -> Befund, aber nicht kritisch.
	cur = []shared.DockerDatabase{
		{Container: "shop_db", Engine: "mongo", Name: "shop", Collections: 12},
		{Container: "shop_db", Engine: "mongo", Name: "shop_staging", Collections: 3},
		{Container: "stats_db", Engine: "mysql", Name: "stats", Collections: 5},
	}
	d = diffDockerDBs(old, cur)
	if len(d) != 1 || d[0].Critical || len(d[0].Added) != 1 {
		t.Fatalf("neu: %+v", d)
	}
	// Erster Stand (nichts Altes) -> nichts.
	if d := diffDockerDBs(nil, cur); len(d) != 0 {
		t.Fatalf("baseline: %+v", d)
	}
	// Agent liefert jetzt Fehler (DB nicht erreichbar) -> keine Löschmeldung.
	cur = []shared.DockerDatabase{{Container: "shop_db", Engine: "mongo", Error: "timeout"}}
	if d := diffDockerDBs(old, cur); len(d) != 0 {
		t.Fatalf("fehler: %+v", d)
	}
}
