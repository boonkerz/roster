package api

import (
	"context"
	"io"
	"log/slog"
	"net"
	"path/filepath"
	"testing"
	"time"

	"github.com/boonkerz/roster/internal/server/auth"
	"github.com/boonkerz/roster/internal/server/config"
	"github.com/boonkerz/roster/internal/server/model"
	"github.com/boonkerz/roster/internal/server/store"
	"github.com/boonkerz/roster/internal/server/threat"
	"github.com/boonkerz/roster/internal/shared"
)

// TestThreatListedFleetBlock: ein einzelner Fehlversuch von einer gelisteten IP erzeugt den
// Vorfall „threat_listed“ und sperrt die IP auf allen überwachten Geräten – auch wenn die
// allgemeine Flottensperre und die automatische Sperre aus sind. Mit sec_threat_block=0
// bleibt es beim Vorfall.
func TestThreatListedFleetBlock(t *testing.T) {
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
	web, sftp := mk("web-01"), mk("sftp-01")
	p := &model.Policy{ID: store.NewID(), Name: "Server"}
	if err := st.CreatePolicy(ctx, p); err != nil {
		t.Fatal(err)
	}
	for _, d := range []*model.Device{web, sftp} {
		if err := st.AddAssignment(ctx, &model.Assignment{ID: store.NewID(), PolicyID: p.ID, TargetType: "device", TargetID: d.ID}); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.SetPolicySecurity(ctx, p.ID, model.PolicySecurity{FIMIntervalMin: 60, AuthMonitor: true, ComplianceProfile: "server", ComplianceDisabled: []string{}, ComplianceIntervalH: 24}); err != nil {
		t.Fatal(err)
	}
	_ = st.SetSetting(ctx, store.SettingSecAutoBlock, "0")
	_ = st.SetSetting(ctx, store.SettingSecFleetBlock, "0")
	_ = st.SetSetting(ctx, store.SettingSecAutoBlockMinutes, "60")

	ix := threat.NewIndex()
	_, n, _ := net.ParseCIDR("203.0.113.0/24")
	ix.Add("Spamhaus DROP", nil, []*net.IPNet{n})
	threat.Install(ix)
	t.Cleanup(func() { threat.Install(nil) })

	events := []shared.AuthEvent{{Time: time.Now().Add(-time.Minute), Kind: "login_failed", User: "root", SourceIP: "203.0.113.77", Service: "sshd", RecordID: "a"}}
	incs := s.processAuthEvents(ctx, web, events)
	if len(incs) != 1 || incs[0].Kind != "threat_listed" {
		t.Fatalf("incidents: %+v", incs)
	}
	s.runResponses(ctx, web, incs)
	for _, d := range []*model.Device{web, sftp} {
		if b, _ := st.ActiveIPBlock(ctx, d.ID, "203.0.113.77"); b == nil {
			t.Fatalf("Sperre fehlt auf %s", d.Hostname)
		}
	}
	got, _ := st.Incidents(ctx, store.IncidentFilter{DeviceID: web.ID, Threat: true})
	if len(got) != 1 || len(got[0].Threat) != 1 || got[0].Threat[0] != "Spamhaus DROP" {
		t.Fatalf("Anreicherung/Filter: %+v", got)
	}
	if got, _ := st.Incidents(ctx, store.IncidentFilter{DeviceID: web.ID, Threat: true, Technique: "T1595"}); len(got) != 1 {
		t.Fatalf("Technik T1595: %d", len(got))
	}

	// Nur melden, nicht sperren.
	_ = st.SetSetting(ctx, store.SettingSecThreatBlock, "0")
	d3 := mk("web-02")
	if err := st.AddAssignment(ctx, &model.Assignment{ID: store.NewID(), PolicyID: p.ID, TargetType: "device", TargetID: d3.ID}); err != nil {
		t.Fatal(err)
	}
	events[0].SourceIP, events[0].RecordID = "203.0.113.78", "b"
	incs = s.processAuthEvents(ctx, d3, events)
	s.runResponses(ctx, d3, incs)
	if b, _ := st.ActiveIPBlock(ctx, d3.ID, "203.0.113.78"); b != nil {
		t.Fatalf("mit sec_threat_block=0 darf nicht gesperrt werden: %+v", b)
	}
	// Aus: kein Vorfall.
	_ = st.SetSetting(ctx, store.SettingSecThreatEnabled, "0")
	events[0].SourceIP, events[0].RecordID = "203.0.113.79", "c"
	if incs := s.processAuthEvents(ctx, d3, events); len(incs) != 0 {
		t.Fatalf("aus: %+v", incs)
	}
}
