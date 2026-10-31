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

// TestSecurityPolicyBundle: Regeln vereinigt, Schalter ODER, Intervall Minimum,
// Compliance-Disabled als Schnittmenge; FIM-Ereignisse speichern/bestätigen.
func TestSecurityPolicyBundle(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()
	dev := &model.Device{ID: store.NewID(), Hostname: "srv", OS: "debian"}
	if err := st.CreateDevice(ctx, dev, auth.HashToken("s")); err != nil {
		t.Fatal(err)
	}
	p1 := &model.Policy{ID: store.NewID(), Name: "A"}
	p2 := &model.Policy{ID: store.NewID(), Name: "B"}
	for _, p := range []*model.Policy{p1, p2} {
		if err := st.CreatePolicy(ctx, p); err != nil {
			t.Fatal(err)
		}
		if err := st.AddAssignment(ctx, &model.Assignment{ID: store.NewID(), PolicyID: p.ID, TargetType: "device", TargetID: dev.ID}); err != nil {
			t.Fatal(err)
		}
	}
	// Ohne Einstellungen: kein Security-Bundle.
	b, err := st.EffectivePolicy(ctx, dev.ID)
	if err != nil || b == nil || b.Security != nil {
		t.Fatalf("leer: %v %+v", err, b)
	}
	must := func(err error) {
		if err != nil {
			t.Fatal(err)
		}
	}
	must(st.SetPolicySecurity(ctx, p1.ID, model.PolicySecurity{FIMIntervalMin: 30, AuthMonitor: false, ComplianceEnabled: true, ComplianceProfile: "server", ComplianceDisabled: []string{"a", "b"}, ComplianceIntervalH: 24}))
	must(st.SetPolicySecurity(ctx, p2.ID, model.PolicySecurity{FIMIntervalMin: 120, AuthMonitor: true, ComplianceEnabled: true, ComplianceProfile: "workstation", ComplianceDisabled: []string{"b", "c"}, ComplianceIntervalH: 6}))
	r1 := &model.PolicyFIMRule{PolicyID: p1.ID, Path: "/etc", Recursive: true, Exclude: []string{"*.log"}, Platforms: []string{"linux"}, Alert: true}
	r2 := &model.PolicyFIMRule{PolicyID: p2.ID, Path: "/etc", Recursive: true, Platforms: []string{"linux"}, Alert: false} // Duplikat
	r3 := &model.PolicyFIMRule{PolicyID: p2.ID, Path: "/usr/local/bin", Recursive: true, Alert: true}
	must(st.SaveFIMRule(ctx, r1))
	must(st.SaveFIMRule(ctx, r2))
	must(st.SaveFIMRule(ctx, r3))
	b, err = st.EffectivePolicy(ctx, dev.ID)
	must(err)
	sec := b.Security
	if sec == nil || sec.FIM == nil || sec.FIM.IntervalMin != 30 || len(sec.FIM.Rules) != 2 || !sec.AuthMonitor {
		t.Fatalf("bundle: %+v", sec)
	}
	if sec.Compliance == nil || !sec.Compliance.Enabled || sec.Compliance.Profile != "workstation" || sec.Compliance.IntervalH != 6 || len(sec.Compliance.Disabled) != 1 || sec.Compliance.Disabled[0] != "b" {
		t.Fatalf("compliance: %+v", sec.Compliance)
	}
	// Policy-Laden liefert Security + Regeln.
	pols, err := st.ListPolicies(ctx)
	must(err)
	var got *model.Policy
	for i := range pols {
		if pols[i].ID == p1.ID {
			got = &pols[i]
		}
	}
	if got == nil || got.Security.FIMIntervalMin != 30 || len(got.FIMRules) != 1 || got.FIMRules[0].Exclude[0] != "*.log" {
		t.Fatalf("ListPolicies: %+v", got)
	}
	// Regel ändern/löschen
	r1.Path = "/etc/ssh"
	must(st.SaveFIMRule(ctx, r1))
	rr, _ := st.GetFIMRule(ctx, r1.ID)
	if rr.Path != "/etc/ssh" {
		t.Fatalf("update: %+v", rr)
	}
	must(st.DeleteFIMRule(ctx, r1.ID))
	if _, err := st.GetFIMRule(ctx, r1.ID); err == nil {
		t.Fatal("gelöscht?")
	}
	// Ereignisse
	now := time.Now()
	must(st.InsertFIMEvents(ctx, dev.ID, []shared.FIMEvent{
		{Time: now, Change: "baseline", Size: 10},
		{Time: now, RuleID: r3.ID, Path: "/usr/local/bin/x", Change: "created", NewHash: "abc", Alert: true},
		{Time: now, RuleID: r3.ID, Path: "/usr/local/bin/y", Change: "modified", OldHash: "a", NewHash: "b", Alert: true},
	}))
	ev, err := st.FIMEvents(ctx, store.FIMFilter{DeviceID: dev.ID})
	if err != nil || len(ev) != 3 {
		t.Fatalf("events: %v %d", err, len(ev))
	}
	open, _ := st.FIMEvents(ctx, store.FIMFilter{DeviceID: dev.ID, OpenOnly: true})
	if len(open) != 2 { // Baseline gilt als bestätigt
		t.Fatalf("open: %d", len(open))
	}
	d, _ := st.GetDevice(ctx, dev.ID)
	if d.FIMOpen != 2 {
		t.Fatalf("FIMOpen: %d", d.FIMOpen)
	}
	n, err := st.AckFIMEvents(ctx, []string{open[0].ID}, nil)
	if err != nil || n != 1 {
		t.Fatalf("ack: %v %d", err, n)
	}
	if n, _ := st.AckFIMEvents(ctx, []string{open[1].ID}, map[string]bool{}); n != 0 {
		t.Fatal("leerer scope darf nichts bestätigen")
	}
	if rows, _ := st.FIMEvents(ctx, store.FIMFilter{Query: "/x", OpenOnly: true}); len(rows) != 0 {
		t.Fatalf("query/ack: %+v", rows)
	}
	must(st.PruneFIMEvents(ctx, now.Add(time.Minute)))
	if rows, _ := st.FIMEvents(ctx, store.FIMFilter{DeviceID: dev.ID}); len(rows) != 0 {
		t.Fatalf("prune: %d", len(rows))
	}
}
