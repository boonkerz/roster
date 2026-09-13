package policy

import (
	"strings"
	"testing"
	"time"

	"github.com/boonkerz/roster/internal/shared"
)

func TestEvalProxmoxBackup(t *testing.T) {
	now := time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)
	ago := func(h int) *time.Time { t := now.Add(-time.Duration(h) * time.Hour); return &t }
	yes, no := true, false
	guests := []shared.ProxmoxGuest{
		{VMID: 100, Name: "dns", Status: "running", BackupAt: ago(10), BackupTaskStatus: "ok", BackupJob: &yes},
		{VMID: 101, Name: "web", Status: "running", BackupAt: ago(50), BackupTaskStatus: "ok", BackupJob: &yes},
		{VMID: 102, Name: "db", Status: "running", BackupAt: ago(10), BackupTaskStatus: "failed", BackupTaskMsg: "no space left", BackupJob: &yes},
		{VMID: 103, Name: "neu", Status: "stopped", BackupJob: &no},
		{VMID: 104, Name: "vergessen", Status: "running", BackupAt: ago(5), BackupJob: &no},
		{VMID: 9000, Name: "tpl", Status: "stopped", Template: true},
	}
	spec := func(cfg map[string]any) shared.CheckSpec { return shared.CheckSpec{ID: "c1", Config: cfg} }

	r := evalProxmoxBackup(spec(map[string]any{}), guests, now)
	if r.Status != "failing" || r.Value != 4 {
		t.Fatalf("Standard: %s %v – %s", r.Status, r.Value, r.Output)
	}
	for _, want := range []string{"101 web (letztes Backup vor 2 Tagen)", "102 db (letzter Lauf fehlgeschlagen: no space left)", "103 neu (kein Backup)", "104 vergessen (in keinem Backup-Job)"} {
		if !strings.Contains(r.Output, want) {
			t.Errorf("Ausgabe ohne %q: %s", want, r.Output)
		}
	}
	if strings.Contains(r.Output, "tpl") {
		t.Error("Vorlagen dürfen standardmäßig nicht geprüft werden")
	}

	// Nur ausgewählte Gäste, großzügigeres Alter → grün.
	r = evalProxmoxBackup(spec(map[string]any{"vmids": "100, 101", "max_age_hours": float64(72)}), guests, now)
	if r.Status != "passing" || !strings.Contains(r.Output, "2 Gäste gesichert, ältestes Backup vor 2 Tagen") {
		t.Errorf("Auswahl: %s – %s", r.Status, r.Output)
	}

	// Ausschluss + gestoppte ignorieren + Job nicht verlangen.
	r = evalProxmoxBackup(spec(map[string]any{
		"exclude_vmids": "101;102", "include_stopped": false, "require_job": false,
	}), guests, now)
	if r.Status != "passing" {
		t.Errorf("Ausschluss: %s – %s", r.Status, r.Output)
	}

	// Nichts passt → unbekannt statt grün.
	r = evalProxmoxBackup(spec(map[string]any{"vmids": "555"}), guests, now)
	if r.Status != "unknown" {
		t.Errorf("leere Auswahl: %s – %s", r.Status, r.Output)
	}
}
