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

// TestPendingPatchesFleet prüft Sicherheits-Zähler, flottenweite Liste, Filter und
// Kopfzahlen der Patch-Seite.
func TestPendingPatchesFleet(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()
	mk := func(host string) *model.Device {
		d := &model.Device{ID: store.NewID(), Hostname: host, OS: "ubuntu"}
		if err := st.CreateDevice(ctx, d, auth.HashToken(host)); err != nil {
			t.Fatalf("CreateDevice: %v", err)
		}
		return d
	}
	a, b, c := mk("alpha"), mk("beta"), mk("gamma")
	upd := func(items ...shared.UpdateItem) *shared.OSUpdateInfo {
		n := 0
		for _, it := range items {
			if it.Security {
				n++
			}
		}
		return &shared.OSUpdateInfo{Count: len(items), SecurityCount: n, CheckedAt: time.Now(), Items: items}
	}
	must := func(err error) {
		if err != nil {
			t.Fatal(err)
		}
	}
	must(st.UpdateInventory(ctx, a.ID, shared.Inventory{Hostname: "alpha", OSUpdates: upd(
		shared.UpdateItem{Name: "bash", Security: true, Severity: "Important", Version: "2", CurrentVersion: "1", Source: "jammy-security"},
		shared.UpdateItem{Name: "curl", Severity: "Other", Version: "8"},
	)}))
	must(st.UpdateInventory(ctx, b.ID, shared.Inventory{Hostname: "beta", OSUpdates: upd(
		shared.UpdateItem{Name: "vim", Severity: "Other"},
	)}))
	must(st.UpdateInventory(ctx, c.ID, shared.Inventory{Hostname: "gamma"})) // nie geprüft
	must(st.ApprovePatch(ctx, a.ID, "bash", true))

	da, err := st.GetDevice(ctx, a.ID)
	must(err)
	if da.SecurityUpdatesCount == nil || *da.SecurityUpdatesCount != 1 || da.UpdatesCount == nil || *da.UpdatesCount != 2 {
		t.Fatalf("Zähler alpha: %+v / %+v", da.UpdatesCount, da.SecurityUpdatesCount)
	}
	if len(da.AvailableUpdates) != 2 || !da.AvailableUpdates[0].Security || da.AvailableUpdates[0].Name != "bash" ||
		da.AvailableUpdates[0].CurrentVersion != "1" || da.AvailableUpdates[0].Source != "jammy-security" {
		t.Fatalf("AvailableUpdates: %+v", da.AvailableUpdates)
	}

	all, err := st.PendingPatches(ctx, store.PatchFilter{})
	must(err)
	if len(all) != 3 || all[0].Name != "bash" || !all[0].Security || !all[0].Approved || all[0].Hostname != "alpha" {
		t.Fatalf("PendingPatches: %+v", all)
	}
	sec, err := st.PendingPatches(ctx, store.PatchFilter{SecurityOnly: true})
	must(err)
	if len(sec) != 1 || sec[0].Name != "bash" {
		t.Fatalf("SecurityOnly: %+v", sec)
	}
	if rows, _ := st.PendingPatches(ctx, store.PatchFilter{Query: "VIM"}); len(rows) != 1 || rows[0].Hostname != "beta" {
		t.Fatalf("Query: %+v", rows)
	}
	if rows, _ := st.PendingPatches(ctx, store.PatchFilter{Allowed: map[string]bool{}}); len(rows) != 0 {
		t.Fatalf("leerer Scope muss leer sein: %+v", rows)
	}
	names, err := st.SecurityPatches(ctx, a.ID)
	must(err)
	if len(names) != 1 || names[0] != "bash" {
		t.Fatalf("SecurityPatches: %v", names)
	}

	sum, err := st.PatchSummary(ctx, nil)
	must(err)
	if sum.DevicesTotal != 3 || sum.DevicesChecked != 2 || sum.DevicesPending != 2 || sum.DevicesSecurity != 1 ||
		sum.Pending != 3 || sum.Security != 1 || sum.Approved != 1 || sum.CompliantPct != 50 {
		t.Fatalf("PatchSummary: %+v", sum)
	}
}
