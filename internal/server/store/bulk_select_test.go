package store_test

import (
	"context"
	"testing"

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
