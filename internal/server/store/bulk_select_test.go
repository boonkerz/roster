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

// TestFailingChecksAndManagedIDs deckt die Geräteliste-Filterung nach fehlerhaftem
// Check und die scope-geprüfte Auflösung einer expliziten Geräteauswahl ab.
func TestFailingChecksAndManagedIDs(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()

	cl := &model.Client{ID: store.NewID(), Name: "Kunde"}
	if err := st.CreateClient(ctx, cl); err != nil {
		t.Fatal(err)
	}
	site := &model.Site{ID: store.NewID(), ClientID: cl.ID, Name: "HQ"}
	if err := st.CreateSite(ctx, site); err != nil {
		t.Fatal(err)
	}
	inSite := &model.Device{ID: store.NewID(), Hostname: "a", OS: "linux"}
	noSite := &model.Device{ID: store.NewID(), Hostname: "b", OS: "linux"}
	revoked := &model.Device{ID: store.NewID(), Hostname: "c", OS: "linux"}
	for _, d := range []*model.Device{inSite, noSite, revoked} {
		if err := st.CreateDevice(ctx, d, auth.HashToken(d.Hostname)); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.SetDeviceSite(ctx, inSite.ID, &site.ID); err != nil {
		t.Fatal(err)
	}
	if err := st.RevokeDevice(ctx, revoked.ID); err != nil {
		t.Fatal(err)
	}

	pol := &model.Policy{ID: store.NewID(), Name: "p"}
	if err := st.CreatePolicy(ctx, pol); err != nil {
		t.Fatal(err)
	}
	chk := &model.PolicyCheck{ID: store.NewID(), PolicyID: pol.ID, Name: "Neustart ausstehend", Type: "reboot", Severity: "warning"}
	if err := st.AddCheck(ctx, chk); err != nil {
		t.Fatal(err)
	}
	if _, err := st.SaveCheckResults(ctx, inSite.ID, []shared.CheckResult{{CheckID: chk.ID, Status: "failing"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.SaveCheckResults(ctx, noSite.ID, []shared.CheckResult{{CheckID: chk.ID, Status: "passing"}}); err != nil {
		t.Fatal(err)
	}

	failing, err := st.FailingChecksByDevice(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got := failing[inSite.ID]; len(got) != 1 || got[0].ID != chk.ID || got[0].Name != "Neustart ausstehend" || got[0].Type != "reboot" {
		t.Fatalf("FailingChecksByDevice[inSite] = %+v", got)
	}
	if _, ok := failing[noSite.ID]; ok {
		t.Fatal("passing-Check darf nicht als fehlerhaft gelistet werden")
	}

	all := []string{inSite.ID, noSite.ID, revoked.ID, "gibt-es-nicht"}
	ids, err := st.ManagedDeviceIDs(ctx, all, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 2 {
		t.Fatalf("unbeschränkt: %v, want inSite+noSite (widerrufen/unbekannt raus)", ids)
	}
	ids, err = st.ManagedDeviceIDs(ctx, all, map[string]bool{site.ID: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 1 || ids[0] != inSite.ID {
		t.Fatalf("mit Scope: %v, want nur inSite", ids)
	}
	if ids, _ := st.ManagedDeviceIDs(ctx, nil, nil); len(ids) != 0 {
		t.Fatalf("leere Auswahl: %v", ids)
	}
}

func TestDeferredAlerts(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()
	dev := &model.Device{ID: store.NewID(), Hostname: "a", OS: "linux"}
	if err := st.CreateDevice(ctx, dev, auth.HashToken("a")); err != nil {
		t.Fatal(err)
	}
	d := &model.DeferredAlert{DeviceID: dev.ID, CheckID: "chk", CheckName: "Cron", EventID: "ev1", CommandID: "cmd1", NotBefore: time.Now().Add(time.Minute)}
	if err := st.UpsertDeferredAlert(ctx, d); err != nil {
		t.Fatal(err)
	}
	// Zweiter Fehlschlag desselben Checks ersetzt die Zeile (eine je Gerät/Check).
	d2 := &model.DeferredAlert{DeviceID: dev.ID, CheckID: "chk", CheckName: "Cron", EventID: "ev2", NotBefore: time.Now().Add(time.Minute)}
	if err := st.UpsertDeferredAlert(ctx, d2); err != nil {
		t.Fatal(err)
	}
	got, err := st.DeferredAlertsForDevice(ctx, dev.ID)
	if err != nil || len(got) != 1 || got[0].EventID != "ev2" || got[0].CommandID != "" {
		t.Fatalf("DeferredAlertsForDevice = %+v, %v", got, err)
	}
	if ok, err := st.DeleteDeferredAlertFor(ctx, dev.ID, "chk"); err != nil || !ok {
		t.Fatalf("DeleteDeferredAlertFor: %v %v", ok, err)
	}
	if ok, _ := st.DeleteDeferredAlertFor(ctx, dev.ID, "chk"); ok {
		t.Fatal("zweites Löschen darf nichts finden")
	}
}
