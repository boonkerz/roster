package collect

import (
	"context"
	"fmt"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Aufräumen alter Backups (Befehl proxmox_prune): entfernt je Gast auf seinem
// Backup-Speicher alle Archive bis auf die letzten N – ohne vorher ein neues Backup
// zu ziehen. Das ist der Ausweg, wenn ein Speicher bereits voll ist: vzdump mit
// --prune-backups räumt erst NACH dem Sichern auf und käme dort gar nicht mehr zum Zug.
// Genutzt wird die PVE-API /nodes/{node}/storage/{storage}/prunebackups: GET zeigt an,
// was passieren würde (Trockenlauf), DELETE führt es aus (liefert eine Task-UPID).

// PruneSpec beschreibt einen Aufräumlauf, wie ihn der Server schickt.
type PruneSpec struct {
	Guests   []BackupGuest `json:"guests"`
	Storage  string        `json:"storage"` // Vorgabe, wenn ein Gast kein eigenes Ziel hat
	KeepLast int           `json:"keep_last"`
	DryRun   bool          `json:"dry_run"`
}

// pruneEntry ist eine Zeile aus GET …/prunebackups.
type pruneEntry struct {
	VolID string `json:"volid"`
	Mark  string `json:"mark"` // keep | remove | protected | renamed
	VMID  int    `json:"vmid"`
	CTime int64  `json:"ctime"`
}

// pruneOption baut den Wert für --prune-backups.
func pruneOption(keepLast int) string { return "keep-last=" + strconv.Itoa(keepLast) }

// ProxmoxPrune räumt die Backups der angegebenen Gäste auf. Rückgabe: Exit-Code und
// Ausgabe, deren ERSTE Zeile die Zusammenfassung ist; danach je Archiv eine Zeile.
func ProxmoxPrune(ctx context.Context, spec PruneSpec, progress func(string)) (int, string) {
	if !ProxmoxAvailable() {
		return 1, "dies ist kein Proxmox-VE-Host (pvesh fehlt)"
	}
	if spec.KeepLast <= 0 {
		return 1, "keep_last fehlt – im Backup-Eintrag festlegen, wie viele Sicherungen bleiben sollen"
	}
	if spec.Storage != "" && !pveStorageName.MatchString(spec.Storage) {
		return 1, "ungültiger Speichername: " + spec.Storage
	}
	byNode, err := groupBackupGuests(ctx, spec.Guests)
	if err != nil {
		return 1, err.Error()
	}
	if len(byNode) == 0 {
		return 1, "keine gültigen Gäste angegeben"
	}
	types := map[int]string{}
	for _, g := range spec.Guests {
		types[g.VMID] = g.Type
	}

	cctx, cancel := context.WithTimeout(ctx, 2*time.Hour)
	defer cancel()

	var entries []pruneEntry
	var errs []string
	for _, grp := range sortedGroups(byNode) {
		storage := grp.storage
		if storage == "" {
			storage = spec.Storage
		}
		if storage == "" {
			errs = append(errs, fmt.Sprintf("%s: kein Backup-Ziel bekannt für %s", grp.node, joinInts(byNode[grp])))
			continue
		}
		if !pveStorageName.MatchString(storage) {
			errs = append(errs, "ungültiger Speichername: "+storage)
			continue
		}
		for _, vmid := range byNode[grp] {
			path := "/nodes/" + grp.node + "/storage/" + storage + "/prunebackups"
			args := []string{"--prune-backups", pruneOption(spec.KeepLast), "--vmid", strconv.Itoa(vmid)}
			if t := types[vmid]; t == "qemu" || t == "lxc" {
				args = append(args, "--type", t)
			}
			// Immer erst der Trockenlauf – er liefert die Liste, die auch nach dem
			// echten Lauf als Protokoll dient (DELETE meldet nur eine Task-UPID).
			var plan []pruneEntry
			if err := pveshGet(cctx, &plan, path, args...); err != nil {
				errs = append(errs, fmt.Sprintf("%d auf %s: %v", vmid, storage, err))
				continue
			}
			entries = append(entries, plan...)
			if spec.DryRun || countMark(plan, "remove") == 0 {
				continue
			}
			report(progress, fmt.Sprintf("%s: räume %d auf %s auf (%d Archive)", grp.node, vmid, storage, countMark(plan, "remove")))
			out, err := exec.CommandContext(cctx, "pvesh", append([]string{"delete", path}, args...)...).CombinedOutput()
			if err != nil {
				errs = append(errs, fmt.Sprintf("%d auf %s: %s", vmid, storage, firstNonEmpty(strings.TrimSpace(string(out)), err.Error())))
				continue
			}
			// Neuere PVE-Versionen legen einen Task an; auf dessen Ende warten, damit
			// das Ergebnis den tatsächlichen Stand widerspiegelt.
			if upid := reUPID.FindString(string(out)); upid != "" {
				if status, _ := waitForTask(cctx, grp.node, upid, progress); status != "OK" {
					errs = append(errs, fmt.Sprintf("%d auf %s: %s", vmid, storage, status))
				}
			}
		}
	}

	exit := 0
	if len(errs) > 0 {
		exit = 1
	}
	return exit, pruneReport(entries, errs, spec.DryRun)
}

func countMark(entries []pruneEntry, mark string) int {
	n := 0
	for _, e := range entries {
		if e.Mark == mark {
			n++
		}
	}
	return n
}

// pruneReport: erste Zeile Zusammenfassung, dann je Archiv „entfernt/behalten <volid>“.
func pruneReport(entries []pruneEntry, errs []string, dryRun bool) string {
	removed, kept := countMark(entries, "remove"), countMark(entries, "keep")+countMark(entries, "protected")
	var head string
	switch {
	case dryRun:
		head = fmt.Sprintf("Trockenlauf: %d Archive würden entfernt, %d bleiben", removed, kept)
	default:
		head = fmt.Sprintf("%d Archive entfernt, %d bleiben", removed, kept)
	}
	if len(errs) > 0 {
		head += fmt.Sprintf(" – %d Fehler", len(errs))
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].VMID != entries[j].VMID {
			return entries[i].VMID < entries[j].VMID
		}
		return entries[i].CTime < entries[j].CTime
	})
	lines := []string{head}
	for _, e := range entries {
		verb := "behalten"
		switch {
		case e.Mark == "remove" && dryRun:
			verb = "würde entfernt"
		case e.Mark == "remove":
			verb = "entfernt"
		case e.Mark == "protected":
			verb = "geschützt"
		}
		when := ""
		if e.CTime > 0 {
			when = " (" + time.Unix(e.CTime, 0).Format("02.01.2006 15:04") + ")"
		}
		lines = append(lines, fmt.Sprintf("%-14s %s%s", verb, e.VolID, when))
	}
	for _, e := range errs {
		lines = append(lines, "FEHLER: "+e)
	}
	return strings.Join(lines, "\n")
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
