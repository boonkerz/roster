package store_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/thomaspeterson/pc-inventory/internal/server/auth"
	"github.com/thomaspeterson/pc-inventory/internal/server/model"
	"github.com/thomaspeterson/pc-inventory/internal/server/store"
	"github.com/thomaspeterson/pc-inventory/internal/shared"
)

func newStore(t *testing.T) *store.Store {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	st, err := store.Open("sqlite://" + dbPath)
	if err != nil {
		t.Fatalf("store öffnen: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

func TestUserLifecycle(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()

	if n, _ := st.CountUsers(ctx); n != 0 {
		t.Fatalf("erwartete 0 benutzer, bekam %d", n)
	}
	u := &model.User{ID: store.NewID(), Username: "alice", Role: model.RoleAdmin, AuthSource: model.AuthLocal, PasswordHash: "x"}
	if err := st.CreateUser(ctx, u); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	got, err := st.GetUserByUsername(ctx, "alice")
	if err != nil {
		t.Fatalf("GetUserByUsername: %v", err)
	}
	if got.Role != model.RoleAdmin {
		t.Errorf("rolle = %q, erwartet admin", got.Role)
	}
	if _, err := st.GetUserByUsername(ctx, "bob"); err != store.ErrNotFound {
		t.Errorf("erwartete ErrNotFound, bekam %v", err)
	}
}

func TestEnrollmentTokenConsume(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()

	tok := &model.EnrollmentToken{ID: store.NewID(), Label: "test", MaxUses: 1}
	hash := auth.HashToken("geheim")
	if err := st.CreateEnrollmentToken(ctx, tok, hash); err != nil {
		t.Fatalf("CreateEnrollmentToken: %v", err)
	}
	if _, err := st.ConsumeEnrollmentToken(ctx, hash); err != nil {
		t.Fatalf("erste nutzung sollte ok sein: %v", err)
	}
	if _, err := st.ConsumeEnrollmentToken(ctx, hash); err != store.ErrTokenExhausted {
		t.Errorf("zweite nutzung sollte erschöpft sein, bekam %v", err)
	}
	if _, err := st.ConsumeEnrollmentToken(ctx, auth.HashToken("falsch")); err != store.ErrNotFound {
		t.Errorf("unbekanntes token sollte ErrNotFound geben, bekam %v", err)
	}
}

func TestDeviceInventoryAndRevoke(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()

	dev := &model.Device{ID: store.NewID(), Hostname: "pc01", OS: "windows"}
	token := auth.HashToken("agent-token")
	if err := st.CreateDevice(ctx, dev, token); err != nil {
		t.Fatalf("CreateDevice: %v", err)
	}

	inv := shared.Inventory{
		Hostname:    "pc01",
		CPUModel:    "Test CPU",
		CPUCores:    8,
		MemoryBytes: 1 << 34,
		Interfaces: []shared.Interface{
			{Name: "eth0", MAC: "aa:bb:cc:dd:ee:ff", IPv4: []string{"10.0.0.5"}},
		},
	}
	if err := st.UpdateInventory(ctx, dev.ID, inv); err != nil {
		t.Fatalf("UpdateInventory: %v", err)
	}

	got, err := st.GetDevice(ctx, dev.ID)
	if err != nil {
		t.Fatalf("GetDevice: %v", err)
	}
	if got.LastSeen == nil {
		t.Error("last_seen sollte gesetzt sein")
	}
	if len(got.Interfaces) != 1 || got.Interfaces[0].IPv4 != "10.0.0.5" {
		t.Errorf("interfaces falsch: %+v", got.Interfaces)
	}

	// Zweiter Checkin ersetzt die Interfaces (kein Duplikat).
	if err := st.UpdateInventory(ctx, dev.ID, inv); err != nil {
		t.Fatalf("zweiter UpdateInventory: %v", err)
	}
	got, _ = st.GetDevice(ctx, dev.ID)
	if len(got.Interfaces) != 1 {
		t.Errorf("interfaces sollten ersetzt werden, bekam %d", len(got.Interfaces))
	}

	// Token-Lookup vor und nach Widerruf.
	if _, err := st.DeviceByTokenHash(ctx, token); err != nil {
		t.Fatalf("DeviceByTokenHash: %v", err)
	}
	if err := st.RevokeDevice(ctx, dev.ID); err != nil {
		t.Fatalf("RevokeDevice: %v", err)
	}
	if _, err := st.DeviceByTokenHash(ctx, token); err != store.ErrNotFound {
		t.Errorf("nach widerruf sollte token nicht mehr auffindbar sein, bekam %v", err)
	}

	// Inventar-Historie enthält beide Snapshots.
	hist, err := st.InventoryHistory(ctx, dev.ID, 10)
	if err != nil {
		t.Fatalf("InventoryHistory: %v", err)
	}
	if len(hist) != 2 {
		t.Errorf("erwartete 2 snapshots, bekam %d", len(hist))
	}
}

func TestAlertChannelScopeAndSeverity(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()

	dev := &model.Device{ID: store.NewID(), Hostname: "pc", OS: "linux"}
	if err := st.CreateDevice(ctx, dev, auth.HashToken("t")); err != nil {
		t.Fatalf("CreateDevice: %v", err)
	}

	save := func(c model.AlertChannel) {
		if err := st.SaveAlertChannel(ctx, c); err != nil {
			t.Fatalf("SaveAlertChannel: %v", err)
		}
	}
	cfg := map[string]string{"url": "http://x"}
	save(model.AlertChannel{ID: store.NewID(), Type: "webhook", Name: "G", Enabled: true, Config: cfg, MinSeverity: "warning"})
	scopedID := store.NewID()
	save(model.AlertChannel{ID: scopedID, Type: "webhook", Name: "S", Enabled: true, Config: cfg, MinSeverity: "critical",
		Assignments: []model.ChannelScope{{TargetType: "device", TargetID: dev.ID}}})
	save(model.AlertChannel{ID: store.NewID(), Type: "webhook", Name: "O", Enabled: true, Config: cfg,
		Assignments: []model.ChannelScope{{TargetType: "device", TargetID: "anderes-geraet"}}})
	save(model.AlertChannel{ID: store.NewID(), Type: "webhook", Name: "Off", Enabled: false, Config: cfg})

	chs, err := st.ChannelsForDevice(ctx, dev.ID)
	if err != nil {
		t.Fatalf("ChannelsForDevice: %v", err)
	}
	names := map[string]bool{}
	for _, c := range chs {
		names[c.Name] = true
	}
	if len(chs) != 2 || !names["G"] || !names["S"] || names["O"] || names["Off"] {
		t.Fatalf("Geltungsbereich falsch aufgelöst: %v", names)
	}

	got, err := st.AlertChannel(ctx, scopedID)
	if err != nil {
		t.Fatalf("AlertChannel: %v", err)
	}
	if got.MinSeverity != "critical" || len(got.Assignments) != 1 || got.Assignments[0].TargetID != dev.ID {
		t.Fatalf("Round-Trip falsch: %+v", got)
	}

	// AlertChannels lädt je Kanal die Zuweisungen nach – muss ohne Verbindungs-
	// Deadlock laufen (SetMaxOpenConns(1)).
	all, err := st.AlertChannels(ctx)
	if err != nil || len(all) != 4 {
		t.Fatalf("AlertChannels: %v / %d", err, len(all))
	}
	for _, c := range all {
		if c.Name == "S" && (len(c.Assignments) != 1 || c.Assignments[0].TargetID != dev.ID) {
			t.Fatalf("AlertChannels-Zuweisung falsch: %+v", c)
		}
	}

	pol := &model.Policy{ID: store.NewID(), Name: "p"}
	if err := st.CreatePolicy(ctx, pol); err != nil {
		t.Fatalf("CreatePolicy: %v", err)
	}
	chk := &model.PolicyCheck{ID: store.NewID(), PolicyID: pol.ID, Name: "c", Type: "disk", Severity: "warning"}
	if err := st.AddCheck(ctx, chk); err != nil {
		t.Fatalf("AddCheck: %v", err)
	}
	sev, err := st.CheckSeverities(ctx, []string{chk.ID})
	if err != nil || sev[chk.ID] != "warning" {
		t.Fatalf("CheckSeverities: %v / %v", sev, err)
	}
}

func TestRemediation(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()

	fix := &model.Script{ID: store.NewID(), Name: "Restart svc", Shell: "shell", Content: "echo fix"}
	if err := st.CreateScript(ctx, fix); err != nil {
		t.Fatalf("CreateScript: %v", err)
	}
	pol := &model.Policy{ID: store.NewID(), Name: "p"}
	if err := st.CreatePolicy(ctx, pol); err != nil {
		t.Fatalf("CreatePolicy: %v", err)
	}
	// Check ohne Remediation -> kein Skript.
	plain := &model.PolicyCheck{ID: store.NewID(), PolicyID: pol.ID, Name: "plain", Type: "disk", Severity: "critical"}
	if err := st.AddCheck(ctx, plain); err != nil {
		t.Fatalf("AddCheck plain: %v", err)
	}
	if _, err := st.RemediationScript(ctx, plain.ID); err != store.ErrNotFound {
		t.Fatalf("erwartet ErrNotFound, bekam: %v", err)
	}
	// Check mit Remediation -> Skript wird geliefert und in checksOf zurückgegeben.
	chk := &model.PolicyCheck{ID: store.NewID(), PolicyID: pol.ID, Name: "c", Type: "disk", Severity: "critical", RemediationScriptID: &fix.ID}
	if err := st.AddCheck(ctx, chk); err != nil {
		t.Fatalf("AddCheck: %v", err)
	}
	sc, err := st.RemediationScript(ctx, chk.ID)
	if err != nil || sc.ID != fix.ID || sc.Content != "echo fix" {
		t.Fatalf("RemediationScript: %+v / %v", sc, err)
	}
	got, err := st.ListPolicies(ctx)
	if err != nil || len(got) != 1 || len(got[0].Checks) != 2 {
		t.Fatalf("ListPolicies: %v / %+v", err, got)
	}
	var found bool
	for _, c := range got[0].Checks {
		if c.ID == chk.ID {
			if c.RemediationScriptID == nil || *c.RemediationScriptID != fix.ID {
				t.Fatalf("RemediationScriptID nicht durchgereicht: %+v", c)
			}
			found = true
		}
	}
	if !found {
		t.Fatalf("Check nicht in ListPolicies gefunden")
	}
	// Cooldown: erst fällig, nach Markierung nicht mehr, nach Ablauf wieder.
	dev := "dev1"
	if !st.RemediationDue(ctx, dev, chk.ID, time.Hour) {
		t.Fatalf("erste Remediation sollte fällig sein")
	}
	if err := st.MarkRemediation(ctx, dev, chk.ID, time.Now()); err != nil {
		t.Fatalf("MarkRemediation: %v", err)
	}
	if st.RemediationDue(ctx, dev, chk.ID, time.Hour) {
		t.Fatalf("innerhalb Cooldown sollte nicht fällig sein")
	}
	if !st.RemediationDue(ctx, dev, chk.ID, time.Nanosecond) {
		t.Fatalf("nach Ablauf sollte wieder fällig sein")
	}
	// Upsert: erneutes Markieren aktualisiert nur (kein Fehler).
	if err := st.MarkRemediation(ctx, dev, chk.ID, time.Now()); err != nil {
		t.Fatalf("MarkRemediation upsert: %v", err)
	}
}

func TestCustomFieldsAndCollector(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()

	// Hierarchie: client -> site -> device.
	cl := &model.Client{ID: store.NewID(), Name: "Acme"}
	if err := st.CreateClient(ctx, cl); err != nil {
		t.Fatalf("CreateClient: %v", err)
	}
	site := &model.Site{ID: store.NewID(), ClientID: cl.ID, Name: "HQ"}
	if err := st.CreateSite(ctx, site); err != nil {
		t.Fatalf("CreateSite: %v", err)
	}
	dev := &model.Device{ID: store.NewID(), Hostname: "pc", OS: "linux"}
	if err := st.CreateDevice(ctx, dev, auth.HashToken("t")); err != nil {
		t.Fatalf("CreateDevice: %v", err)
	}
	if err := st.SetDeviceSite(ctx, dev.ID, &site.ID); err != nil {
		t.Fatalf("SetDeviceSite: %v", err)
	}

	// Definition + manueller Wert (Liste als JSON).
	f := &model.CustomField{ID: store.NewID(), Model: "device", Name: "tags", Type: "list", Options: []string{}, Default: ""}
	if err := st.CreateCustomField(ctx, f); err != nil {
		t.Fatalf("CreateCustomField: %v", err)
	}
	if err := st.SetCustomFieldValue(ctx, f.ID, dev.ID, `["a","b"]`); err != nil {
		t.Fatalf("SetCustomFieldValue: %v", err)
	}
	vals, err := st.CustomFieldValues(ctx, "device", dev.ID)
	if err != nil || len(vals) != 1 || vals[0].Value != `["a","b"]` {
		t.Fatalf("CustomFieldValues: %v / %+v", err, vals)
	}

	// JSON-Collector mit Typ-Inferenz und Auto-Anlage auf allen Ebenen.
	out := `{"status":0,"agent":{"anydeskId":"123456","online":true,"hosts":["x","y"]},"client":{"vertrag":"Gold"},"site":{"vlan":"42"}}`
	if err := st.ApplyCollected(ctx, dev.ID, out); err != nil {
		t.Fatalf("ApplyCollected: %v", err)
	}
	byName := func(mdl, eid string) map[string]model.CustomFieldValue {
		vs, e := st.CustomFieldValues(ctx, mdl, eid)
		if e != nil {
			t.Fatalf("CustomFieldValues(%s): %v", mdl, e)
		}
		m := map[string]model.CustomFieldValue{}
		for _, v := range vs {
			m[v.Field.Name] = v
		}
		return m
	}
	dm := byName("device", dev.ID)
	if dm["anydeskId"].Field.Type != "text" || dm["anydeskId"].Value != "123456" {
		t.Fatalf("anydeskId falsch: %+v", dm["anydeskId"])
	}
	if dm["online"].Field.Type != "checkbox" || dm["online"].Value != "true" {
		t.Fatalf("online falsch: %+v", dm["online"])
	}
	if dm["hosts"].Field.Type != "list" || dm["hosts"].Value != `["x","y"]` {
		t.Fatalf("hosts falsch: %+v", dm["hosts"])
	}
	if byName("client", cl.ID)["vertrag"].Value != "Gold" {
		t.Fatalf("client.vertrag falsch")
	}
	if byName("site", site.ID)["vlan"].Value != "42" {
		t.Fatalf("site.vlan falsch")
	}

	// Platzhalter-Ersetzung (Liste komma-getrennt, Client-/Site-Ebene).
	agent, client, siteM, err := st.FieldMapsForDevice(ctx, dev.ID)
	if err != nil {
		t.Fatalf("FieldMapsForDevice: %v", err)
	}
	script := "id={{agent.anydeskId}} hosts={{agent.hosts}} vertrag={{client.vertrag}} vlan={{site.vlan}}"
	got := store.SubstituteFields(script, agent, client, siteM)
	want := "id=123456 hosts=x,y vertrag=Gold vlan=42"
	if got != want {
		t.Fatalf("Substitution falsch:\n got=%q\nwant=%q", got, want)
	}
}

func TestSubstituteFieldsFilters(t *testing.T) {
	agent := map[string]string{
		"domains": `["a.de","b.de","c.de"]`,
		"name":    "srv1",
	}
	none := map[string]string{}
	cases := map[string]string{
		"{{agent.domains}}":                      "a.de,b.de,c.de",
		"{{ agent.domains | first }}":            "a.de",
		"{{agent.domains|last}}":                 "c.de",
		"{{ agent.domains | nth(1) }}":           "b.de",
		"{{ agent.domains | count }}":            "3",
		`{{ agent.domains | join(" ") }}`:        "a.de b.de c.de",
		"{{ agent.name | upper }}":               "SRV1",
		`{{ agent.fehlt | default("x") }}`:       "x",
		"https://{{agent.domains|first}}/x":      "https://a.de/x",
		"{{agent.domains|first}}-{{agent.name}}": "a.de-srv1",
	}
	for in, want := range cases {
		if got := store.SubstituteFields(in, agent, none, none); got != want {
			t.Errorf("SubstituteFields(%q) = %q, erwartet %q", in, got, want)
		}
	}
}

func TestPruneHistory(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()
	dev := &model.Device{ID: store.NewID(), Hostname: "pc", OS: "linux"}
	if err := st.CreateDevice(ctx, dev, auth.HashToken("t")); err != nil {
		t.Fatalf("CreateDevice: %v", err)
	}
	old := time.Now().Add(-40 * 24 * time.Hour)
	recent := time.Now().Add(-2 * 24 * time.Hour)
	if err := st.SaveTaskResults(ctx, dev.ID, []shared.TaskResult{
		{TaskID: "task-a", ExitCode: 0, Output: "alt", RanAt: old},
		{TaskID: "task-a", ExitCode: 0, Output: "neu", RanAt: recent},
	}); err != nil {
		t.Fatalf("SaveTaskResults: %v", err)
	}
	tasks, _, err := st.PruneHistory(ctx, time.Now().Add(-30*24*time.Hour), time.Now().Add(-60*24*time.Hour))
	if err != nil {
		t.Fatalf("PruneHistory: %v", err)
	}
	if tasks != 1 {
		t.Fatalf("erwartete 1 gelöschten Task-Lauf, bekam %d", tasks)
	}
	res, err := st.TaskResultsFor(ctx, dev.ID, 10)
	if err != nil {
		t.Fatalf("TaskResultsFor: %v", err)
	}
	if len(res) != 1 || res[0].Output != "neu" {
		t.Fatalf("erwartete nur den neuen Lauf, bekam %+v", res)
	}
}

func TestCheckEvents(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()
	dev := &model.Device{ID: store.NewID(), Hostname: "pc", OS: "linux"}
	if err := st.CreateDevice(ctx, dev, auth.HashToken("t")); err != nil {
		t.Fatalf("CreateDevice: %v", err)
	}
	cid := "check-1"
	save := func(status string) []model.CheckEvent {
		ev, err := st.SaveCheckResults(ctx, dev.ID, []shared.CheckResult{{CheckID: cid, Status: status, Output: status}})
		if err != nil {
			t.Fatalf("SaveCheckResults(%s): %v", status, err)
		}
		return ev
	}
	// Erstmeldung gut -> kein Ereignis.
	if ev := save("passing"); len(ev) != 0 {
		t.Fatalf("Erstmeldung passing sollte kein Ereignis erzeugen, bekam %d", len(ev))
	}
	// gut -> warning = Ereignis.
	if ev := save("warning"); len(ev) != 1 || ev[0].OldStatus != "passing" || ev[0].NewStatus != "warning" {
		t.Fatalf("erwartete passing→warning, bekam %+v", ev)
	}
	// gleich bleiben -> kein Ereignis.
	if ev := save("warning"); len(ev) != 0 {
		t.Fatalf("unveränderter Status sollte kein Ereignis erzeugen, bekam %d", len(ev))
	}
	// warning -> passing = Recovery-Ereignis.
	if ev := save("passing"); len(ev) != 1 || ev[0].OldStatus != "warning" || ev[0].NewStatus != "passing" {
		t.Fatalf("erwartete warning→passing (Recovery), bekam %+v", ev)
	}
	hist, err := st.CheckEventsFor(ctx, dev.ID, 10)
	if err != nil {
		t.Fatalf("CheckEventsFor: %v", err)
	}
	if len(hist) != 2 {
		t.Fatalf("erwartete 2 Verlaufseinträge, bekam %d", len(hist))
	}
	// notified markieren.
	if err := st.MarkEventsNotified(ctx, []string{hist[0].ID}, time.Now()); err != nil {
		t.Fatalf("MarkEventsNotified: %v", err)
	}
	hist, _ = st.CheckEventsFor(ctx, dev.ID, 10)
	if !hist[0].Notified || hist[0].NotifiedAt == nil {
		t.Fatalf("jüngstes Ereignis sollte als benachrichtigt markiert sein: %+v", hist[0])
	}
}

func TestLatestTaskResults(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()
	dev := &model.Device{ID: store.NewID(), Hostname: "pc", OS: "linux"}
	if err := st.CreateDevice(ctx, dev, auth.HashToken("t")); err != nil {
		t.Fatalf("CreateDevice: %v", err)
	}
	old := time.Now().Add(-2 * time.Hour)
	mid := time.Now().Add(-1 * time.Hour)
	now := time.Now()
	// task-a: zwei Läufe (alt + neu), task-b: ein Lauf.
	if err := st.SaveTaskResults(ctx, dev.ID, []shared.TaskResult{
		{TaskID: "task-a", ExitCode: 1, Output: "a-alt", RanAt: old},
		{TaskID: "task-a", ExitCode: 0, Output: "a-neu", RanAt: now},
		{TaskID: "task-b", ExitCode: 0, Output: "b", RanAt: mid},
	}); err != nil {
		t.Fatalf("SaveTaskResults: %v", err)
	}
	latest, err := st.LatestTaskResultsFor(ctx, dev.ID)
	if err != nil {
		t.Fatalf("LatestTaskResultsFor: %v", err)
	}
	if len(latest) != 2 {
		t.Fatalf("erwartete 2 Einträge (je Task einer), bekam %d: %+v", len(latest), latest)
	}
	byTask := map[string]model.TaskResult{}
	for _, r := range latest {
		byTask[r.TaskID] = r
	}
	if byTask["task-a"].Output != "a-neu" || byTask["task-a"].ExitCode != 0 {
		t.Fatalf("task-a sollte den neuesten Lauf liefern: %+v", byTask["task-a"])
	}
	// Historie liefert weiterhin alle Läufe.
	all, _ := st.TaskResultsFor(ctx, dev.ID, 10)
	if len(all) != 3 {
		t.Fatalf("Historie sollte 3 Läufe haben, bekam %d", len(all))
	}
}

func TestSoftwareChangeTracking(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()
	dev := &model.Device{ID: store.NewID(), Hostname: "pc", OS: "linux"}
	if err := st.CreateDevice(ctx, dev, auth.HashToken("t")); err != nil {
		t.Fatalf("CreateDevice: %v", err)
	}
	inv := func(sw ...shared.SoftwarePackage) shared.Inventory {
		return shared.Inventory{Hostname: "pc", Software: sw}
	}
	// 1) Erstinventar = Baseline -> keine Events.
	if err := st.UpdateInventory(ctx, dev.ID, inv(
		shared.SoftwarePackage{Name: "Firefox", Version: "120"},
		shared.SoftwarePackage{Name: "7zip", Version: "23"},
	)); err != nil {
		t.Fatalf("UpdateInventory 1: %v", err)
	}
	if ev, _ := st.SoftwareEventsFor(ctx, dev.ID, 10); len(ev) != 0 {
		t.Fatalf("Baseline sollte keine Events erzeugen, bekam %d", len(ev))
	}
	// 2) Firefox aktualisiert, 7zip entfernt, VLC neu.
	if err := st.UpdateInventory(ctx, dev.ID, inv(
		shared.SoftwarePackage{Name: "Firefox", Version: "121"},
		shared.SoftwarePackage{Name: "VLC", Version: "3.0"},
	)); err != nil {
		t.Fatalf("UpdateInventory 2: %v", err)
	}
	ev, _ := st.SoftwareEventsFor(ctx, dev.ID, 10)
	got := map[string]model.SoftwareEvent{}
	for _, e := range ev {
		got[e.Name] = e
	}
	if got["Firefox"].Change != "updated" || got["Firefox"].OldVersion != "120" || got["Firefox"].Version != "121" {
		t.Fatalf("Firefox-Update falsch: %+v", got["Firefox"])
	}
	if got["7zip"].Change != "removed" || got["7zip"].Version != "23" {
		t.Fatalf("7zip-Entfernung falsch: %+v", got["7zip"])
	}
	if got["VLC"].Change != "added" || got["VLC"].Version != "3.0" {
		t.Fatalf("VLC-Installation falsch: %+v", got["VLC"])
	}
	if len(ev) != 3 {
		t.Fatalf("erwartete 3 Events, bekam %d", len(ev))
	}
}

func TestDeviceInMaintenance(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()
	cl := &model.Client{ID: store.NewID(), Name: "Acme"}
	st.CreateClient(ctx, cl)
	site := &model.Site{ID: store.NewID(), ClientID: cl.ID, Name: "HQ"}
	st.CreateSite(ctx, site)
	dev := &model.Device{ID: store.NewID(), Hostname: "pc", OS: "linux"}
	st.CreateDevice(ctx, dev, auth.HashToken("t"))
	st.SetDeviceSite(ctx, dev.ID, &site.ID)
	now := time.Now()

	check := func() bool { in, err := st.DeviceInMaintenance(ctx, dev.ID, now); if err != nil { t.Fatal(err) }; return in }

	if check() {
		t.Fatal("ohne Fenster sollte keine Wartung aktiv sein")
	}
	// aktives Fenster direkt am Gerät
	w := &model.MaintenanceWindow{TargetType: "device", TargetID: dev.ID, StartsAt: now.Add(-time.Hour), EndsAt: now.Add(time.Hour)}
	st.CreateMaintenanceWindow(ctx, w)
	if !check() {
		t.Fatal("Gerät-Fenster sollte aktiv sein")
	}
	st.DeleteMaintenanceWindow(ctx, w.ID)
	// über die Site
	ws := &model.MaintenanceWindow{TargetType: "site", TargetID: site.ID, StartsAt: now.Add(-time.Hour), EndsAt: now.Add(time.Hour)}
	st.CreateMaintenanceWindow(ctx, ws)
	if !check() {
		t.Fatal("Site-Fenster sollte für das Gerät gelten")
	}
	st.DeleteMaintenanceWindow(ctx, ws.ID)
	// über den Client
	wc := &model.MaintenanceWindow{TargetType: "client", TargetID: cl.ID, StartsAt: now.Add(-time.Hour), EndsAt: now.Add(time.Hour)}
	st.CreateMaintenanceWindow(ctx, wc)
	if !check() {
		t.Fatal("Client-Fenster sollte für das Gerät gelten")
	}
	st.DeleteMaintenanceWindow(ctx, wc.ID)
	// abgelaufenes Fenster -> inaktiv
	wp := &model.MaintenanceWindow{TargetType: "device", TargetID: dev.ID, StartsAt: now.Add(-2 * time.Hour), EndsAt: now.Add(-time.Hour)}
	st.CreateMaintenanceWindow(ctx, wp)
	if check() {
		t.Fatal("abgelaufenes Fenster sollte inaktiv sein")
	}
}

func TestDevicesForTarget(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()
	cl := &model.Client{ID: store.NewID(), Name: "Acme"}
	st.CreateClient(ctx, cl)
	site := &model.Site{ID: store.NewID(), ClientID: cl.ID, Name: "HQ"}
	st.CreateSite(ctx, site)
	d1 := &model.Device{ID: store.NewID(), Hostname: "a", OS: "linux"}
	d2 := &model.Device{ID: store.NewID(), Hostname: "b", OS: "linux"}
	st.CreateDevice(ctx, d1, auth.HashToken("t1"))
	st.CreateDevice(ctx, d2, auth.HashToken("t2"))
	st.SetDeviceSite(ctx, d1.ID, &site.ID)
	st.SetDeviceSite(ctx, d2.ID, &site.ID)
	g := &model.Group{ID: store.NewID(), Name: "srv"}
	st.CreateGroup(ctx, g)
	st.SetDeviceGroups(ctx, d1.ID, []string{g.ID})

	count := func(tt, id string) int { ids, err := st.DevicesForTarget(ctx, tt, id); if err != nil { t.Fatal(err) }; return len(ids) }
	if count("device", d1.ID) != 1 { t.Fatal("device") }
	if count("site", site.ID) != 2 { t.Fatal("site") }
	if count("client", cl.ID) != 2 { t.Fatal("client") }
	if count("group", g.ID) != 1 { t.Fatal("group") }
	if count("all", "") != 2 { t.Fatal("all") }
}

func TestSearchDevices(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()
	d1 := &model.Device{ID: store.NewID(), Hostname: "web01", OS: "linux"}
	d2 := &model.Device{ID: store.NewID(), Hostname: "pc02", OS: "windows"}
	st.CreateDevice(ctx, d1, auth.HashToken("t1"))
	st.CreateDevice(ctx, d2, auth.HashToken("t2"))
	// d1 hat fail2ban installiert, d2 nicht.
	st.UpdateInventory(ctx, d1.ID, shared.Inventory{Hostname: "web01", Software: []shared.SoftwarePackage{{Name: "fail2ban", Version: "1.0"}}})
	st.UpdateInventory(ctx, d2.ID, shared.Inventory{Hostname: "pc02", Software: []shared.SoftwarePackage{{Name: "7zip"}}})

	res, err := st.SearchDevices(ctx, "fail2ban")
	if err != nil {
		t.Fatalf("SearchDevices: %v", err)
	}
	if len(res) != 1 || res[0].Hostname != "web01" {
		t.Fatalf("Software-Suche fehlgeschlagen: %+v", res)
	}
	// Hostname-Treffer
	if res, _ := st.SearchDevices(ctx, "pc0"); len(res) != 1 || res[0].Hostname != "pc02" {
		t.Fatalf("Hostname-Suche fehlgeschlagen: %+v", res)
	}
	// Custom Field Treffer
	f := &model.CustomField{ID: store.NewID(), Model: "device", Name: "anydesk", Type: "text"}
	st.CreateCustomField(ctx, f)
	st.SetCustomFieldValue(ctx, f.ID, d2.ID, "123456789")
	if res, _ := st.SearchDevices(ctx, "123456"); len(res) != 1 || res[0].Hostname != "pc02" {
		t.Fatalf("Custom-Field-Suche fehlgeschlagen: %+v", res)
	}
}

func TestReportSchedules(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()
	// Kanal anlegen (FK-frei, aber für Join)
	ch := model.AlertChannel{ID: store.NewID(), Type: "email", Name: "Mail", Enabled: true, Config: map[string]string{"host": "x"}}
	if err := st.SaveAlertChannel(ctx, ch); err != nil {
		t.Fatalf("SaveAlertChannel: %v", err)
	}
	rs := &model.ReportSchedule{Title: "Wöchentlich", Frequency: "weekly", ChannelID: ch.ID}
	if err := st.CreateReportSchedule(ctx, rs); err != nil {
		t.Fatalf("CreateReportSchedule: %v", err)
	}
	list, _ := st.ListReportSchedules(ctx)
	if len(list) != 1 || list[0].ChannelName != "Mail" {
		t.Fatalf("Liste falsch: %+v", list)
	}
	// Neu (last_run nil) -> fällig
	due, _ := st.DueReportSchedules(ctx, time.Now())
	if len(due) != 1 {
		t.Fatalf("sollte fällig sein, bekam %d", len(due))
	}
	// gerade gelaufen -> nicht mehr fällig
	st.MarkReportRun(ctx, rs.ID, time.Now())
	due, _ = st.DueReportSchedules(ctx, time.Now())
	if len(due) != 0 {
		t.Fatalf("sollte nicht fällig sein, bekam %d", len(due))
	}
	// vor 8 Tagen gelaufen -> weekly wieder fällig
	st.MarkReportRun(ctx, rs.ID, time.Now().Add(-8*24*time.Hour))
	due, _ = st.DueReportSchedules(ctx, time.Now())
	if len(due) != 1 {
		t.Fatalf("nach 8 Tagen wieder fällig, bekam %d", len(due))
	}
}
