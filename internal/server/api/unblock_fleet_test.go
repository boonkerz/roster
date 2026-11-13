package api

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/boonkerz/roster/internal/server/auth"
	"github.com/boonkerz/roster/internal/server/config"
	"github.com/boonkerz/roster/internal/server/model"
	"github.com/boonkerz/roster/internal/server/store"
)

// TestReleaseEverywhere: „IP überall freigeben“ schickt unblock_ip an alle überwachten Geräte
// (auch ohne Eintrag), setzt vorhandene Einträge auf released und nennt Netzsperren, die die
// IP enthalten; die Sperren-Suche findet eine IP auch in solchen Netzsperren.
func TestReleaseEverywhere(t *testing.T) {
	st, err := store.Open("sqlite://" + filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	ctx := context.Background()
	s := &Server{store: st, cfg: config.Config{}, log: slog.New(slog.NewTextHandler(io.Discard, nil)), term: newTermHub()}
	mk := func(name string) *model.Device {
		d := &model.Device{ID: store.NewID(), Hostname: name, OS: "ubuntu"}
		if err := st.CreateDevice(ctx, d, auth.HashToken(name)); err != nil {
			t.Fatal(err)
		}
		return d
	}
	web, other, laptop := mk("web-01"), mk("web-02"), mk("laptop")
	p := &model.Policy{ID: store.NewID(), Name: "Server"}
	if err := st.CreatePolicy(ctx, p); err != nil {
		t.Fatal(err)
	}
	for _, d := range []*model.Device{web, other} {
		if err := st.AddAssignment(ctx, &model.Assignment{ID: store.NewID(), PolicyID: p.ID, TargetType: "device", TargetID: d.ID}); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.SetPolicySecurity(ctx, p.ID, model.PolicySecurity{FIMIntervalMin: 60, AuthMonitor: true, ComplianceProfile: "server", ComplianceDisabled: []string{}, ComplianceIntervalH: 24}); err != nil {
		t.Fatal(err)
	}
	// web: Netzsperre, die die IP enthält; laptop (nicht überwacht): Einzel-IP.
	if err := st.InsertIPBlock(ctx, &model.IPBlock{DeviceID: web.ID, IP: "198.51.100.0/24", Status: "active", Reason: "scan", CreatedBy: "system"}); err != nil {
		t.Fatal(err)
	}
	if err := st.InsertIPBlock(ctx, &model.IPBlock{DeviceID: laptop.ID, IP: "198.51.100.30", Status: "active", Reason: "manuell", CreatedBy: "admin"}); err != nil {
		t.Fatal(err)
	}

	// Suche nach der IP findet beide: Text-Treffer und enthaltende Netzsperre.
	rows, err := st.IPBlocksFiltered(ctx, store.IPBlockFilter{Query: "198.51.100.30", Status: "active"})
	if err != nil || len(rows) != 2 {
		t.Fatalf("suche: %v %+v", err, rows)
	}
	if rows, _ := st.IPBlocksFiltered(ctx, store.IPBlockFilter{Query: "198.51.101.30"}); len(rows) != 0 {
		t.Fatalf("fremde IP gefunden: %+v", rows)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/security/unblock-ip", strings.NewReader(`{"ip":"198.51.100.30","all_devices":true}`))
	s.handleUnblockIPFleet(rec, req)
	var resp struct {
		Devices int      `json:"devices"`
		Nets    []string `json:"nets"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	// laptop (Eintrag) + web + other (überwacht, ohne Eintrag) = 3; das /24 wird genannt.
	if rec.Code != 200 || resp.Devices != 3 || len(resp.Nets) != 1 || resp.Nets[0] != "198.51.100.0/24" {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	for _, d := range []*model.Device{web, other, laptop} {
		cmds, _ := st.PendingCommands(ctx, d.ID)
		found := false
		for _, c := range cmds {
			if c.Type == "unblock_ip" && c.Payload["ip"] == "198.51.100.30" {
				found = true
			}
		}
		if !found {
			t.Errorf("%s: kein unblock_ip", d.Hostname)
		}
	}
	if rows, _ := st.IPBlocksFiltered(ctx, store.IPBlockFilter{DeviceID: laptop.ID, Status: "released"}); len(rows) != 1 {
		t.Fatalf("laptop-Eintrag nicht freigegeben: %+v", rows)
	}
	if bl, _ := st.ActiveIPBlock(ctx, web.ID, "198.51.100.0/24"); bl == nil {
		t.Fatal("Netzsperre darf durch Einzel-IP nicht aufgehoben werden")
	}
}
