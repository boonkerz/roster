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

// TestAutoBlockFleetSubnet: ohne Richtlinien-Regel sperrt die globale Einstellung die
// Quell-IP; mit Flotten-Sperre auch auf dem zweiten überwachten Gerät (nicht auf dem
// unüberwachten dritten); ab der eingestellten Zahl auffälliger IPs aus demselben /24 wird das
// Netz statt der IP gesperrt. Log-Quellen landen im Bundle und machen ein Gerät „überwacht“.
func TestAutoBlockFleetSubnet(t *testing.T) {
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
	mk("laptop")
	// web-01: Anmeldungen überwachen; sftp-01: nur Log-Quelle; laptop: nichts.
	pAuth := &model.Policy{ID: store.NewID(), Name: "Server"}
	pLog := &model.Policy{ID: store.NewID(), Name: "Container"}
	for _, p := range []*model.Policy{pAuth, pLog} {
		if err := st.CreatePolicy(ctx, p); err != nil {
			t.Fatal(err)
		}
	}
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(st.AddAssignment(ctx, &model.Assignment{ID: store.NewID(), PolicyID: pAuth.ID, TargetType: "device", TargetID: web.ID}))
	must(st.AddAssignment(ctx, &model.Assignment{ID: store.NewID(), PolicyID: pLog.ID, TargetType: "device", TargetID: sftp.ID}))
	must(st.SetPolicySecurity(ctx, pAuth.ID, model.PolicySecurity{FIMIntervalMin: 60, AuthMonitor: true, ComplianceProfile: "server", ComplianceDisabled: []string{}, ComplianceIntervalH: 24}))
	must(st.SaveLogSource(ctx, &model.PolicyLogSource{PolicyID: pLog.ID, Name: "sftp", Type: "docker", Target: "*sftp*", Format: "sshd", Enabled: true}))

	// Bundle des Log-Quellen-Geräts enthält die Quelle, obwohl AuthMonitor aus ist.
	b, err := st.EffectivePolicy(ctx, sftp.ID)
	if err != nil || b.Security == nil || len(b.Security.LogSources) != 1 || b.Security.LogSources[0].Name != "sftp" || b.Security.AuthMonitor {
		t.Fatalf("bundle: %v %+v", err, b.Security)
	}
	mon, err := st.MonitoredDeviceIDs(ctx)
	if err != nil || len(mon) != 2 {
		t.Fatalf("monitored: %v %+v", err, mon)
	}

	// Globale Auto-Sperre an, Flotte an, Netzsperre ab 3 IPs, Dauer 7 Tage.
	must(st.SetSetting(ctx, store.SettingSecAutoBlock, "1"))
	must(st.SetSetting(ctx, store.SettingSecFleetBlock, "1"))
	must(st.SetSetting(ctx, store.SettingSecSubnetIPs, "3"))
	must(st.SetSetting(ctx, store.SettingSecAutoBlockMinutes, "10080"))
	must(st.SetSetting(ctx, store.SettingSecResponseCooldown, "0"))

	// 1) Brute-Force von einer einzelnen IP → IP auf web-01 und sftp-01 gesperrt, laptop nicht.
	base := time.Now().Add(-5 * time.Minute)
	var events []shared.AuthEvent
	for i := 0; i < 12; i++ {
		events = append(events, shared.AuthEvent{Time: base.Add(time.Duration(i) * 10 * time.Second), Kind: "login_failed", User: "root", SourceIP: "203.0.113.5", Service: "sshd", RecordID: "a" + string(rune('a'+i))})
	}
	incs := s.processAuthEvents(ctx, web, events)
	if len(incs) != 1 || incs[0].Kind != "brute_force" {
		t.Fatalf("incidents: %+v", incs)
	}
	s.runResponses(ctx, web, incs)
	blocks, _ := st.IPBlocks(ctx, nil, "", 0)
	got := map[string]string{}
	for _, bl := range blocks {
		got[bl.Hostname] = bl.IP
	}
	if got["web-01"] != "203.0.113.5" || got["sftp-01"] != "203.0.113.5" || got["laptop"] != "" {
		t.Fatalf("fleet blocks: %+v", got)
	}
	inc, _ := st.GetIncident(ctx, incs[0].ID)
	if inc.Response != "block_ip 203.0.113.5" || inc.ResponseStatus != "queued (+1 Geräte)" {
		t.Errorf("incident response: %q %q", inc.Response, inc.ResponseStatus)
	}
	for _, bl := range blocks {
		if bl.Until == nil || time.Until(*bl.Until) < 6*24*time.Hour {
			t.Errorf("Dauer: %+v", bl.Until)
		}
	}

	// 2) Drei weitere IPs aus 198.51.100.0/24 fallen auf (zwei mit wenigen Versuchen, die
	//    dritte mit Brute-Force) → das ganze Netz wird gesperrt.
	events = nil
	n := 0
	for _, ip := range []string{"198.51.100.10", "198.51.100.20"} {
		for i := 0; i < 3; i++ {
			n++
			events = append(events, shared.AuthEvent{Time: base.Add(time.Duration(n) * time.Second), Kind: "login_failed", User: "root", SourceIP: ip, Service: "sshd", RecordID: "b" + string(rune('a'+n))})
		}
	}
	for i := 0; i < 12; i++ {
		n++
		events = append(events, shared.AuthEvent{Time: base.Add(time.Duration(n) * time.Second), Kind: "login_failed", User: "root", SourceIP: "198.51.100.30", Service: "sshd", RecordID: "c" + string(rune('a'+i))})
	}
	incs = s.processAuthEvents(ctx, web, events)
	if len(incs) != 1 || incs[0].SourceIP != "198.51.100.30" {
		t.Fatalf("incidents 2: %+v", incs)
	}
	s.runResponses(ctx, web, incs)
	if bl, _ := st.ActiveIPBlock(ctx, web.ID, "198.51.100.0/24"); bl == nil {
		t.Fatalf("Netzsperre fehlt")
	}
	if bl, _ := st.ActiveIPBlock(ctx, sftp.ID, "198.51.100.0/24"); bl == nil {
		t.Fatalf("Netzsperre Flotte fehlt")
	}
	if bl, _ := st.ActiveIPBlock(ctx, web.ID, "198.51.100.30"); bl != nil {
		t.Errorf("Einzel-IP sollte nicht zusätzlich gesperrt sein: %+v", bl)
	}

	// 3) Richtlinien-Regel hat Vorrang: run_script statt Sperre.
	must(st.SaveResponse(ctx, &model.PolicyResponse{PolicyID: pAuth.ID, Trigger: "scan", Action: "run_script", ScriptID: "missing", Platforms: []string{}, Enabled: true}))
	var probes []shared.AuthEvent
	for i := 0; i < 25; i++ {
		probes = append(probes, shared.AuthEvent{Time: base.Add(time.Duration(i) * time.Second), Kind: "probe", SourceIP: "192.0.2.77", Service: "nginx-proxy", Detail: "/wp-login.php", RecordID: "p" + string(rune('a'+i))})
	}
	incs = s.processAuthEvents(ctx, web, probes)
	if len(incs) != 1 || incs[0].Kind != "scan" {
		t.Fatalf("scan: %+v", incs)
	}
	s.runResponses(ctx, web, incs)
	if bl, _ := st.ActiveIPBlock(ctx, web.ID, "192.0.2.77"); bl != nil {
		t.Errorf("Richtlinien-Regel (run_script) muss Vorrang haben, keine Sperre erwartet")
	}
	// Ohne Regel: Scan wird gesperrt.
	s2 := mk("web-02")
	must(st.AddAssignment(ctx, &model.Assignment{ID: store.NewID(), PolicyID: pLog.ID, TargetType: "device", TargetID: s2.ID}))
	incs = s.processAuthEvents(ctx, s2, probes)
	s.runResponses(ctx, s2, incs)
	if bl, _ := st.ActiveIPBlock(ctx, s2.ID, "192.0.2.77"); bl == nil {
		t.Errorf("Scan ohne Regel muss gesperrt werden")
	}
	// Nie-sperren-Liste.
	must(st.SetSetting(ctx, store.SettingSecNeverBlock, "192.0.2.0/24"))
	s3 := mk("web-03")
	must(st.AddAssignment(ctx, &model.Assignment{ID: store.NewID(), PolicyID: pLog.ID, TargetType: "device", TargetID: s3.ID}))
	incs = s.processAuthEvents(ctx, s3, probes)
	s.runResponses(ctx, s3, incs)
	if bl, _ := st.ActiveIPBlock(ctx, s3.ID, "192.0.2.77"); bl != nil {
		t.Errorf("never-block missachtet")
	}
}

// TestResolveSecurityAlerts: ohne eigene Einstellung werden alle Arten gemeldet; die
// Richtlinie am Gerät gewinnt vor der am Client; '*' zählt nicht als eigene Einstellung.
func TestResolveSecurityAlerts(t *testing.T) {
	st, err := store.Open("sqlite://" + filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	ctx := context.Background()
	d := &model.Device{ID: store.NewID(), Hostname: "srv", OS: "ubuntu"}
	if err := st.CreateDevice(ctx, d, auth.HashToken("x")); err != nil {
		t.Fatal(err)
	}
	p := &model.Policy{ID: store.NewID(), Name: "P"}
	if err := st.CreatePolicy(ctx, p); err != nil {
		t.Fatal(err)
	}
	if err := st.AddAssignment(ctx, &model.Assignment{ID: store.NewID(), PolicyID: p.ID, TargetType: "device", TargetID: d.ID}); err != nil {
		t.Fatal(err)
	}
	set := st.ResolveSecurityAlerts(ctx, d.ID)
	if len(set.Kinds) != len(model.AllIncidentKinds) || set.SkipBlocked {
		t.Fatalf("default: %+v", set)
	}
	n := model.PolicyNotifications{SoftwareAlertKinds: []string{}, SoftwareIgnore: []string{}, SecurityAlertKinds: []string{"brute_force_success", "fim_change"}, SecurityAlertSkipBlocked: true}
	if err := st.SetPolicyNotifications(ctx, p.ID, n); err != nil {
		t.Fatal(err)
	}
	set = st.ResolveSecurityAlerts(ctx, d.ID)
	if set.Kinds["brute_force"] || !set.Kinds["fim_change"] || !set.Kinds["brute_force_success"] || !set.SkipBlocked {
		t.Fatalf("configured: %+v", set)
	}
	// Alle Arten gewählt → wieder Standard ('*'), SkipBlocked bleibt eigene Einstellung.
	n.SecurityAlertKinds = append([]string(nil), model.AllIncidentKinds...)
	n.SecurityAlertSkipBlocked = false
	if err := st.SetPolicyNotifications(ctx, p.ID, n); err != nil {
		t.Fatal(err)
	}
	ps, _ := st.ListPolicies(ctx)
	if len(ps) != 1 || len(ps[0].Notifications.SecurityAlertKinds) != len(model.AllIncidentKinds) {
		t.Fatalf("list: %+v", ps)
	}
}
