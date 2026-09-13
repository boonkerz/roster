package collect

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/boonkerz/roster/internal/shared"
)

// Auszug aus `pvesh get /cluster/resources --type vm --output-format json` (PVE 8).
const pveVMResources = `[
 {"cpu":0.0123,"disk":0,"id":"qemu/101","maxcpu":4,"maxdisk":34359738368,"maxmem":8589934592,"mem":2147483648,"name":"win-srv","node":"pve1","status":"running","template":0,"type":"qemu","uptime":86400,"vmid":101},
 {"cpu":0,"disk":0,"id":"lxc/100","maxcpu":2,"maxdisk":8589934592,"maxmem":1073741824,"mem":0,"name":"dns","node":"pve1","status":"stopped","template":0,"type":"lxc","uptime":0,"vmid":100},
 {"id":"qemu/9000","maxcpu":2,"name":"debian-tpl","node":"pve2","status":"stopped","template":1,"type":"qemu","vmid":9000}
]`

// Auszug aus `pvesh get /cluster/resources --type storage --output-format json`.
const pveStorageResources = `[
 {"content":"iso,vztmpl,backup","id":"storage/pve1/local","node":"pve1","plugintype":"dir","shared":0,"status":"available","storage":"local","type":"storage"},
 {"content":"images,rootdir","id":"storage/pve1/local-lvm","node":"pve1","shared":0,"status":"available","storage":"local-lvm","type":"storage"},
 {"content":"backup","id":"storage/pve1/pbs","node":"pve1","plugintype":"pbs","shared":1,"status":"available","storage":"pbs","type":"storage"},
 {"content":"backup","id":"storage/pve2/pbs","node":"pve2","plugintype":"pbs","shared":1,"status":"available","storage":"pbs","type":"storage"},
 {"content":"backup","id":"storage/pve2/nfs-off","node":"pve2","shared":1,"status":"unknown","storage":"nfs-off","type":"storage"}
]`

func TestParseGuests(t *testing.T) {
	var res []pveResource
	if err := json.Unmarshal([]byte(pveVMResources), &res); err != nil {
		t.Fatal(err)
	}
	g := parseGuests(res)
	if len(g) != 3 {
		t.Fatalf("%d Gäste, erwartet 3", len(g))
	}
	if g[0].VMID != 100 || g[1].VMID != 101 || g[2].VMID != 9000 {
		t.Errorf("nicht nach VMID sortiert: %d %d %d", g[0].VMID, g[1].VMID, g[2].VMID)
	}
	if g[1].Type != "qemu" || g[1].Status != "running" || g[1].CPUs != 4 || g[1].Mem != 2147483648 || g[1].Uptime != 86400 {
		t.Errorf("laufende VM falsch übernommen: %+v", g[1])
	}
	if g[0].Type != "lxc" || g[0].Mem != 0 || g[0].MaxMem != 1073741824 {
		t.Errorf("gestoppter Container falsch übernommen: %+v", g[0])
	}
	if !g[2].Template {
		t.Error("Vorlage nicht erkannt")
	}
}

func TestBackupStorages(t *testing.T) {
	var res []pveResource
	if err := json.Unmarshal([]byte(pveStorageResources), &res); err != nil {
		t.Fatal(err)
	}
	got := backupStorages(res)
	want := [][2]string{{"pve1", "local"}, {"pve1", "pbs"}}
	if len(got) != len(want) {
		t.Fatalf("got %v, erwartet %v (geteilt nur einmal, nicht verfügbar raus)", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("%d: %v, erwartet %v", i, got[i], want[i])
		}
	}
}

func TestApplyBackupContents(t *testing.T) {
	state := map[int]*pveBackupState{}
	seen := map[string]bool{}
	applyBackupContents(state, "local", []pveContent{
		{VolID: "local:backup/vzdump-qemu-101-2026_09_10-01_00_02.vma.zst", VMID: 101, CTime: 1757466002, Size: 100},
		{VolID: "local:backup/vzdump-qemu-101-2026_09_11-01_00_02.vma.zst", VMID: 101, CTime: 1757552402, Size: 200},
	}, seen)
	applyBackupContents(state, "pbs", []pveContent{
		{VolID: "pbs:backup/vm/101/2026-09-12T01:00:00Z", VMID: 101, CTime: 1757638800, Size: 300},
		{VolID: "pbs:backup/vm/101/2026-09-12T01:00:00Z", VMID: 101, CTime: 1757638800, Size: 300}, // doppelt gemeldet
	}, seen)
	st := state[101]
	if st == nil || st.count != 3 {
		t.Fatalf("count = %+v, erwartet 3 (Duplikat zählt einmal)", st)
	}
	if st.storage != "pbs" || st.size != 300 || st.at.Unix() != 1757638800 {
		t.Errorf("neuestes Backup falsch: %+v", st)
	}
}

// Auszug aus einem echten vzdump-Jobprotokoll (gekürzt).
var vzdumpLog = []string{
	"INFO: starting new backup job: vzdump 100 101 102 --mode snapshot --storage pbs --compress zstd",
	"INFO: Starting Backup of VM 100 (lxc)",
	"INFO: Backup started at 2026-09-12 01:00:02",
	"INFO: Finished Backup of VM 100 (00:00:41)",
	"INFO: Starting Backup of VM 101 (qemu)",
	"ERROR: Backup of VM 101 failed - unable to find VM '101'",
	"INFO: Starting Backup of VM 102 (qemu)",
	"INFO: Backup job finished with errors",
}

func TestParseVzdumpLog(t *testing.T) {
	r := parseVzdumpLog(vzdumpLog, "job errors")
	if !r[100].ok {
		t.Errorf("100 sollte ok sein: %+v", r[100])
	}
	if r[101].ok || r[101].msg != "unable to find VM '101'" {
		t.Errorf("101 sollte mit Meldung fehlschlagen: %+v", r[101])
	}
	if res, ok := r[102]; !ok || res.ok || res.msg != "Lauf abgebrochen: job errors" {
		t.Errorf("102 gestartet ohne Ende muss als abgebrochen gelten: %+v", r[102])
	}
	// Bei OK-Lauf wird ein fehlendes „Finished" nicht zum Fehler erklärt.
	if _, ok := parseVzdumpLog([]string{"INFO: Starting Backup of VM 105 (qemu)"}, "OK")[105]; ok {
		t.Error("ohne Fehlerstatus darf kein Ergebnis erfunden werden")
	}
}

func TestApplyTaskResultNewestWins(t *testing.T) {
	state := map[int]*pveBackupState{}
	newer := time.Date(2026, 9, 12, 1, 0, 0, 0, time.UTC)
	older := newer.Add(-24 * time.Hour)
	applyTaskResult(state, 101, newer, vzdumpResult{ok: true})
	applyTaskResult(state, 101, older, vzdumpResult{ok: false, msg: "alt"})
	if st := state[101]; st.taskOK == nil || !*st.taskOK || !st.taskAt.Equal(newer) {
		t.Errorf("älterer Lauf hat neueren überschrieben: %+v", st)
	}
}

func TestMergeBackupState(t *testing.T) {
	at := time.Date(2026, 9, 12, 1, 0, 0, 0, time.UTC)
	no := false
	st := &pveBackupState{at: at, size: 42, storage: "pbs", count: 7, taskOK: &no, taskAt: at, taskMsg: "kaputt"}
	var g shared.ProxmoxGuest
	mergeBackupState(&g, st)
	if g.BackupAt == nil || !g.BackupAt.Equal(at) || g.BackupSize != 42 || g.BackupStorage != "pbs" || g.BackupCount != 7 {
		t.Errorf("Backup nicht übernommen: %+v", g)
	}
	if g.BackupTaskStatus != "failed" || g.BackupTaskMsg != "kaputt" {
		t.Errorf("Laufstatus nicht übernommen: %+v", g)
	}
	var empty shared.ProxmoxGuest
	mergeBackupState(&empty, nil)
	if empty.BackupAt != nil || empty.BackupTaskStatus != "" {
		t.Error("ohne Stand darf nichts gesetzt werden")
	}
}

func TestProxmoxControlValidation(t *testing.T) {
	if ProxmoxAvailable() {
		t.Skip("echter Proxmox-Host – Validierung wird hier nicht ohne Wirkung getestet")
	}
	if code, _ := ProxmoxControl(t.Context(), "pve1", "qemu", 101, "start"); code == 0 {
		t.Error("ohne pvesh darf keine Aktion als erfolgreich gelten")
	}
}
