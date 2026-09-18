package api

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/boonkerz/roster/internal/server/alert"
	"github.com/boonkerz/roster/internal/server/model"
	"github.com/boonkerz/roster/internal/server/store"
	"github.com/boonkerz/roster/internal/shared"
)

// Abschluss eines Backup-Laufs: Ergebnis festhalten, ggf. alarmieren und die Folgeaktion
// auf dem anderen Gerät einreihen.
//
// Der Normalweg kommt über den Checkin (completeBackupRuns) – dann liegt das Ergebnis
// ohne Verzögerung vor. Der Loop macht dasselbe für Läufe, deren Ergebnis nie ankommt
// (Timeout, Agent-Neustart). Damit die Folgeaktion trotz zweier Wege genau einmal läuft,
// entscheidet allein der bedingte Update in FinishBackupRun.

// completeBackupRuns wird aus handleCheckin aufgerufen, nachdem Befehlsergebnisse
// gespeichert wurden.
func (s *Server) completeBackupRuns(ctx context.Context, results []shared.CommandResult) {
	for _, res := range results {
		run, err := s.store.BackupRunByCommand(ctx, res.CommandID)
		if err != nil || run == nil {
			continue // gehört zu keinem Backup-Lauf
		}
		b, err := s.store.GetBackup(ctx, run.BackupID)
		if err != nil {
			continue
		}
		status := "ok"
		if res.ExitCode != 0 {
			status = "failed"
		}
		summary, output := splitBackupOutput(res.Output)
		s.finishRun(ctx, *run, *b, status, res.ExitCode, summary, output)
	}
}

// splitBackupOutput trennt die erste Zeile (Zusammenfassung des Agents) vom Rest.
func splitBackupOutput(out string) (summary, rest string) {
	out = strings.TrimSpace(out)
	if out == "" {
		return "", ""
	}
	if i := strings.IndexByte(out, '\n'); i >= 0 {
		return strings.TrimSpace(out[:i]), strings.TrimSpace(out[i+1:])
	}
	return out, ""
}

// finishRun schließt einen Lauf ab. Alarm und Folgeaktion laufen nur, wenn dieser Aufruf
// den Zustand tatsächlich geändert hat (sonst war ein anderer Pfad schneller).
func (s *Server) finishRun(ctx context.Context, run model.BackupRun, b model.PolicyBackup, status string, exitCode int, summary, output string) {
	changed, err := s.store.FinishBackupRun(ctx, run.ID, status, exitCode, summary, output)
	if err != nil {
		s.log.Error("backup-lauf abschließen", "lauf", run.ID, "err", err)
		return
	}
	if !changed {
		return
	}
	run.Status, run.ExitCode, run.Summary = status, exitCode, summary
	now := time.Now().UTC()
	run.FinishedAt = &now

	s.log.Info("backup beendet", "backup", b.Name, "gerät", run.DeviceID, "status", status, "ergebnis", summary)
	if status != "ok" {
		s.notifyBackupFailed(ctx, run, b)
	}
	s.maybeRunFollowUp(ctx, run, b)
}

// maybeRunFollowUp reiht die Folgeaktion auf dem konfigurierten Gerät ein.
//
// Zwei Besonderheiten: Sie läuft auch nach Timeout oder Fehlschlag (after_when=always) –
// beim Anwendungsfall „Backup-Ziel schläft danach wieder ein" darf ein Fehler nicht dazu
// führen, dass das Gerät wach bleibt. Und sie wartet, solange noch andere Läufe auf
// dasselbe Zielgerät offen sind: sonst legt der erste fertige Lauf das Ziel schlafen,
// während ein zweiter noch schreibt.
func (s *Server) maybeRunFollowUp(ctx context.Context, run model.BackupRun, b model.PolicyBackup) {
	if b.AfterDeviceID == nil || b.AfterWhen == "" {
		return
	}
	success := run.Status == "ok"
	switch b.AfterWhen {
	case "success":
		if !success {
			return
		}
	case "failure":
		if success {
			return
		}
	}
	if b.AfterScriptID == nil {
		s.log.Warn("folgeaktion ohne skript", "backup", b.ID)
		s.notifyBackupProblem(ctx, run.DeviceID, "Backup „"+b.Name+"“: Folgeskript fehlt (gelöscht?) – es wurde nichts ausgeführt.")
		return
	}
	if open, err := s.store.OpenRunsForFollowDevice(ctx, *b.AfterDeviceID, run.ID); err == nil && open > 0 {
		s.log.Info("folgeaktion verschoben – weitere backups laufen", "backup", b.ID, "offen", open)
		return
	}

	sc, err := s.store.ScriptByID(ctx, *b.AfterScriptID)
	if err != nil {
		s.log.Warn("folgeskript nicht gefunden", "backup", b.ID, "err", err)
		return
	}
	content := sc.Content
	if agent, client, site, ferr := s.store.FieldMapsForDevice(ctx, *b.AfterDeviceID); ferr == nil {
		content = store.SubstituteFields(content, agent, client, site)
	}
	device, _ := s.store.GetDevice(ctx, run.DeviceID)
	payload := map[string]any{
		"shell": sc.Shell, "script": content, "platforms": sc.Platforms,
		"env": backupEnv(run, b, device),
	}
	cmdID, err := s.queueCommand(ctx, *b.AfterDeviceID, "run_script", "Nach Backup: "+b.Name, payload)
	if err != nil {
		s.log.Error("folgeaktion einreihen", "backup", b.ID, "err", err)
		return
	}
	_ = s.store.SetBackupRunFollow(ctx, run.ID, cmdID)
	_ = s.store.InsertAudit(ctx, model.AuditEntry{
		TS: time.Now().UTC(), Username: "system",
		Action: "Folgeaktion nach Backup „" + b.Name + "“ (" + run.Status + ")",
		Method: "AUTO", Path: "/devices/" + *b.AfterDeviceID, Status: 200,
	})
}

// backupEnv stellt dem Folgeskript das Ergebnis als Umgebungsvariablen bereit.
func backupEnv(run model.BackupRun, b model.PolicyBackup, device *model.Device) map[string]string {
	host := run.DeviceID
	if device != nil && device.Hostname != "" {
		host = device.Hostname
	}
	dur := 0
	if run.StartedAt != nil && run.FinishedAt != nil {
		dur = int(run.FinishedAt.Sub(*run.StartedAt).Seconds())
	}
	env := map[string]string{
		"ROSTER_BACKUP_STATUS":       run.Status, // ok | failed | timeout
		"ROSTER_BACKUP_NAME":         b.Name,
		"ROSTER_BACKUP_ID":           b.ID,
		"ROSTER_BACKUP_RUN_ID":       run.ID,
		"ROSTER_BACKUP_EXIT":         strconv.Itoa(run.ExitCode),
		"ROSTER_BACKUP_DEVICE":       host,
		"ROSTER_BACKUP_SUMMARY":      run.Summary,
		"ROSTER_BACKUP_DURATION_SEC": strconv.Itoa(dur),
	}
	if run.StartedAt != nil {
		env["ROSTER_BACKUP_STARTED"] = run.StartedAt.UTC().Format(time.RFC3339)
	}
	if run.FinishedAt != nil {
		env["ROSTER_BACKUP_FINISHED"] = run.FinishedAt.UTC().Format(time.RFC3339)
	}
	for k, v := range env {
		env[k] = sanitizeEnvValue(v)
	}
	return env
}

// sanitizeEnvValue macht aus einer Ausgabe einen brauchbaren Variablenwert:
// einzeilig und gekappt.
func sanitizeEnvValue(v string) string {
	v = strings.ReplaceAll(strings.ReplaceAll(v, "\r", " "), "\n", " ")
	v = strings.TrimSpace(v)
	if len(v) > 1000 {
		v = v[:1000] + "…"
	}
	return v
}

// notifyBackupFailed meldet einen fehlgeschlagenen Lauf über die Alarmkanäle des Geräts
// (gleiche Regeln wie die übrigen Meldewege: Wartungsfenster, Mindest-Schweregrad).
func (s *Server) notifyBackupFailed(ctx context.Context, run model.BackupRun, b model.PolicyBackup) {
	device, err := s.store.GetDevice(ctx, run.DeviceID)
	if err != nil {
		return
	}
	reason := run.Summary
	if reason == "" {
		reason = "ohne nähere Angabe"
	}
	subject := fmt.Sprintf("[Roster] Backup fehlgeschlagen: %s (%s)", b.Name, device.Hostname)
	body := fmt.Sprintf("Das Backup %q auf %q endete mit Status %s.\n\n%s",
		b.Name, device.Hostname, run.Status, reason)
	s.dispatchDeviceAlert(ctx, device, "critical", subject, body)
}

// notifyBackupProblem meldet Konfigurationsprobleme rund um Backups.
func (s *Server) notifyBackupProblem(ctx context.Context, deviceID, text string) {
	device, err := s.store.GetDevice(ctx, deviceID)
	if err != nil {
		return
	}
	s.dispatchDeviceAlert(ctx, device, "warning", "[Roster] Backup-Konfiguration", text)
}

// dispatchDeviceAlert verschickt eine Meldung an die Kanäle eines Geräts – dieselbe
// Reihenfolge wie notifyOffline: Alarme aktiv, kein Wartungsfenster, Schweregrad-Filter.
func (s *Server) dispatchDeviceAlert(ctx context.Context, d *model.Device, severity, subject, body string) {
	if cfg, err := s.store.GetAlertConfig(ctx); err != nil || !cfg.Enabled {
		return
	}
	if inMaint, err := s.store.DeviceInMaintenance(ctx, d.ID, time.Now()); err == nil && inMaint {
		return
	}
	channels, err := s.store.ChannelsForDevice(ctx, d.ID)
	if err != nil {
		return
	}
	for _, ch := range channels {
		if severityRank(severity) < severityRank(ch.MinSeverity) {
			continue
		}
		alert.Dispatch(s.log, ch, alert.Notification{Subject: subject, Body: body})
	}
}
