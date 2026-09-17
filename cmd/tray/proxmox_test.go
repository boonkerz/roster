package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/boonkerz/roster/internal/shared"
)

func TestBackupBadge(t *testing.T) {
	now := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)
	ago := func(h int) *time.Time { v := now.Add(-time.Duration(h) * time.Hour); return &v }
	yes, no := true, false
	cases := []struct {
		name  string
		g     pveGuest
		label string
		color [3]uint8
	}{
		{"fehlgeschlagen schlägt Alter", pveGuest{BackupAt: ago(1), BackupTaskStatus: "failed"}, "Backup fehlgeschlagen", colRed},
		{"kein Backup", pveGuest{}, "kein Backup", colRed},
		{"veraltet", pveGuest{BackupAt: ago(30), BackupJob: &yes}, "Backup vor 30 h", colAmber},
		{"frisch, aber kein Job", pveGuest{BackupAt: ago(2), BackupJob: &no}, "kein Backup-Job", colAmber},
		{"alles gut", pveGuest{BackupAt: ago(5), BackupJob: &yes, BackupTaskStatus: "ok"}, "Backup vor 5 h", colGreen},
	}
	for _, c := range cases {
		// humanAge rechnet mit time.Now – daher Alter relativ zu jetzt neu setzen.
		if c.g.BackupAt != nil {
			v := time.Now().Add(-now.Sub(*c.g.BackupAt))
			c.g.BackupAt = &v
		}
		b := backupBadge(c.g, time.Now())
		if b.label != c.label || b.color != c.color {
			t.Errorf("%s: %q %v, erwartet %q %v", c.name, b.label, b.color, c.label, c.color)
		}
	}
}

func TestParseGuestHit(t *testing.T) {
	action, dev, vmid, ok := parseGuestHit("pve:shutdown:abc123:101")
	if !ok || action != "shutdown" || dev != "abc123" || vmid != 101 {
		t.Errorf("got %q %q %d %v", action, dev, vmid, ok)
	}
	for _, bad := range []string{"pve:start:abc", "term:abc", "pve:start:abc:x"} {
		if _, _, _, ok := parseGuestHit(bad); ok {
			t.Errorf("%q dürfte nicht erkannt werden", bad)
		}
	}
}

// fakeServer nimmt Proxmox-Aktionen an und meldet den Befehl nach einer Abfrage als erledigt.
type fakeServer struct {
	mu      sync.Mutex
	actions []string
	exit    int
	output  string
}

func (f *fakeServer) handler(t *testing.T) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/proxmox-control"):
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			f.mu.Lock()
			f.actions = append(f.actions, body["action"].(string))
			f.mu.Unlock()
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]string{"command_id": "cmd1"})
		case strings.HasSuffix(r.URL.Path, "/commands/cmd1"):
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "done", "exit_code": f.exit, "output": f.output})
		default:
			t.Errorf("unerwarteter Aufruf %s", r.URL.Path)
		}
	})
}

func (f *fakeServer) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.actions)
}

func testApp(t *testing.T, f *fakeServer) *app {
	srv := httptest.NewServer(f.handler(t))
	t.Cleanup(srv.Close)
	a := newApp(&config{})
	a.cli = newClient(srv.URL, "tok", false)
	a.scale = 1
	a.devices = []device{{ID: "dev1", Hostname: "pve1", ProxmoxVersion: "8.2.4", ProxmoxGuests: []pveGuest{
		{Node: "pve1", VMID: 101, Type: "qemu", Name: "win-srv", Status: "running"},
		{Node: "pve1", VMID: 103, Type: "qemu", Name: "test-vm", Status: "stopped"},
	}}}
	return a
}

// waitIdle wartet, bis keine Gast-Aktion mehr läuft.
func waitIdle(t *testing.T, a *app) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		a.mu.Lock()
		busy := len(a.guestBusy)
		a.mu.Unlock()
		if busy == 0 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("Aktion wurde nicht fertig")
}

func TestStopNeedsSecondClick(t *testing.T) {
	f := &fakeServer{output: "VM 101 gestoppt"}
	a := testApp(t, f)

	a.clickGuest("pve:stop:dev1:101")
	if f.count() != 0 || a.armed != "pve:stop:dev1:101" {
		t.Fatalf("erster Klick darf nur scharf schalten (Aufrufe %d, armed %q)", f.count(), a.armed)
	}
	if !strings.Contains(a.status, "nochmal klicken") {
		t.Errorf("Hinweis fehlt: %q", a.status)
	}

	a.clickGuest("pve:stop:dev1:101")
	time.Sleep(50 * time.Millisecond)
	waitIdle(t, a)
	if f.count() != 1 || f.actions[0] != "stop" {
		t.Fatalf("zweiter Klick muss auslösen: %v", f.actions)
	}
	a.mu.Lock()
	status, bad := a.status, a.statusBad
	a.mu.Unlock()
	if bad || !strings.Contains(status, "VM 101 gestoppt") {
		t.Errorf("Ergebnis nicht gemeldet: %q bad=%v", status, bad)
	}

	// Verfallene Bestätigung schaltet neu scharf statt auszulösen.
	a.clickGuest("pve:reboot:dev1:101")
	a.armedAt = time.Now().Add(-2 * confirmWindow)
	a.clickGuest("pve:reboot:dev1:101")
	if f.count() != 1 {
		t.Errorf("verfallene Bestätigung hat ausgelöst: %v", f.actions)
	}
}

func TestStartRunsImmediatelyAndReportsErrors(t *testing.T) {
	f := &fakeServer{exit: 1, output: "Warnung\nstart failed: storage offline"}
	a := testApp(t, f)
	a.clickGuest("pve:start:dev1:103")
	time.Sleep(50 * time.Millisecond)
	waitIdle(t, a)
	if f.count() != 1 || f.actions[0] != "start" {
		t.Fatalf("Start muss sofort laufen: %v", f.actions)
	}
	a.mu.Lock()
	status, bad := a.status, a.statusBad
	a.mu.Unlock()
	if !bad || !strings.HasSuffix(status, "start failed: storage offline") {
		t.Errorf("Fehler nicht (mit letzter Zeile) gemeldet: %q bad=%v", status, bad)
	}
}

func TestListItemsSearchFindsGuests(t *testing.T) {
	a := testApp(t, &fakeServer{})
	rows := a.devices

	if items := a.listItems(rows); len(items) != 1 {
		t.Fatalf("zugeklappt nur der Host erwartet, got %d", len(items))
	}
	a.expanded["dev1"] = true
	if items := a.listItems(rows); len(items) != 3 {
		t.Fatalf("aufgeklappt Host + 2 Gäste erwartet, got %d", len(items))
	}

	// Suche trifft nur einen Gast: der erscheint ohne Aufklappen, der andere nicht.
	delete(a.expanded, "dev1")
	a.filter = "test-vm"
	items := a.listItems(a.filtered())
	if len(items) != 2 || items[1].guest == nil || items[1].guest.VMID != 103 {
		t.Fatalf("Suche nach Gast: %+v", items)
	}

	// Suche trifft den Host selbst: Gäste bleiben zugeklappt.
	a.filter = "pve1"
	if items := a.listItems(a.filtered()); len(items) != 1 {
		t.Fatalf("Suche nach Host: %d Zeilen", len(items))
	}
}

func TestTempBadge(t *testing.T) {
	if _, ok := tempBadge(nil); ok {
		t.Error("ohne Sensoren keine Plakette")
	}
	normal := []shared.Temperature{
		{Label: "ACPI-Zone", Class: "board", Celsius: 71},
		{Label: "CPU Package 0", Class: "cpu", Celsius: 58.4, High: 100, Critical: 100},
		{Label: "NVMe Composite", Class: "disk", Celsius: 40, High: 70, Critical: 85},
	}
	if b, _ := tempBadge(normal); b.label != "58 °C" || b.color != colLine {
		t.Errorf("Normalfall zeigt die CPU neutral: %q %v", b.label, b.color)
	}
	hotDisk := append(append([]shared.Temperature(nil), normal...), shared.Temperature{Label: "NVMe 2", Class: "disk", Celsius: 88, High: 70, Critical: 85})
	if b, _ := tempBadge(hotDisk); b.label != "NVMe 2 88 °C" || b.color != colRed {
		t.Errorf("kritische Platte muss vorgehen: %q %v", b.label, b.color)
	}
	warmCPU := []shared.Temperature{{Label: "CPU Package 0", Class: "cpu", Celsius: 93, High: 100, Critical: 100}}
	if b, _ := tempBadge(warmCPU); b.label != "CPU 93 °C" || b.color != colAmber {
		t.Errorf("CPU im Warnbereich: %q %v", b.label, b.color)
	}
	noCPU := []shared.Temperature{{Label: "ACPI-Zone", Class: "board", Celsius: 44}, {Label: "WLAN", Class: "other", Celsius: 51}}
	if b, _ := tempBadge(noCPU); b.label != "51 °C" {
		t.Errorf("ohne CPU der heißeste Sensor: %q", b.label)
	}
}

func TestFanBadge(t *testing.T) {
	if _, ok := fanBadge([]shared.Fan{{Label: "CPU Fan", RPM: 1200}}); ok {
		t.Error("laufende Lüfter brauchen keine Plakette")
	}
	if b, ok := fanBadge([]shared.Fan{{Label: "CPU Fan", RPM: 1200}, {Label: "Lüfter 3", RPM: 0, Min: 300}}); !ok || b.label != "Lüfter steht" || b.color != colRed {
		t.Errorf("ein stehender Lüfter: %q %v", b.label, b.color)
	}
	if b, _ := fanBadge([]shared.Fan{{RPM: 0}, {RPM: 500, Alarm: true}}); b.label != "2 Lüfter gestört" {
		t.Errorf("mehrere: %q", b.label)
	}
}
