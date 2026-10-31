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

func TestSecurityEventsStore(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()
	d := &model.Device{ID: store.NewID(), Hostname: "srv", OS: "debian"}
	if err := st.CreateDevice(ctx, d, auth.HashToken("s")); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	events := []shared.AuthEvent{
		{Time: now.Add(-3 * time.Minute), Kind: "login_failed", User: "root", SourceIP: "203.0.113.5", Service: "sshd", RecordID: "a"},
		{Time: now.Add(-2 * time.Minute), Kind: "invalid_user", User: "admin", SourceIP: "203.0.113.5", Service: "sshd", RecordID: "b"},
		{Time: now.Add(-1 * time.Minute), Kind: "login_ok", User: "thomas", SourceIP: "192.168.1.10", Service: "sshd", RecordID: "c"},
	}
	fresh, err := st.InsertAuthEvents(ctx, d.ID, events)
	if err != nil || len(fresh) != 3 {
		t.Fatalf("insert: %v %d", err, len(fresh))
	}
	fresh, err = st.InsertAuthEvents(ctx, d.ID, events)
	if err != nil || len(fresh) != 0 {
		t.Fatalf("idempotent: %v %d", err, len(fresh))
	}
	if since := st.AuthMonitorSince(ctx, d.ID); since.IsZero() {
		t.Fatal("auth_monitor_since nicht gesetzt")
	}
	fails, _ := st.FailedLoginsSince(ctx, d.ID, now.Add(-time.Hour))
	if len(fails["203.0.113.5"]) != 2 {
		t.Fatalf("fails: %+v", fails)
	}
	if err := st.UpsertKnownIPs(ctx, d.ID, events); err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertKnownIPs(ctx, d.ID, events); err != nil {
		t.Fatal(err)
	}
	known, _ := st.KnownIPs(ctx, d.ID)
	if !known["thomas@192.168.1.10"] || len(known) != 1 {
		t.Fatalf("known: %+v", known)
	}
	inc := &model.SecurityIncident{DeviceID: d.ID, TS: now, Kind: "brute_force", Severity: "warning", SourceIP: "203.0.113.5", Title: "BF"}
	if err := st.InsertIncident(ctx, inc); err != nil {
		t.Fatal(err)
	}
	inc2 := &model.SecurityIncident{DeviceID: d.ID, TS: now, Kind: "brute_force_success", Severity: "critical", SourceIP: "203.0.113.5", Username: "root", Title: "OK after BF"}
	if err := st.InsertIncident(ctx, inc2); err != nil {
		t.Fatal(err)
	}
	recent, _ := st.RecentIncidentIPs(ctx, d.ID, "brute_force", now.Add(-time.Hour))
	if _, ok := recent["203.0.113.5"]; !ok {
		t.Fatalf("recent: %+v", recent)
	}
	list, err := st.Incidents(ctx, store.IncidentFilter{Status: "open"})
	if err != nil || len(list) != 2 || list[0].Hostname != "srv" {
		t.Fatalf("incidents: %v %+v", err, list)
	}
	if rows, _ := st.Incidents(ctx, store.IncidentFilter{Severity: "critical"}); len(rows) != 1 || rows[0].Username != "root" {
		t.Fatalf("severity filter: %+v", rows)
	}
	if rows, _ := st.Incidents(ctx, store.IncidentFilter{Query: "203.0"}); len(rows) != 2 {
		t.Fatalf("query: %+v", rows)
	}
	if err := st.SetIncidentStatus(ctx, inc.ID, "acknowledged", "admin"); err != nil {
		t.Fatal(err)
	}
	got, _ := st.GetIncident(ctx, inc.ID)
	if got.Status != "acknowledged" || got.AcknowledgedBy != "admin" || got.AcknowledgedAt == nil {
		t.Fatalf("status: %+v", got)
	}
	dev, _ := st.GetDevice(ctx, d.ID)
	if dev.IncidentsOpen != 1 {
		t.Fatalf("IncidentsOpen: %d", dev.IncidentsOpen)
	}
	sum, err := st.SecuritySummary(ctx, nil)
	if err != nil || sum.OpenIncidents != 1 || sum.CriticalOpen != 1 || sum.FailedLogins24h != 2 || sum.DevicesMonitored != 1 || len(sum.TopIPs) != 1 || sum.TopIPs[0].IP != "203.0.113.5" {
		t.Fatalf("summary: %v %+v", err, sum)
	}
	ae, _ := st.AuthEvents(ctx, store.AuthEventFilter{Kind: "failed"})
	if len(ae) != 2 || ae[0].Kind != "invalid_user" {
		t.Fatalf("auth events failed: %+v", ae)
	}
	if ae, _ := st.AuthEvents(ctx, store.AuthEventFilter{IP: "192.168.1.10"}); len(ae) != 1 || ae[0].Username != "thomas" {
		t.Fatalf("auth events ip: %+v", ae)
	}
	if ae, _ := st.AuthEvents(ctx, store.AuthEventFilter{Allowed: map[string]bool{}}); len(ae) != 0 {
		t.Fatal("leerer scope")
	}
	if err := st.PruneSecurityEvents(ctx, now.Add(time.Minute), now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if ae, _ := st.AuthEvents(ctx, store.AuthEventFilter{}); len(ae) != 0 {
		t.Fatalf("prune events: %d", len(ae))
	}
	if rows, _ := st.Incidents(ctx, store.IncidentFilter{}); len(rows) != 1 || rows[0].Status != "open" {
		t.Fatalf("prune incidents (offene bleiben): %+v", rows)
	}
}
