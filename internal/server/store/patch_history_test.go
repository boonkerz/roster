package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/boonkerz/roster/internal/server/auth"
	"github.com/boonkerz/roster/internal/server/model"
	"github.com/boonkerz/roster/internal/server/store"
	"github.com/boonkerz/roster/internal/shared"
)

// TestPatchHistoryIdempotent: doppeltes Einfügen legt keine Duplikate an; Filter und
// Zähler für den Report funktionieren.
func TestPatchHistoryIdempotent(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()
	d := &model.Device{ID: store.NewID(), Hostname: "srv", OS: "debian"}
	if err := st.CreateDevice(ctx, d, auth.HashToken("x")); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	events := []shared.PatchEvent{
		{Time: now.Add(-time.Hour), Action: "upgrade", Name: "bash", OldVersion: "1", NewVersion: "2", Source: "apt", Detail: "unattended"},
		{Time: now.Add(-2 * time.Hour), Action: "install", Name: "htop", NewVersion: "3", Source: "apt"},
		{Time: now.Add(-10 * 24 * time.Hour), Action: "remove", Name: "nano", OldVersion: "6", Source: "apt"},
	}
	n, err := st.InsertPatchHistory(ctx, d.ID, events)
	if err != nil || n != 3 {
		t.Fatalf("insert: n=%d err=%v", n, err)
	}
	n, err = st.InsertPatchHistory(ctx, d.ID, events)
	if err != nil || n != 0 {
		t.Fatalf("zweites insert: n=%d err=%v", n, err)
	}
	all, err := st.PatchHistory(ctx, store.PatchHistoryFilter{Since: now.AddDate(0, 0, -90)})
	if err != nil || len(all) != 3 || all[0].Name != "bash" || all[0].Hostname != "srv" || all[0].OldVersion != "1" {
		t.Fatalf("history: %v %+v", err, all)
	}
	if rows, _ := st.PatchHistory(ctx, store.PatchHistoryFilter{Since: now.AddDate(0, 0, -7)}); len(rows) != 2 {
		t.Fatalf("since 7d: %d", len(rows))
	}
	if rows, _ := st.PatchHistory(ctx, store.PatchHistoryFilter{Action: "remove"}); len(rows) != 1 || rows[0].Name != "nano" {
		t.Fatalf("action: %+v", rows)
	}
	if rows, _ := st.PatchHistory(ctx, store.PatchHistoryFilter{Query: "HT"}); len(rows) != 1 {
		t.Fatalf("query: %+v", rows)
	}
	if rows, _ := st.PatchHistory(ctx, store.PatchHistoryFilter{Allowed: map[string]bool{}}); len(rows) != 0 {
		t.Fatalf("leerer scope: %+v", rows)
	}
	counts, err := st.PatchInstallCounts(ctx, now.AddDate(0, 0, -7))
	if err != nil || counts[d.ID] != 2 {
		t.Fatalf("counts: %v %v", counts, err)
	}
	if err := st.PrunePatchHistory(ctx, 5*24*time.Hour); err != nil {
		t.Fatal(err)
	}
	if rows, _ := st.PatchHistory(ctx, store.PatchHistoryFilter{}); len(rows) != 2 {
		t.Fatalf("prune: %d", len(rows))
	}
}
