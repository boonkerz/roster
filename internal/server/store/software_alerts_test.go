package store_test

import (
	"context"
	"testing"

	"github.com/boonkerz/roster/internal/server/auth"
	"github.com/boonkerz/roster/internal/server/model"
	"github.com/boonkerz/roster/internal/server/store"
)

func TestResolveSoftwareAlertsPrecedence(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()
	cl := &model.Client{ID: store.NewID(), Name: "Acme"}
	st.CreateClient(ctx, cl)
	site := &model.Site{ID: store.NewID(), ClientID: cl.ID, Name: "HQ"}
	st.CreateSite(ctx, site)
	dev := &model.Device{ID: store.NewID(), Hostname: "pc", OS: "linux"}
	st.CreateDevice(ctx, dev, auth.HashToken("t"))
	st.SetDeviceSite(ctx, dev.ID, &site.ID)

	mkPolicy := func(name string, n model.PolicyNotifications, targetType, targetID string) (policyID, asgID string) {
		p := &model.Policy{ID: store.NewID(), Name: name}
		if err := st.CreatePolicy(ctx, p); err != nil {
			t.Fatalf("CreatePolicy %s: %v", name, err)
		}
		if err := st.SetPolicyNotifications(ctx, p.ID, n); err != nil {
			t.Fatalf("SetPolicyNotifications %s: %v", name, err)
		}
		a := &model.Assignment{ID: store.NewID(), PolicyID: p.ID, TargetType: targetType, TargetID: targetID}
		if err := st.AddAssignment(ctx, a); err != nil {
			t.Fatalf("AddAssignment %s: %v", name, err)
		}
		return p.ID, a.ID
	}
	resolve := func() store.SoftwareAlertSetting {
		s, err := st.ResolveSoftwareAlerts(ctx, dev.ID)
		if err != nil {
			t.Fatalf("ResolveSoftwareAlerts: %v", err)
		}
		return s
	}

	// Ohne Richtlinie: globaler Standard.
	if err := st.SaveAlertConfig(ctx, model.AlertConfig{Enabled: true, AlertSoftware: true}); err != nil {
		t.Fatal(err)
	}
	if s := resolve(); !s.Enabled || s.Source != "global" || len(s.Kinds) != 0 || len(s.Ignore) != 0 {
		t.Fatalf("Standard erwartet, bekam %+v", s)
	}

	pC, _ := mkPolicy("Client aus", model.PolicyNotifications{SoftwareAlerts: "off"}, "client", cl.ID)
	pS, asgS := mkPolicy("Standort an", model.PolicyNotifications{
		SoftwareAlerts: "on", SoftwareAlertKinds: []string{"added"}, SoftwareIgnore: []string{"lib*"},
	}, "site", site.ID)
	pD, asgD := mkPolicy("Gerät an", model.PolicyNotifications{
		SoftwareAlerts: "on", SoftwareIgnore: []string{"Edge*"},
	}, "device", dev.ID)

	if s := resolve(); !s.Enabled || s.Source != "policy" || s.PolicyID != pD || len(s.Ignore) != 1 || s.Ignore[0] != "Edge*" {
		t.Fatalf("Geräte-Richtlinie sollte gewinnen, bekam %+v", s)
	}
	st.DeleteAssignment(ctx, asgD)
	if s := resolve(); !s.Enabled || s.PolicyID != pS || len(s.Kinds) != 1 || s.Kinds[0] != "added" || len(s.Ignore) != 1 {
		t.Fatalf("Standort-Richtlinie sollte gewinnen, bekam %+v", s)
	}
	// „Standard“ in der Standort-Richtlinie macht sie transparent → Client gewinnt.
	st.SetPolicyNotifications(ctx, pS, model.PolicyNotifications{SoftwareAlerts: ""})
	if s := resolve(); s.Enabled || s.PolicyID != pC || s.Source != "policy" {
		t.Fatalf("Client-Richtlinie sollte gewinnen, bekam %+v", s)
	}
	// Geräte-Override überschreibt nur den Schalter, nicht die Richtlinien-Zuordnung.
	st.SetDeviceSoftwareAlerts(ctx, dev.ID, "on")
	if s := resolve(); !s.Enabled || s.Source != "device" || s.PolicyID != pC {
		t.Fatalf("Geräte-Override on erwartet, bekam %+v", s)
	}
	st.SetDeviceSoftwareAlerts(ctx, dev.ID, "")
	st.DeleteAssignment(ctx, asgS)
	st.SetPolicyNotifications(ctx, pC, model.PolicyNotifications{SoftwareAlerts: ""})
	if s := resolve(); !s.Enabled || s.Source != "global" {
		t.Fatalf("zurück zum Standard erwartet, bekam %+v", s)
	}
	st.SetDeviceSoftwareAlerts(ctx, dev.ID, "off")
	if s := resolve(); s.Enabled || s.Source != "device" {
		t.Fatalf("Geräte-Override off erwartet, bekam %+v", s)
	}
}

func TestSoftwareAlertSettingFilter(t *testing.T) {
	events := []model.SoftwareEvent{
		{Change: "added", Name: "Firefox"},
		{Change: "removed", Name: "lib-foo"},
		{Change: "updated", Name: "Microsoft Edge"},
		{Change: "updated", Name: "Edge WebView"},
	}
	names := func(ev []model.SoftwareEvent) []string {
		var out []string
		for _, e := range ev {
			out = append(out, e.Name)
		}
		return out
	}
	if got := (store.SoftwareAlertSetting{}).Filter(events); len(got) != 4 {
		t.Fatalf("leere Einstellung sollte alles durchlassen, bekam %v", names(got))
	}
	got := store.SoftwareAlertSetting{Kinds: []string{"added", "updated"}, Ignore: []string{"microsoft edge*"}}.Filter(events)
	if len(got) != 2 || got[0].Name != "Firefox" || got[1].Name != "Edge WebView" {
		t.Fatalf("Filter falsch: %v", names(got))
	}
	if got := (store.SoftwareAlertSetting{Kinds: []string{"removed"}}).Filter(events); len(got) != 1 || got[0].Name != "lib-foo" {
		t.Fatalf("Kinds=removed falsch: %v", names(got))
	}
	if got := (store.SoftwareAlertSetting{Ignore: []string{"[bad"}}).Filter(events); len(got) != 4 {
		t.Fatalf("ungültiges Muster darf nichts verschlucken, bekam %v", names(got))
	}
}

func TestSetPolicyNotificationsRoundtrip(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()
	p := &model.Policy{ID: store.NewID(), Name: "P"}
	if err := st.CreatePolicy(ctx, p); err != nil {
		t.Fatal(err)
	}
	list, _ := st.ListPolicies(ctx)
	if n := list[0].Notifications; n.SoftwareAlerts != "" || len(n.SoftwareAlertKinds) != 3 || len(n.SoftwareIgnore) != 0 {
		t.Fatalf("Defaults falsch: %+v", n)
	}
	want := model.PolicyNotifications{SoftwareAlerts: "on", SoftwareAlertKinds: []string{}, SoftwareIgnore: []string{"lib*", "Microsoft Edge*"}}
	if err := st.SetPolicyNotifications(ctx, p.ID, want); err != nil {
		t.Fatal(err)
	}
	list, _ = st.ListPolicies(ctx)
	n := list[0].Notifications
	if n.SoftwareAlerts != "on" || len(n.SoftwareAlertKinds) != 0 || len(n.SoftwareIgnore) != 2 || n.SoftwareIgnore[1] != "Microsoft Edge*" {
		t.Fatalf("Roundtrip falsch: %+v", n)
	}
}
