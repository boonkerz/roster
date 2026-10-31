package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/boonkerz/roster/internal/server/auth"
	"github.com/boonkerz/roster/internal/server/model"
	"github.com/boonkerz/roster/internal/server/store"
)

func TestDeviceEOLStore(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()
	d := &model.Device{ID: store.NewID(), Hostname: "u", OS: "ubuntu", OSVersion: "20.04"}
	if err := st.CreateDevice(ctx, d, auth.HashToken("u")); err != nil {
		t.Fatal(err)
	}
	if err := st.SetEOLCache(ctx, "ubuntu", `[{"name":"20.04"}]`); err != nil {
		t.Fatal(err)
	}
	if err := st.SetEOLCache(ctx, "ubuntu", `[{"name":"20.04"},{"name":"24.04"}]`); err != nil {
		t.Fatal(err)
	}
	data, at, err := st.EOLCache(ctx, "ubuntu")
	if err != nil || data != `[{"name":"20.04"},{"name":"24.04"}]` || at.IsZero() {
		t.Fatalf("cache: %v %q %v", err, data, at)
	}
	if data2, _, _ := st.EOLCache(ctx, "nope"); data2 != "" {
		t.Fatalf("fehlender cache muss leer sein")
	}
	eol := time.Date(2025, 5, 29, 0, 0, 0, 0, time.UTC)
	prev, err := st.UpsertDeviceEOL(ctx, model.DeviceEOL{DeviceID: d.ID, Kind: "os", Product: "ubuntu", Cycle: "20.04", Label: "20.04 (LTS)", EOLDate: &eol, Status: "eol"})
	if err != nil || prev != "" {
		t.Fatalf("upsert: %v %q", err, prev)
	}
	if err := st.MarkEOLAlerted(ctx, d.ID, "os", "eol"); err != nil {
		t.Fatal(err)
	}
	prev, err = st.UpsertDeviceEOL(ctx, model.DeviceEOL{DeviceID: d.ID, Kind: "os", Product: "ubuntu", Cycle: "20.04", EOLDate: &eol, Status: "eol"})
	if err != nil || prev != "eol" {
		t.Fatalf("zweites upsert: %v %q", err, prev)
	}
	dev, err := st.GetDevice(ctx, d.ID)
	if err != nil || dev.EOLStatus != "eol" || dev.EOLDate == nil || !dev.EOLDate.Equal(eol) {
		t.Fatalf("GetDevice: %v %q %v", err, dev.EOLStatus, dev.EOLDate)
	}
	fleet, err := st.EOLFleet(ctx, nil)
	if err != nil || len(fleet) != 1 || fleet[0].Hostname != "u" || fleet[0].Status != "eol" || fleet[0].Label != "" {
		t.Fatalf("fleet: %v %+v", err, fleet)
	}
	per, _ := st.DeviceEOLFor(ctx, d.ID)
	if len(per) != 1 || per[0].Kind != "os" {
		t.Fatalf("per device: %+v", per)
	}
	if rows, _ := st.EOLFleet(ctx, map[string]bool{}); len(rows) != 0 {
		t.Fatalf("leerer scope: %+v", rows)
	}
	if err := st.DeleteDeviceEOL(ctx, d.ID, "os"); err != nil {
		t.Fatal(err)
	}
	if dev, _ := st.GetDevice(ctx, d.ID); dev.EOLStatus != "" {
		t.Fatalf("nach delete: %q", dev.EOLStatus)
	}
}
