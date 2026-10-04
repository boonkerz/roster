package api

import (
	"testing"
	"time"

	"github.com/boonkerz/roster/internal/server/model"
)

func TestDecideDeferred(t *testing.T) {
	now := time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)
	d := model.DeferredAlert{CreatedAt: now.Add(-3 * time.Minute), NotBefore: now.Add(-1 * time.Minute), CommandID: "c1"}
	ranLongAgo := now.Add(-5 * time.Minute)
	ranJustNow := now.Add(-10 * time.Second)

	cases := []struct {
		name   string
		status string
		done   bool
		ranAt  *time.Time
		d      model.DeferredAlert
		want   deferredAction
	}{
		{"wieder gut → still verwerfen", "passing", true, &ranLongAgo, d, deferredDrop},
		{"skript läuft noch → warten", "failing", false, nil, d, deferredKeep},
		{"skript fertig, Check danach gelaufen → melden", "failing", true, &ranLongAgo, d, deferredFire},
		{"skript gerade erst fertig → Ergebnis ist noch das alte", "failing", true, &ranJustNow, d, deferredKeep},
		{"Warnung zählt wie Fehler", "warning", true, &ranLongAgo, d, deferredFire},
		{"unbekannt → warten", "unknown", true, &ranLongAgo, d, deferredKeep},
		{"Schonfrist noch nicht um → warten", "failing", true, &ranLongAgo,
			model.DeferredAlert{CreatedAt: now.Add(-time.Minute), NotBefore: now.Add(time.Minute)}, deferredKeep},
		{"Höchstdauer überschritten → melden, auch wenn Befehl hängt", "failing", false, nil,
			model.DeferredAlert{CreatedAt: now.Add(-3 * time.Hour), NotBefore: now.Add(time.Hour)}, deferredFire},
	}
	for _, c := range cases {
		if got := decideDeferred(c.d, c.status, c.done, c.ranAt, now); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}
