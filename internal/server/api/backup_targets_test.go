package api

import (
	"fmt"
	"testing"

	"github.com/boonkerz/roster/internal/server/model"
)

// Ziel je Gast: der Eintrag hat einen Vorgabe-Speicher, einzelne Gäste dürfen abweichen.
func TestProxmoxBackupGuestsTargets(t *testing.T) {
	device := &model.Device{ProxmoxGuests: []model.ProxmoxGuest{
		{Node: "pve", VMID: 107, Type: "lxc"},
		{Node: "pve", VMID: 110, Type: "qemu"},
		{Node: "pve", VMID: 121, Type: "qemu"},
		{Node: "pve", VMID: 900, Type: "qemu", Template: true},
	}}
	b := model.PolicyBackup{Type: "proxmox", Config: map[string]any{
		"all":     true,
		"storage": "backup-pi_1",
		"targets": map[string]any{
			"110": "backup-pi_2",
			"121": "backup-pi_1", // gleich der Vorgabe → kein eigenes Feld
			"999": "backup-pi_2", // Gast existiert nicht
			"abc": "unsinn",      // kaputter Schlüssel
		},
	}}
	got := proxmoxBackupGuests(b, device)
	if len(got) != 3 {
		t.Fatalf("Vorlagen müssen raus, erwartet 3 Gäste, bekam %d: %+v", len(got), got)
	}
	want := map[int]string{107: "", 110: "backup-pi_2", 121: ""}
	for _, g := range got {
		vmid := g["vmid"].(int)
		storage, _ := g["storage"].(string)
		if storage != want[vmid] {
			t.Errorf("VMID %d: Ziel %q, erwartet %q", vmid, storage, want[vmid])
		}
	}
}

func TestGuestTargets(t *testing.T) {
	got := guestTargets(map[string]any{"targets": map[string]any{
		"107": "backup-pi_1", " 110 ": " backup-pi_2 ", "111": "", "x": "y",
	}})
	if fmt.Sprint(got) != "map[107:backup-pi_1 110:backup-pi_2]" {
		t.Errorf("unerwartet: %v", got)
	}
	if len(guestTargets(map[string]any{})) != 0 {
		t.Error("ohne targets muss die Zuordnung leer sein (Verhalten wie vorher)")
	}
}

// Ein alter Agent ignoriert guests[].storage still – der Lauf darf deshalb gar nicht
// erst starten, sonst landet alles unbemerkt auf dem falschen Speicher.
func TestAgentSupportsVersion(t *testing.T) {
	cases := []struct {
		version, min string
		want         bool
	}{
		{"0.15.0", minAgentVersionTargets, false},
		{"0.16.0", minAgentVersionTargets, true},
		{"0.16.1", minAgentVersionTargets, true},
		{"1.0.0", minAgentVersionTargets, true},
		{"dev", minAgentVersionTargets, true},
		{"", minAgentVersionTargets, true},
		{"0.14.9", minAgentVersionBackup, false},
		{"0.15.0", minAgentVersionBackup, true},
	}
	for _, c := range cases {
		if got := agentSupportsVersion(c.version, c.min); got != c.want {
			t.Errorf("agentSupportsVersion(%q, %q) = %v, erwartet %v", c.version, c.min, got, c.want)
		}
	}
}
