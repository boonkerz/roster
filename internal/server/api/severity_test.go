package api

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/boonkerz/roster/internal/server/auth"
	"github.com/boonkerz/roster/internal/server/config"
	"github.com/boonkerz/roster/internal/server/model"
	"github.com/boonkerz/roster/internal/server/store"
	"github.com/boonkerz/roster/internal/shared"
)

// TestSeverityOverride: der Schweregrad je Vorfallsart aus den Einstellungen überschreibt den
// Standard des Katalogs (Brute-Force warning → critical); ungültige Werte werden ignoriert.
func TestSeverityOverride(t *testing.T) {
	st, err := store.Open("sqlite://" + filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	ctx := context.Background()
	s := &Server{store: st, cfg: config.Config{}, log: slog.New(slog.NewTextHandler(io.Discard, nil)), term: newTermHub()}
	d := &model.Device{ID: store.NewID(), Hostname: "web-01", OS: "ubuntu"}
	if err := st.CreateDevice(ctx, d, auth.HashToken("x")); err != nil {
		t.Fatal(err)
	}
	if err := st.SetSetting(ctx, store.SettingSecSeverityPrefix+"brute_force", "critical"); err != nil {
		t.Fatal(err)
	}
	_ = st.SetSetting(ctx, store.SettingSecSeverityPrefix+"scan", "bogus")
	cfg := s.securitySettings(ctx)
	if cfg.Severity["brute_force"] != "critical" || cfg.Severity["scan"] != "" {
		t.Fatalf("settings: %+v", cfg.Severity)
	}
	base := time.Now().Add(-time.Minute)
	var events []shared.AuthEvent
	for i := 0; i < 12; i++ {
		events = append(events, shared.AuthEvent{Time: base.Add(time.Duration(i) * time.Second), Kind: "login_failed", User: "root", SourceIP: "203.0.113.5", Service: "sshd", RecordID: "r" + string(rune('a'+i))})
	}
	incs := s.processAuthEvents(ctx, d, events)
	if len(incs) != 1 || incs[0].Kind != "brute_force" || incs[0].Severity != "critical" {
		t.Fatalf("incidents: %+v", incs)
	}
	got, _ := st.Incidents(ctx, store.IncidentFilter{DeviceID: d.ID})
	if len(got) != 1 || got[0].Severity != "critical" || len(got[0].Mitre) == 0 {
		t.Fatalf("gespeichert: %+v", got)
	}
}
