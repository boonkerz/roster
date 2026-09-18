package collect

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/boonkerz/roster/internal/shared"
)

// Proxmox-VE-Inventar über pvesh – die lokale API-CLI jedes PVE-Nodes. Sie läuft als
// root ohne Token und arbeitet clusterweit (Aufrufe für andere Nodes proxyt PVE
// selbst), ein Agent auf einem Node reicht also für den ganzen Cluster.
//
// Kosten: pvesh startet jedes Mal einen Perl-Prozess (~0,5 s). Die Gastliste wird bei
// jedem Checkin frisch geholt (Status nach Start/Stop soll sofort stimmen), der
// Backup-Status ändert sich selten und wird zwischengespeichert; ausgewertete
// vzdump-Protokolle ändern sich nie und werden je Task (UPID) gemerkt.

const (
	pveBackupTTL     = 10 * time.Minute
	pveTaskLookback  = 14 * 24 * time.Hour // ältere vzdump-Läufe interessieren nicht
	pveMaxLogFetches = 20                  // Protokolle pro Auffrischung (erste Runde)
	pveLogCacheMax   = 500
)

// ProxmoxAvailable meldet, ob dies ein Proxmox-VE-Host ist (pvesh im PATH).
func ProxmoxAvailable() bool {
	_, err := exec.LookPath("pvesh")
	return err == nil
}

// pveshGet ruft `pvesh get <path> --output-format json [args]` auf und dekodiert das
// Ergebnis nach out.
func pveshGet(ctx context.Context, out any, path string, args ...string) error {
	cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	full := append([]string{"get", path, "--output-format", "json"}, args...)
	raw, err := exec.CommandContext(cctx, "pvesh", full...).Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok && len(ee.Stderr) > 0 {
			return fmt.Errorf("pvesh %s: %s", path, strings.TrimSpace(string(ee.Stderr)))
		}
		return fmt.Errorf("pvesh %s: %w", path, err)
	}
	return json.Unmarshal(raw, out)
}

// pveResource spiegelt einen Eintrag aus /cluster/resources (type vm bzw. storage).
type pveResource struct {
	Type     string  `json:"type"` // qemu | lxc | storage
	Node     string  `json:"node"`
	VMID     int     `json:"vmid"`
	Name     string  `json:"name"`
	Status   string  `json:"status"`
	Template int     `json:"template"`
	MaxCPU   float64 `json:"maxcpu"`
	CPU      float64 `json:"cpu"`
	Mem      uint64  `json:"mem"`
	MaxMem   uint64  `json:"maxmem"`
	MaxDisk  uint64  `json:"maxdisk"`
	Uptime   uint64  `json:"uptime"`
	Storage  string  `json:"storage"`
	Content  string  `json:"content"` // Speicher: kommagetrennt, z. B. "backup,iso"
	Shared   int     `json:"shared"`
}

// pveContent ist ein Eintrag aus /nodes/{node}/storage/{storage}/content.
type pveContent struct {
	VolID string `json:"volid"`
	VMID  int    `json:"vmid"`
	CTime int64  `json:"ctime"`
	Size  uint64 `json:"size"`
}

// pveTask ist ein Eintrag aus /nodes/{node}/tasks.
type pveTask struct {
	UPID      string `json:"upid"`
	Node      string `json:"node"`
	ID        string `json:"id"` // VMID bei Einzel-Backups, leer bei Job-Läufen
	StartTime int64  `json:"starttime"`
	EndTime   int64  `json:"endtime"`
	Status    string `json:"status"` // "OK" oder Fehlertext
}

// pveBackupState ist der zusammengeführte Backup-Stand eines Gasts.
type pveBackupState struct {
	at      time.Time
	size    uint64
	storage string
	count   int
	taskOK  *bool
	taskAt  time.Time
	taskMsg string
}

// pveBackups ist der zwischengespeicherte Backup-Stand des Clusters.
type pveBackups struct {
	version   string
	state     map[int]*pveBackupState
	jobsKnown bool         // not-backed-up abrufbar (PVE ≥ 7.1)?
	notInJob  map[int]bool // Gäste, die in keinem Backup-Job stecken
}

// parseGuests filtert qemu/lxc aus /cluster/resources und sortiert nach VMID.
func parseGuests(res []pveResource) []shared.ProxmoxGuest {
	out := make([]shared.ProxmoxGuest, 0, len(res))
	for _, r := range res {
		if r.Type != "qemu" && r.Type != "lxc" {
			continue
		}
		g := shared.ProxmoxGuest{
			Node: r.Node, VMID: r.VMID, Type: r.Type, Name: r.Name, Status: r.Status,
			Template: r.Template == 1, CPUs: r.MaxCPU, MaxMem: r.MaxMem, MaxDisk: r.MaxDisk,
		}
		if r.Status == "running" {
			g.CPU, g.Mem, g.Uptime = r.CPU, r.Mem, r.Uptime
		}
		out = append(out, g)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].VMID < out[j].VMID })
	return out
}

// backupStorages liefert (node, storage)-Paare mit Backup-Inhalt. Geteilte Speicher
// (NFS, PBS …) tauchen je Node auf, werden aber nur einmal abgefragt.
func backupStorages(res []pveResource) [][2]string {
	var out [][2]string
	seenShared := map[string]bool{}
	for _, r := range res {
		if r.Type != "storage" || r.Status != "available" || !hasContent(r.Content, "backup") {
			continue
		}
		if r.Shared == 1 {
			if seenShared[r.Storage] {
				continue
			}
			seenShared[r.Storage] = true
		}
		out = append(out, [2]string{r.Node, r.Storage})
	}
	return out
}

func hasContent(csv, want string) bool {
	for _, c := range strings.Split(csv, ",") {
		if strings.TrimSpace(c) == want {
			return true
		}
	}
	return false
}

// applyBackupContents trägt die vorhandenen Backups (neuestes je VMID) ein.
// Doppelte Volumes (gleicher Speicher über mehrere Nodes gemeldet) zählen einmal.
func applyBackupContents(state map[int]*pveBackupState, storage string, items []pveContent, seen map[string]bool) {
	for _, it := range items {
		if it.VMID <= 0 || seen[it.VolID] {
			continue
		}
		seen[it.VolID] = true
		st := stateFor(state, it.VMID)
		st.count++
		at := time.Unix(it.CTime, 0).UTC()
		if at.After(st.at) {
			st.at, st.size, st.storage = at, it.Size, storage
		}
	}
}

func stateFor(state map[int]*pveBackupState, vmid int) *pveBackupState {
	st := state[vmid]
	if st == nil {
		st = &pveBackupState{}
		state[vmid] = st
	}
	return st
}

var (
	reVzdumpStart  = regexp.MustCompile(`Starting Backup of VM (\d+)`)
	reVzdumpDone   = regexp.MustCompile(`Finished Backup of VM (\d+)`)
	reVzdumpFailed = regexp.MustCompile(`Backup of VM (\d+) failed - (.*)`)
)

// vzdumpResult ist das Ergebnis eines Gasts innerhalb eines vzdump-Laufs.
type vzdumpResult struct {
	ok  bool
	msg string
}

// parseVzdumpLog wertet das Protokoll eines vzdump-Laufs aus: je VMID erfolgreich
// oder fehlgeschlagen. Gestartet, aber weder beendet noch als Fehler gemeldet
// (Lauf abgebrochen) zählt als fehlgeschlagen, sofern der Lauf nicht OK war.
func parseVzdumpLog(lines []string, taskStatus string) map[int]vzdumpResult {
	out := map[int]vzdumpResult{}
	started := map[int]bool{}
	for _, l := range lines {
		if m := reVzdumpFailed.FindStringSubmatch(l); m != nil {
			id, _ := strconv.Atoi(m[1])
			out[id] = vzdumpResult{ok: false, msg: strings.TrimSpace(m[2])}
			continue
		}
		if m := reVzdumpDone.FindStringSubmatch(l); m != nil {
			id, _ := strconv.Atoi(m[1])
			out[id] = vzdumpResult{ok: true}
			continue
		}
		if m := reVzdumpStart.FindStringSubmatch(l); m != nil {
			id, _ := strconv.Atoi(m[1])
			started[id] = true
		}
	}
	for id := range started {
		if _, done := out[id]; !done && taskStatus != "OK" {
			msg := "Lauf abgebrochen"
			if taskStatus != "" {
				msg += ": " + taskStatus
			}
			out[id] = vzdumpResult{ok: false, msg: msg}
		}
	}
	return out
}

// applyTaskResult trägt ein Laufergebnis ein, sofern für den Gast noch kein neueres steht.
func applyTaskResult(state map[int]*pveBackupState, vmid int, at time.Time, r vzdumpResult) {
	st := stateFor(state, vmid)
	if st.taskOK != nil && !at.After(st.taskAt) {
		return
	}
	ok := r.ok
	st.taskOK, st.taskAt, st.taskMsg = &ok, at, r.msg
}

// Zwischenspeicher für den Backup-Stand und bereits ausgewertete Protokolle.
var pveCache = struct {
	sync.Mutex
	at      time.Time
	backups *pveBackups
	logs    map[string]map[int]vzdumpResult // UPID -> Ergebnis
}{logs: map[string]map[int]vzdumpResult{}}

// Proxmox liefert das PVE-Inventar oder nil, wenn dies kein Proxmox-Host ist bzw.
// die Gastliste nicht abrufbar war.
func Proxmox(ctx context.Context) *shared.ProxmoxInfo {
	if !ProxmoxAvailable() {
		return nil
	}
	var res []pveResource
	if err := pveshGet(ctx, &res, "/cluster/resources", "--type", "vm"); err != nil {
		return nil
	}
	guests := parseGuests(res)
	b := proxmoxBackups(ctx)
	for i := range guests {
		g := &guests[i]
		mergeBackupState(g, b.state[g.VMID])
		if b.jobsKnown {
			inJob := !b.notInJob[g.VMID]
			g.BackupJob = &inJob
		}
	}
	return &shared.ProxmoxInfo{Version: b.version, Guests: guests}
}

// mergeBackupState überträgt den Backup-Stand auf einen Gast.
func mergeBackupState(g *shared.ProxmoxGuest, st *pveBackupState) {
	if st == nil {
		return
	}
	if !st.at.IsZero() {
		at := st.at
		g.BackupAt, g.BackupSize, g.BackupStorage = &at, st.size, st.storage
	}
	g.BackupCount = st.count
	if st.taskOK != nil {
		taskAt := st.taskAt
		g.BackupTaskAt, g.BackupTaskMsg = &taskAt, st.taskMsg
		g.BackupTaskStatus = "failed"
		if *st.taskOK {
			g.BackupTaskStatus = "ok"
		}
	}
}

// proxmoxBackups liefert Version und Backup-Stand, bei Bedarf frisch ermittelt.
func proxmoxBackups(ctx context.Context) *pveBackups {
	pveCache.Lock()
	defer pveCache.Unlock()
	if pveCache.backups != nil && time.Since(pveCache.at) < pveBackupTTL {
		return pveCache.backups
	}

	b := &pveBackups{state: map[int]*pveBackupState{}, notInJob: map[int]bool{}}
	var ver struct {
		Version string `json:"version"`
	}
	if err := pveshGet(ctx, &ver, "/version"); err == nil {
		b.version = ver.Version
	}
	state := b.state

	// 1) Vorhandene Backups auf allen Backup-Speichern.
	var storages []pveResource
	if err := pveshGet(ctx, &storages, "/cluster/resources", "--type", "storage"); err == nil {
		seen := map[string]bool{}
		for _, ns := range backupStorages(storages) {
			var items []pveContent
			path := "/nodes/" + ns[0] + "/storage/" + ns[1] + "/content"
			if err := pveshGet(ctx, &items, path, "--content", "backup"); err == nil {
				applyBackupContents(state, ns[1], items, seen)
			}
		}
	}

	// 2) Ergebnisse der letzten vzdump-Läufe je Node.
	nodes := map[string]bool{}
	var nodeRes []pveResource
	if err := pveshGet(ctx, &nodeRes, "/cluster/resources", "--type", "node"); err == nil {
		for _, n := range nodeRes {
			if n.Node != "" && n.Status == "online" {
				nodes[n.Node] = true
			}
		}
	}
	fetched := 0
	cutoff := time.Now().Add(-pveTaskLookback).Unix()
	for node := range nodes {
		var tasks []pveTask
		if err := pveshGet(ctx, &tasks, "/nodes/"+node+"/tasks", "--typefilter", "vzdump", "--limit", "100"); err != nil {
			continue
		}
		sort.Slice(tasks, func(i, j int) bool { return tasks[i].StartTime > tasks[j].StartTime })
		for _, t := range tasks {
			if t.StartTime < cutoff || t.EndTime == 0 {
				continue // zu alt oder läuft noch
			}
			at := time.Unix(t.EndTime, 0).UTC()
			if vmid, err := strconv.Atoi(t.ID); err == nil && vmid > 0 {
				// Einzel-Backup: Taskstatus gilt direkt für diesen Gast.
				r := vzdumpResult{ok: t.Status == "OK"}
				if !r.ok {
					r.msg = t.Status
				}
				applyTaskResult(state, vmid, at, r)
				continue
			}
			// Job-Lauf über mehrere Gäste: Protokoll auswerten (einmal je UPID).
			results, cached := pveCache.logs[t.UPID]
			if !cached {
				if fetched >= pveMaxLogFetches {
					continue
				}
				fetched++
				var lines []struct {
					T string `json:"t"`
				}
				if err := pveshGet(ctx, &lines, "/nodes/"+node+"/tasks/"+t.UPID+"/log", "--limit", "50000"); err != nil {
					continue
				}
				texts := make([]string, len(lines))
				for i, l := range lines {
					texts[i] = l.T
				}
				results = parseVzdumpLog(texts, t.Status)
				if len(pveCache.logs) >= pveLogCacheMax {
					pveCache.logs = map[string]map[int]vzdumpResult{}
				}
				pveCache.logs[t.UPID] = results
			}
			for vmid, r := range results {
				applyTaskResult(state, vmid, at, r)
			}
		}
	}

	// 3) Gäste ohne Backup-Job (PVE ≥ 7.1). Ohne diese Abfrage bleibt es „unbekannt".
	var notBacked []struct {
		VMID int `json:"vmid"`
	}
	if err := pveshGet(ctx, &notBacked, "/cluster/backup-info/not-backed-up"); err == nil {
		b.jobsKnown = true
		for _, n := range notBacked {
			b.notInJob[n.VMID] = true
		}
	}

	pveCache.at, pveCache.backups = time.Now(), b
	return b
}

var pveNodeName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9.-]{0,62}$`)

// ProxmoxControl startet, stoppt, fährt herunter oder rebootet einen Gast. Die
// Aktion läuft als PVE-Task; pvesh wartet auf dessen Ende (Herunterfahren kann
// dauern, daher großzügiger Timeout). Liefert Exit-Code (0=ok) und Ausgabe.
func ProxmoxControl(ctx context.Context, node, gtype string, vmid int, action string) (int, string) {
	if !ProxmoxAvailable() {
		return 1, "dies ist kein Proxmox-VE-Host (pvesh fehlt)"
	}
	switch action {
	case "start", "stop", "shutdown", "reboot":
	default:
		return 1, "unbekannte Aktion: " + action
	}
	if gtype != "qemu" && gtype != "lxc" {
		return 1, "ungültiger Gast-Typ: " + gtype
	}
	if vmid < 100 || !pveNodeName.MatchString(node) {
		return 1, "node und vmid ungültig"
	}
	cctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	path := fmt.Sprintf("/nodes/%s/%s/%d/status/%s", node, gtype, vmid, action)
	out, err := exec.CommandContext(cctx, "pvesh", "create", path).CombinedOutput()
	msg := strings.TrimSpace(string(out))
	if err != nil {
		if msg == "" {
			msg = err.Error()
		}
		return 1, msg
	}
	label := map[string]string{"start": "gestartet", "stop": "gestoppt", "shutdown": "heruntergefahren", "reboot": "neu gestartet"}[action]
	return 0, fmt.Sprintf("%s %d %s", map[string]string{"qemu": "VM", "lxc": "Container"}[gtype], vmid, label)
}

// --- Backups auslösen -------------------------------------------------------

// BackupGuest ist ein zu sichernder Gast (Node aus dem Inventar des Servers).
type BackupGuest struct {
	Node string `json:"node"`
	VMID int    `json:"vmid"`
	Type string `json:"type"`
}

// BackupSpec beschreibt einen Backup-Lauf, wie ihn der Server schickt.
type BackupSpec struct {
	Guests     []BackupGuest `json:"guests"`
	Storage    string        `json:"storage"`
	Mode       string        `json:"mode"`     // snapshot | suspend | stop
	Compress   string        `json:"compress"` // zstd | gzip | lzo | 0
	Notes      string        `json:"notes"`    // notes-template, z. B. {{guestname}}
	MaxMinutes int           `json:"max_minutes"`
}

var (
	pveStorageName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)
	pveModes       = map[string]bool{"snapshot": true, "suspend": true, "stop": true}
	pveCompress    = map[string]bool{"zstd": true, "gzip": true, "lzo": true, "0": true}
	reUPID         = regexp.MustCompile(`UPID:[^\s"']+`)
)

// ProxmoxBackup sichert die angegebenen Gäste per vzdump. Je Node läuft ein
// vzdump-Task; der Agent verfolgt ihn über seine UPID und wertet am Ende das
// Task-Protokoll je Gast aus (dieselbe Auswertung wie beim Backup-Status im Inventar).
// progress meldet Zwischenstände, damit die Oberfläche bei stundenlangen Läufen etwas
// zu zeigen hat. Rückgabe: Exit-Code (0 = alle Gäste ok) und Ausgabe, deren ERSTE Zeile
// die Zusammenfassung ist.
func ProxmoxBackup(ctx context.Context, spec BackupSpec, progress func(string)) (int, string) {
	if !ProxmoxAvailable() {
		return 1, "dies ist kein Proxmox-VE-Host (pvesh fehlt)"
	}
	if spec.Storage != "" && !pveStorageName.MatchString(spec.Storage) {
		return 1, "ungültiger Speichername: " + spec.Storage
	}
	mode := spec.Mode
	if !pveModes[mode] {
		mode = "snapshot"
	}
	compress := spec.Compress
	if !pveCompress[compress] {
		compress = "zstd"
	}
	byNode, err := groupBackupGuests(ctx, spec.Guests)
	if err != nil {
		return 1, err.Error()
	}
	if len(byNode) == 0 {
		return 1, "keine gültigen Gäste angegeben"
	}

	deadline := time.Duration(spec.MaxMinutes) * time.Minute
	if deadline <= 0 {
		deadline = 12 * time.Hour
	}
	cctx, cancel := context.WithTimeout(ctx, deadline)
	defer cancel()

	results := map[int]vzdumpResult{}
	var logs []string
	failed := false
	for _, node := range sortedKeys(byNode) {
		vmids := byNode[node]
		args := []string{"create", "/nodes/" + node + "/vzdump",
			"--vmid", joinInts(vmids), "--mode", mode, "--compress", compress}
		if spec.Storage != "" {
			args = append(args, "--storage", spec.Storage)
		}
		if strings.TrimSpace(spec.Notes) != "" {
			args = append(args, "--notes-template", spec.Notes)
		}
		note := fmt.Sprintf("%s: vzdump %s gestartet", node, joinInts(vmids))
		report(progress, note)

		out, err := exec.CommandContext(cctx, "pvesh", args...).CombinedOutput()
		upid := reUPID.FindString(string(out))
		if upid == "" {
			failed = true
			msg := strings.TrimSpace(string(out))
			if msg == "" && err != nil {
				msg = err.Error()
			}
			logs = append(logs, node+": Start fehlgeschlagen: "+msg)
			for _, id := range vmids {
				results[id] = vzdumpResult{ok: false, msg: "Start fehlgeschlagen"}
			}
			continue
		}
		status, lines := waitForTask(cctx, node, upid, progress)
		nodeResults := parseVzdumpLog(lines, status)
		for id, r := range nodeResults {
			results[id] = r
			if !r.ok {
				failed = true
			}
		}
		// Gäste, über die das Protokoll nichts sagt (Abbruch vor dem Start).
		for _, id := range vmids {
			if _, ok := results[id]; !ok {
				failed = true
				results[id] = vzdumpResult{ok: false, msg: "kein Ergebnis im Protokoll (" + status + ")"}
			}
		}
		logs = append(logs, lines...)
	}

	summary := backupSummary(results)
	exit := 0
	if failed {
		exit = 1
	}
	return exit, summary + "\n" + tailLines(logs, 120)
}

// groupBackupGuests gruppiert die Gäste nach Node und korrigiert den Node anhand des
// aktuellen Clusterzustands – ein Gast kann seit der Planung migriert worden sein.
func groupBackupGuests(ctx context.Context, guests []BackupGuest) (map[string][]int, error) {
	current := map[int]string{}
	var res []pveResource
	if err := pveshGet(ctx, &res, "/cluster/resources", "--type", "vm"); err == nil {
		for _, r := range res {
			if r.Type == "qemu" || r.Type == "lxc" {
				current[r.VMID] = r.Node
			}
		}
	}
	byNode := map[string][]int{}
	for _, g := range guests {
		if g.VMID < 100 {
			continue
		}
		node := g.Node
		if live, ok := current[g.VMID]; ok && live != "" {
			node = live // Gast wurde verschoben
		}
		if !pveNodeName.MatchString(node) {
			continue
		}
		byNode[node] = append(byNode[node], g.VMID)
	}
	for node := range byNode {
		sort.Ints(byNode[node])
	}
	return byNode, nil
}

// waitForTask verfolgt einen PVE-Task bis zum Ende und liefert Status und Protokoll.
func waitForTask(ctx context.Context, node, upid string, progress func(string)) (string, []string) {
	type taskStatus struct {
		Status     string `json:"status"`     // running | stopped
		ExitStatus string `json:"exitstatus"` // OK | Fehlertext
	}
	lastNote := time.Now()
	for {
		var st taskStatus
		if err := pveshGet(ctx, &st, "/nodes/"+node+"/tasks/"+upid+"/status"); err != nil {
			return "Statusabfrage fehlgeschlagen: " + err.Error(), nil
		}
		if st.Status != "running" {
			lines := taskLog(ctx, node, upid)
			status := st.ExitStatus
			if status == "" {
				status = "unbekannt"
			}
			return status, lines
		}
		if time.Since(lastNote) >= time.Minute {
			lastNote = time.Now()
			if lines := taskLog(ctx, node, upid); len(lines) > 0 {
				report(progress, lines[len(lines)-1])
			}
		}
		select {
		case <-ctx.Done():
			return "abgebrochen (Zeitlimit)", taskLog(ctx, node, upid)
		case <-time.After(10 * time.Second):
		}
	}
}

// taskLog liest das Protokoll eines Tasks.
func taskLog(ctx context.Context, node, upid string) []string {
	var lines []struct {
		T string `json:"t"`
	}
	if err := pveshGet(ctx, &lines, "/nodes/"+node+"/tasks/"+upid+"/log", "--limit", "50000"); err != nil {
		return nil
	}
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = l.T
	}
	return out
}

// backupSummary fasst das Ergebnis je Gast in einer Zeile zusammen.
func backupSummary(results map[int]vzdumpResult) string {
	var okIDs, failIDs []int
	for id, r := range results {
		if r.ok {
			okIDs = append(okIDs, id)
		} else {
			failIDs = append(failIDs, id)
		}
	}
	sort.Ints(okIDs)
	sort.Ints(failIDs)
	if len(failIDs) == 0 {
		return fmt.Sprintf("%d Gäste gesichert (%s)", len(okIDs), joinInts(okIDs))
	}
	parts := make([]string, 0, len(failIDs))
	for _, id := range failIDs {
		parts = append(parts, fmt.Sprintf("%d: %s", id, results[id].msg))
	}
	return fmt.Sprintf("%d von %d Gästen fehlgeschlagen – %s",
		len(failIDs), len(failIDs)+len(okIDs), strings.Join(parts, "; "))
}

// tailLines liefert die letzten n Zeilen (das Ende eines vzdump-Protokolls ist das
// Interessante; trunc kappt die Ausgabe sonst mitten im Vorspann).
func tailLines(lines []string, n int) string {
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

func report(progress func(string), msg string) {
	if progress != nil {
		progress(msg)
	}
}

func sortedKeys(m map[string][]int) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func joinInts(v []int) string {
	parts := make([]string, len(v))
	for i, n := range v {
		parts[i] = strconv.Itoa(n)
	}
	return strings.Join(parts, ",")
}
