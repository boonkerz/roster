package api

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/boonkerz/roster/internal/server/model"
	"github.com/boonkerz/roster/internal/server/store"
)

// Backup-Zeitplan. Anders als Tasks (die der Agent selbst plant) steuert der Server die
// Backups: nur er kennt andere Geräte (Startbedingung „warte auf das Backup-Ziel",
// Folgeaktion auf einem anderen Gerät) und behält Läufe über Neustarts hinweg.
//
// Der Loop ist bewusst schlicht und idempotent: Jeder Tick schaut nach fälligen
// Einträgen, wartenden und laufenden Läufen. Alle Zustandswechsel gehen über bedingte
// Updates im Store – doppelte Ticks oder zwei Serverinstanzen können nichts doppelt tun.

// minAgentVersionBackup ist die erste Agent-Version, die den Befehl proxmox_backup
// kennt. Ältere Agenten verwerfen unbekannte Befehle kommentarlos; der Lauf liefe sonst
// bis zum Timeout ins Leere.
const minAgentVersionBackup = "0.15.0"

// RunBackupLoop prüft jede Minute Zeitpläne und offene Läufe. Wird beim Serverstart
// als Goroutine gestartet (wie RunReportLoop / RunOfflineLoop).
func (s *Server) RunBackupLoop(ctx context.Context) {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.backupTick(ctx, time.Now())
		}
	}
}

func (s *Server) backupTick(ctx context.Context, now time.Time) {
	s.scheduleDueBackups(ctx, now)
	s.progressOpenRuns(ctx, now)
}

// scheduleDueBackups legt für fällige Einträge je zugewiesenem Gerät einen Lauf an.
func (s *Server) scheduleDueBackups(ctx context.Context, now time.Time) {
	backups, err := s.store.EnabledBackups(ctx)
	if err != nil {
		s.log.Error("backup-einträge lesen", "err", err)
		return
	}
	for _, b := range backups {
		scheduled, due := backupDue(b, now)
		if !due {
			continue
		}
		devices, err := s.backupTargets(ctx, b)
		if err != nil {
			s.log.Error("backup-ziele ermitteln", "backup", b.ID, "err", err)
			continue
		}
		if len(devices) == 0 {
			continue // keine passende Zuweisung – nichts zu planen
		}
		// Erst den Anspruch sichern (bedingtes UPDATE), dann Läufe anlegen.
		claimed, err := s.store.ClaimBackupSchedule(ctx, b.ID, scheduled)
		if err != nil {
			s.log.Error("backup-zeitplan sichern", "backup", b.ID, "err", err)
			continue
		}
		if !claimed {
			continue // ein anderer Tick / eine andere Instanz war schneller
		}
		for i := range devices {
			s.startOrQueueRun(ctx, b, &devices[i], scheduled, "schedule", now)
		}
	}
}

// backupTargets liefert die Geräte, auf denen ein Eintrag laufen soll. Proxmox-Backups
// nur auf PVE-Hosts – sonst erzeugt eine Standort-Zuweisung Läufe auf jedem Windows-PC.
func (s *Server) backupTargets(ctx context.Context, b model.PolicyBackup) ([]model.Device, error) {
	devices, err := s.store.DevicesForPolicy(ctx, b.PolicyID)
	if err != nil {
		return nil, err
	}
	out := devices[:0]
	for _, d := range devices {
		if b.Type == "proxmox" && d.ProxmoxVersion == "" {
			continue
		}
		out = append(out, d)
	}
	return out, nil
}

// startOrQueueRun legt einen Lauf an: entweder wartend (Startbedingung) oder sofort
// gestartet. Liefert die Lauf-ID (leer bei Fehler).
func (s *Server) startOrQueueRun(ctx context.Context, b model.PolicyBackup, d *model.Device, scheduled time.Time, trigger string, now time.Time) string {
	run := &model.BackupRun{
		BackupID: b.ID, DeviceID: d.ID, TriggerType: trigger,
		Status: "waiting", ScheduledAt: scheduled,
	}
	if b.WaitDeviceID != nil && b.WaitMinutes > 0 {
		until := now.Add(time.Duration(b.WaitMinutes) * time.Minute)
		run.WaitUntil = &until
	}
	if err := s.store.InsertBackupRun(ctx, run); err != nil {
		// Der UNIQUE-Index verhindert Doppelläufe – das ist kein Fehlerfall.
		s.log.Info("backup-lauf nicht angelegt", "backup", b.ID, "gerät", d.ID, "grund", err)
		return ""
	}
	s.tryStartRun(ctx, b, *run, now)
	return run.ID
}

// tryStartRun startet einen wartenden Lauf, sobald die Startbedingung erfüllt ist.
func (s *Server) tryStartRun(ctx context.Context, b model.PolicyBackup, run model.BackupRun, now time.Time) {
	device, err := s.store.GetDevice(ctx, run.DeviceID)
	if err != nil {
		s.finishRun(ctx, run, b, "failed", 1, "Gerät nicht mehr vorhanden", "")
		return
	}
	if s.computeStatus(device) != "online" {
		if run.WaitUntil == nil || now.After(*run.WaitUntil) {
			s.finishRun(ctx, run, b, "timeout", 1, "Gerät "+device.Hostname+" war nicht erreichbar", "")
		}
		return // sonst: nächster Tick
	}
	if b.WaitDeviceID != nil {
		waitFor, err := s.store.GetDevice(ctx, *b.WaitDeviceID)
		if err == nil && s.computeStatus(waitFor) != "online" {
			if run.WaitUntil == nil || now.After(*run.WaitUntil) {
				s.finishRun(ctx, run, b, "timeout", 1, "Startbedingung nicht erfüllt: "+waitFor.Hostname+" blieb offline", "")
			}
			return
		}
	}
	if b.Type == "proxmox" && !agentSupportsBackup(device.AgentVersion) {
		s.finishRun(ctx, run, b, "failed", 1,
			"Agent "+device.AgentVersion+" kennt den Befehl noch nicht (nötig: "+minAgentVersionBackup+")", "")
		return
	}

	payload, label, cmdType, err := s.backupCommand(ctx, b, device)
	if err != nil {
		s.finishRun(ctx, run, b, "failed", 1, err.Error(), "")
		return
	}
	cmdID, err := s.queueCommand(ctx, device.ID, cmdType, label, payload)
	if err != nil {
		s.finishRun(ctx, run, b, "failed", 1, "Befehl konnte nicht eingereiht werden: "+err.Error(), "")
		return
	}
	deadline := now.Add(time.Duration(maxInt(b.TimeoutMinutes, 60)) * time.Minute)
	started, err := s.store.StartBackupRun(ctx, run.ID, cmdID, deadline)
	if err != nil || !started {
		return
	}
	_ = s.store.InsertAudit(ctx, model.AuditEntry{
		TS: time.Now().UTC(), Username: "system", Action: "Backup gestartet: " + b.Name,
		Method: "AUTO", Path: "/devices/" + device.ID, Status: 200,
	})
	s.log.Info("backup gestartet", "backup", b.ID, "gerät", device.Hostname, "befehl", cmdID)
}

// backupCommand baut den Agent-Befehl für einen Eintrag.
func (s *Server) backupCommand(ctx context.Context, b model.PolicyBackup, d *model.Device) (map[string]any, string, string, error) {
	switch b.Type {
	case "proxmox":
		guests := proxmoxBackupGuests(b, d)
		if len(guests) == 0 {
			return nil, "", "", fmt.Errorf("keine passenden Gäste auf %s", d.Hostname)
		}
		payload := map[string]any{
			"guests":      guests,
			"storage":     cfgString(b.Config, "storage"),
			"mode":        cfgStringOr(b.Config, "mode", "snapshot"),
			"compress":    cfgStringOr(b.Config, "compress", "zstd"),
			"notes":       cfgString(b.Config, "notes"),
			"max_minutes": maxInt(b.TimeoutMinutes, 60),
		}
		return payload, "Backup: " + b.Name, "proxmox_backup", nil

	case "script":
		if b.ScriptID == nil {
			return nil, "", "", fmt.Errorf("kein Skript hinterlegt")
		}
		sc, err := s.store.ScriptByID(ctx, *b.ScriptID)
		if err != nil {
			return nil, "", "", fmt.Errorf("skript nicht gefunden")
		}
		content := sc.Content
		if agent, client, site, ferr := s.store.FieldMapsForDevice(ctx, d.ID); ferr == nil {
			content = store.SubstituteFields(content, agent, client, site)
		}
		payload := map[string]any{"shell": sc.Shell, "script": content, "platforms": sc.Platforms}
		return payload, "Backup: " + b.Name, "run_script", nil
	}
	return nil, "", "", fmt.Errorf("unbekannter Backup-Typ %q", b.Type)
}

// proxmoxBackupGuests wählt die zu sichernden Gäste aus dem Inventar des Hosts. Der
// Node kommt aus dem Inventar; der Agent prüft ihn vor dem Lauf noch einmal (ein Gast
// kann inzwischen migriert sein). Vorlagen werden nie gesichert.
func proxmoxBackupGuests(b model.PolicyBackup, d *model.Device) []map[string]any {
	all := cfgBool(b.Config, "all")
	wanted := map[int]bool{}
	for _, v := range cfgInts(b.Config, "vmids") {
		wanted[v] = true
	}
	var out []map[string]any
	for _, g := range d.ProxmoxGuests {
		if g.Template || (!all && !wanted[g.VMID]) {
			continue
		}
		out = append(out, map[string]any{"node": g.Node, "vmid": g.VMID, "type": g.Type})
	}
	return out
}

// progressOpenRuns bringt wartende Läufe voran und beendet hängende (Watchdog). Der
// Normalfall – Ergebnis des Agents – läuft über den Checkin-Hook, nicht hierüber.
func (s *Server) progressOpenRuns(ctx context.Context, now time.Time) {
	runs, err := s.store.OpenBackupRuns(ctx)
	if err != nil {
		s.log.Error("offene backup-läufe lesen", "err", err)
		return
	}
	for _, run := range runs {
		b, err := s.store.GetBackup(ctx, run.BackupID)
		if err != nil {
			continue
		}
		switch run.Status {
		case "waiting":
			s.tryStartRun(ctx, *b, run, now)
		case "running":
			if run.DeadlineAt != nil && now.After(*run.DeadlineAt) {
				s.finishRun(ctx, run, *b, "timeout", 1, "Zeitlimit überschritten", "")
			}
		}
	}
}

// agentSupportsBackup vergleicht die gemeldete Agent-Version mit der Mindestversion.
// Unbekannte oder Entwicklungsversionen ("dev") gelten als tauglich.
func agentSupportsBackup(version string) bool {
	v := strings.TrimSpace(version)
	if v == "" || v == "dev" {
		return true
	}
	return compareVersions(v, minAgentVersionBackup) >= 0
}

// compareVersions vergleicht "1.2.3"-Versionen numerisch (-1, 0, 1).
func compareVersions(a, b string) int {
	pa, pb := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < 3; i++ {
		na, nb := versionPart(pa, i), versionPart(pb, i)
		if na != nb {
			if na < nb {
				return -1
			}
			return 1
		}
	}
	return 0
}

func versionPart(parts []string, i int) int {
	if i >= len(parts) {
		return 0
	}
	digits := parts[i]
	if idx := strings.IndexFunc(digits, func(r rune) bool { return r < '0' || r > '9' }); idx >= 0 {
		digits = digits[:idx] // "1.2.3-rc1" → 3
	}
	n, _ := strconv.Atoi(digits)
	return n
}

func maxInt(v, min int) int {
	if v < min {
		return min
	}
	return v
}

// Kleine Helfer für die typabhängige Config (JSON aus der Oberfläche).
func cfgString(cfg map[string]any, key string) string {
	s, _ := cfg[key].(string)
	return strings.TrimSpace(s)
}

func cfgStringOr(cfg map[string]any, key, def string) string {
	if v := cfgString(cfg, key); v != "" {
		return v
	}
	return def
}

func cfgBool(cfg map[string]any, key string) bool {
	b, _ := cfg[key].(bool)
	return b
}

func cfgInts(cfg map[string]any, key string) []int {
	raw, _ := cfg[key].([]any)
	out := make([]int, 0, len(raw))
	for _, v := range raw {
		switch n := v.(type) {
		case float64:
			out = append(out, int(n))
		case int:
			out = append(out, n)
		case string:
			if i, err := strconv.Atoi(strings.TrimSpace(n)); err == nil {
				out = append(out, i)
			}
		}
	}
	return out
}
