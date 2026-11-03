package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/boonkerz/roster/internal/server/auth"
	"github.com/boonkerz/roster/internal/server/model"
	"github.com/boonkerz/roster/internal/server/store"
)

func TestResponsesAndIPBlocks(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()
	dev := &model.Device{ID: store.NewID(), Hostname: "srv", OS: "debian"}
	if err := st.CreateDevice(ctx, dev, auth.HashToken("s")); err != nil {
		t.Fatal(err)
	}
	p := &model.Policy{ID: store.NewID(), Name: "P"}
	if err := st.CreatePolicy(ctx, p); err != nil {
		t.Fatal(err)
	}
	if err := st.AddAssignment(ctx, &model.Assignment{ID: store.NewID(), PolicyID: p.ID, TargetType: "device", TargetID: dev.ID}); err != nil {
		t.Fatal(err)
	}
	r1 := &model.PolicyResponse{PolicyID: p.ID, Trigger: "brute_force", Action: "block_ip", Minutes: 30, Platforms: []string{"linux"}, Enabled: true}
	r2 := &model.PolicyResponse{PolicyID: p.ID, Trigger: "account_change", Action: "run_script", ScriptID: "sc1", Platforms: []string{}, Enabled: false}
	for _, r := range []*model.PolicyResponse{r1, r2} {
		if err := st.SaveResponse(ctx, r); err != nil {
			t.Fatal(err)
		}
	}
	active, err := st.ResponsesForDevice(ctx, dev.ID)
	if err != nil || len(active) != 1 || active[0].Action != "block_ip" || active[0].Minutes != 30 || active[0].Platforms[0] != "linux" {
		t.Fatalf("ResponsesForDevice: %v %+v", err, active)
	}
	r2.Enabled = true
	if err := st.SaveResponse(ctx, r2); err != nil {
		t.Fatal(err)
	}
	if active, _ = st.ResponsesForDevice(ctx, dev.ID); len(active) != 2 {
		t.Fatalf("nach enable: %d", len(active))
	}
	got, _ := st.GetResponse(ctx, r2.ID)
	if got.ScriptID != "sc1" {
		t.Fatalf("GetResponse: %+v", got)
	}
	pols, _ := st.ListPolicies(ctx)
	if len(pols) != 1 || len(pols[0].Responses) != 2 {
		t.Fatalf("ListPolicies responses: %+v", pols)
	}
	if err := st.DeleteResponse(ctx, r2.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.GetResponse(ctx, r2.ID); err == nil {
		t.Fatal("gelöscht?")
	}
	// IP-Sperren
	until := time.Now().Add(30 * time.Minute)
	b := &model.IPBlock{DeviceID: dev.ID, IP: "203.0.113.5", Reason: "BF", Until: &until, CommandID: "cmd1", CreatedBy: "system"}
	if err := st.InsertIPBlock(ctx, b); err != nil {
		t.Fatal(err)
	}
	if ab, _ := st.ActiveIPBlock(ctx, dev.ID, "203.0.113.5"); ab == nil || ab.Status != "pending" {
		t.Fatalf("ActiveIPBlock: %+v", ab)
	}
	if ab, _ := st.ActiveIPBlock(ctx, dev.ID, "203.0.113.6"); ab != nil {
		t.Fatal("fremde IP darf nicht aktiv sein")
	}
	bc, _ := st.IPBlockByCommand(ctx, "cmd1")
	if bc == nil || bc.ID != b.ID {
		t.Fatalf("by command: %+v", bc)
	}
	if err := st.SetIPBlockStatus(ctx, b.ID, "active", "ok"); err != nil {
		t.Fatal(err)
	}
	list, _ := st.IPBlocks(ctx, nil, "", 0)
	if len(list) != 1 || list[0].Status != "active" || list[0].Hostname != "srv" {
		t.Fatalf("IPBlocks: %+v", list)
	}
	// Abgelaufen → Status expired beim Lesen
	past := time.Now().Add(-time.Minute)
	b2 := &model.IPBlock{DeviceID: dev.ID, IP: "203.0.113.9", Until: &past, Status: "active", CreatedBy: "admin"}
	if err := st.InsertIPBlock(ctx, b2); err != nil {
		t.Fatal(err)
	}
	g, _ := st.GetIPBlock(ctx, b2.ID)
	if g.Status != "expired" {
		t.Fatalf("expired: %+v", g)
	}
	if ab, _ := st.ActiveIPBlock(ctx, dev.ID, "203.0.113.9"); ab != nil {
		t.Fatal("abgelaufene Sperre darf nicht aktiv sein")
	}
	if rows, _ := st.IPBlocks(ctx, map[string]bool{}, "", 0); len(rows) != 0 {
		t.Fatal("leerer scope")
	}
	// Vorfall mit Reaktion → RecentResponse / IncidentByResponseCommand
	inc := &model.SecurityIncident{DeviceID: dev.ID, TS: time.Now(), Kind: "brute_force", Severity: "warning", SourceIP: "203.0.113.5", Title: "BF"}
	if err := st.InsertIncident(ctx, inc); err != nil {
		t.Fatal(err)
	}
	if err := st.SetIncidentResponse(ctx, inc.ID, "block_ip 203.0.113.5", "queued", "cmd1"); err != nil {
		t.Fatal(err)
	}
	if recent, _ := st.RecentResponse(ctx, dev.ID, "block_ip 203.0.113.5", time.Now().Add(-time.Hour)); !recent {
		t.Fatal("RecentResponse")
	}
	if i2, _ := st.IncidentByResponseCommand(ctx, "cmd1"); i2 == nil || i2.ID != inc.ID {
		t.Fatalf("IncidentByResponseCommand: %+v", i2)
	}
}

// TestExpireUndeliveredBlocks: Sperrbefehle, deren Sperre vor der Zustellung abläuft
// (Gerät offline), werden erledigt und die Sperre als abgelaufen markiert; noch gültige
// und bereits zugestellte bleiben unberührt.
func TestExpireUndeliveredBlocks(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()
	dev := &model.Device{ID: store.NewID(), Hostname: "srv", OS: "debian"}
	if err := st.CreateDevice(ctx, dev, auth.HashToken("s")); err != nil {
		t.Fatal(err)
	}
	past, future := time.Now().Add(-time.Hour), time.Now().Add(time.Hour)
	queued, err := st.BlockIPOnDevices(ctx, []string{dev.ID}, "203.0.113.9", 60, &past, "alt", "", "system", "IP sperren")
	if err != nil || len(queued) != 1 {
		t.Fatalf("BlockIPOnDevices: %v %v", err, queued)
	}
	if _, err := st.BlockIPOnDevices(ctx, []string{dev.ID}, "203.0.113.10", 60, &future, "neu", "", "system", "IP sperren"); err != nil {
		t.Fatal(err)
	}
	n, err := st.ExpireUndeliveredBlocks(ctx, dev.ID)
	if err != nil || n != 1 {
		t.Fatalf("ExpireUndeliveredBlocks: n=%d err=%v", n, err)
	}
	cmds, err := st.PendingCommands(ctx, dev.ID)
	if err != nil || len(cmds) != 1 || cmds[0].Payload["ip"] != "203.0.113.10" {
		t.Fatalf("PendingCommands: %v %+v", err, cmds)
	}
	blocks, err := st.IPBlocksFiltered(ctx, store.IPBlockFilter{DeviceID: dev.ID, Status: "expired"})
	if err != nil || len(blocks) != 1 || blocks[0].IP != "203.0.113.9" {
		t.Fatalf("expired: %v %+v", err, blocks)
	}
	if b, _ := st.ActiveIPBlock(ctx, dev.ID, "203.0.113.10"); b == nil {
		t.Fatal("gültige Sperre fehlt")
	}
}
