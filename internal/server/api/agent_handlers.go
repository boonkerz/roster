package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/thomaspeterson/pc-inventory/internal/server/alert"
	"github.com/thomaspeterson/pc-inventory/internal/server/auth"
	"github.com/thomaspeterson/pc-inventory/internal/server/model"
	"github.com/thomaspeterson/pc-inventory/internal/server/store"
	"github.com/thomaspeterson/pc-inventory/internal/shared"
)

// handleEnroll tauscht ein gültiges Enrollment-Token gegen ein eindeutiges Agent-Token.
func (s *Server) handleEnroll(w http.ResponseWriter, r *http.Request) {
	var req shared.EnrollRequest
	if !s.decodeJSON(w, r, &req) {
		return
	}
	if req.EnrollmentToken == "" {
		s.writeErr(w, http.StatusBadRequest, "enrollment_token fehlt")
		return
	}

	siteID, err := s.store.ConsumeEnrollmentToken(r.Context(), auth.HashToken(req.EnrollmentToken))
	switch {
	case errors.Is(err, store.ErrNotFound), errors.Is(err, store.ErrTokenExhausted):
		s.writeErr(w, http.StatusUnauthorized, "enrollment-token ungültig, abgelaufen oder aufgebraucht")
		return
	case err != nil:
		s.mapStoreErr(w, err)
		return
	}

	agentToken := auth.GenerateToken()
	device := &model.Device{
		ID:        store.NewID(),
		Hostname:  req.Hostname,
		OS:        req.OS,
		OSVersion: req.OSVersion,
		SiteID:    siteID,
	}
	if err := s.store.CreateDevice(r.Context(), device, auth.HashToken(agentToken)); err != nil {
		s.mapStoreErr(w, err)
		return
	}

	s.log.Info("agent enrolled", "device_id", device.ID, "hostname", req.Hostname)
	s.writeJSON(w, http.StatusOK, shared.EnrollResponse{
		AgentID:    device.ID,
		AgentToken: agentToken,
	})
}

// handleCheckin nimmt das Inventar entgegen, aktualisiert last_seen und liefert
// ausstehende Befehle (aktuell leer; Befehlsausführung folgt in einer späteren Phase).
func (s *Server) handleCheckin(w http.ResponseWriter, r *http.Request) {
	device := deviceFrom(r.Context())
	var req shared.CheckinRequest
	if !s.decodeJSON(w, r, &req) {
		return
	}
	before := time.Now()
	if err := s.store.UpdateInventory(r.Context(), device.ID, req.Inventory); err != nil {
		s.mapStoreErr(w, err)
		return
	}
	s.alertSoftwareChanges(r.Context(), device, before)
	if len(req.CheckResults) > 0 {
		events, err := s.store.SaveCheckResults(r.Context(), device.ID, req.CheckResults)
		if err != nil {
			s.log.Error("check-ergebnisse speichern", "err", err)
		} else if len(events) > 0 {
			s.alertTransitions(r.Context(), device, events)
			s.runRemediations(r.Context(), device, events)
		}
	}
	if len(req.TaskResults) > 0 {
		if err := s.store.SaveTaskResults(r.Context(), device.ID, req.TaskResults); err != nil {
			s.log.Error("task-ergebnisse speichern", "err", err)
		}
		s.collectFields(r.Context(), device.ID, req.TaskResults)
	}
	if len(req.CommandResults) > 0 {
		if err := s.store.SaveCommandResults(r.Context(), req.CommandResults); err != nil {
			s.log.Error("befehls-ergebnisse speichern", "err", err)
		}
	}
	policy, err := s.store.EffectivePolicy(r.Context(), device.ID)
	if err != nil {
		s.log.Error("effektive policy", "err", err)
	}
	commands, err := s.store.PendingCommands(r.Context(), device.ID)
	if err != nil {
		s.log.Error("offene befehle", "err", err)
	}
	s.writeJSON(w, http.StatusOK, shared.CheckinResponse{
		NextCheckinSec:     int(s.cfg.CheckinInterval.Seconds()),
		Commands:           commands,
		LatestAgentVersion: s.version,
		Policy:             policy,
	})
}

// handleCommandProgress nimmt einen Zwischenstand eines laufenden Befehls entgegen
// (z.B. Verzeichnis-Scan) und legt ihn als Teil-Ergebnis ab – für Live-Polling.
func (s *Server) handleCommandProgress(w http.ResponseWriter, r *http.Request) {
	device := deviceFrom(r.Context())
	var req shared.CommandProgress
	if !s.decodeJSON(w, r, &req) {
		return
	}
	if req.CommandID == "" {
		s.writeErr(w, http.StatusBadRequest, "command_id fehlt")
		return
	}
	if err := s.store.UpdateCommandProgress(r.Context(), device.ID, req.CommandID, req.Output); err != nil {
		s.mapStoreErr(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// collectFields übernimmt die JSON-Ausgabe von Collector-Tasks in benutzerdefinierte Felder.
func (s *Server) collectFields(ctx context.Context, deviceID string, results []shared.TaskResult) {
	collectors, err := s.store.CollectorTaskIDs(ctx)
	if err != nil || len(collectors) == 0 {
		return
	}
	for _, r := range results {
		if !collectors[r.TaskID] || r.Output == "" {
			continue
		}
		if err := s.store.ApplyCollected(ctx, deviceID, r.Output); err != nil {
			s.log.Warn("collector-felder übernehmen", "task", r.TaskID, "err", err)
		}
	}
}

// severityRank ordnet Schweregrade: warning < critical. Ein Kanal mit
// min_severity="critical" erhält nur Checks ab Rang 2.
func severityRank(s string) int {
	if s == "critical" {
		return 2
	}
	return 1 // warning (und alles Unbekannte)
}

// resultSeverity bestimmt den Alarm-Schweregrad eines Ergebnisses: ein „warning"-
// Ergebnis ist immer Warnung; ein „failing"-Ergebnis nutzt den am Check
// konfigurierten Schweregrad (Standard critical).
func resultSeverity(status, checkSeverity string) string {
	if status == "warning" {
		return "warning"
	}
	if checkSeverity == "warning" {
		return "warning"
	}
	return "critical"
}

// eventSeverity bestimmt den Alarm-Rang eines Statuswechsels. Eine Wiederherstellung
// (→ passing) nutzt den Schweregrad des Zustands, aus dem sie kommt – damit eine
// „behoben"-Meldung dieselben Kanäle erreicht wie zuvor der Alarm.
func eventSeverity(ev model.CheckEvent, checkSeverity string) string {
	if ev.NewStatus == "passing" {
		return resultSeverity(ev.OldStatus, checkSeverity)
	}
	return resultSeverity(ev.NewStatus, checkSeverity)
}

// eventLabel liefert das Präfix einer Verlaufsmeldung.
func eventLabel(status string) string {
	switch status {
	case "passing":
		return "BEHOBEN"
	case "warning":
		return "WARNUNG"
	case "failing":
		return "FEHLER"
	default:
		return strings.ToUpper(status)
	}
}

// alertTransitions versendet bei Check-Statuswechseln Benachrichtigungen – auch bei
// Wiederherstellung (→ passing). Nur an die für das Gerät geltenden Kanäle und nur,
// wenn der Schweregrad den Mindest-Schweregrad des Kanals erreicht. Versendete
// Ereignisse werden im Verlauf als „benachrichtigt" markiert.
// remediationCooldown verhindert wiederholte Auto-Remediation bei flappenden Checks.
const remediationCooldown = 30 * time.Minute

// runRemediations führt bei Checks, die neu auf „failing" wechseln, ein hinterlegtes
// Remediation-Skript automatisch aus (Self-Healing) – mit Cooldown je Gerät/Check.
func (s *Server) runRemediations(ctx context.Context, device *model.Device, events []model.CheckEvent) {
	for _, ev := range events {
		if ev.NewStatus != "failing" {
			continue
		}
		sc, err := s.store.RemediationScript(ctx, ev.CheckID)
		if err != nil {
			continue // kein Remediation-Skript konfiguriert
		}
		if !s.store.RemediationDue(ctx, device.ID, ev.CheckID, remediationCooldown) {
			continue // Cooldown noch aktiv
		}
		content := sc.Content
		if agent, client, site, ferr := s.store.FieldMapsForDevice(ctx, device.ID); ferr == nil {
			content = store.SubstituteFields(content, agent, client, site)
		}
		name := ev.CheckName
		if name == "" {
			name = ev.CheckID
		}
		payload := map[string]any{"shell": sc.Shell, "script": content, "platforms": sc.Platforms}
		if _, err := s.queueCommand(ctx, device.ID, "run_script", "Auto-Remediation: "+name, payload); err != nil {
			s.log.Warn("remediation einreihen", "check", ev.CheckID, "err", err)
			continue
		}
		_ = s.store.MarkRemediation(ctx, device.ID, ev.CheckID, time.Now())
		_ = s.store.InsertAudit(ctx, model.AuditEntry{
			TS: time.Now().UTC(), Username: "system",
			Action: "Auto-Remediation ausgelöst (" + name + ")", Method: "AUTO",
			Path: "/devices/" + device.ID, Status: 200,
		})
		s.log.Info("auto-remediation ausgelöst", "device", device.Hostname, "check", name, "script", sc.Name)
	}
}

// alertSoftwareChanges benachrichtigt über seit `since` erfasste Software-Änderungen,
// sofern in den Alarm-Einstellungen aktiviert. Behandelt als Warnung (informativ).
func (s *Server) alertSoftwareChanges(ctx context.Context, device *model.Device, since time.Time) {
	cfg, err := s.store.GetAlertConfig(ctx)
	if err != nil || !cfg.Enabled || !cfg.AlertSoftware {
		return
	}
	events, err := s.store.SoftwareEventsSince(ctx, device.ID, since)
	if err != nil || len(events) == 0 {
		return
	}
	if inMaint, err := s.store.DeviceInMaintenance(ctx, device.ID, time.Now()); err == nil && inMaint {
		return
	}
	channels, err := s.store.ChannelsForDevice(ctx, device.ID)
	if err != nil || len(channels) == 0 {
		return
	}
	label := map[string]string{"added": "installiert", "removed": "entfernt", "updated": "aktualisiert"}
	subject := fmt.Sprintf("[PC-Inventar] Software-Änderung auf %s", device.Hostname)
	for _, ch := range channels {
		if severityRank("warning") < severityRank(ch.MinSeverity) {
			continue // Kanal will nur kritische Meldungen
		}
		var b strings.Builder
		fmt.Fprintf(&b, "Software-Änderungen auf Gerät %q:\n\n", device.Hostname)
		for _, ev := range events {
			ver := ev.Version
			if ev.Change == "updated" {
				ver = ev.OldVersion + " → " + ev.Version
			}
			fmt.Fprintf(&b, "  • [%s] %s %s\n", label[ev.Change], ev.Name, ver)
		}
		alert.Dispatch(s.log, ch, alert.Notification{Subject: subject, Body: b.String()})
	}
}

func (s *Server) alertTransitions(ctx context.Context, device *model.Device, events []model.CheckEvent) {
	cfg, err := s.store.GetAlertConfig(ctx)
	if err != nil || !cfg.Enabled {
		return
	}
	// Wartungsfenster aktiv? Dann Statuswechsel weiter protokollieren, aber nicht melden.
	if inMaint, err := s.store.DeviceInMaintenance(ctx, device.ID, time.Now()); err == nil && inMaint {
		return
	}
	channels, err := s.store.ChannelsForDevice(ctx, device.ID)
	if err != nil || len(channels) == 0 {
		return
	}
	// Nur „meldewürdige" Statuswechsel: nach failing/warning oder Wiederherstellung
	// aus einem dieser Zustände. unbekannt-Übergänge lösen keine Meldung aus.
	var notify []model.CheckEvent
	for _, ev := range events {
		switch {
		case ev.NewStatus == "failing" || ev.NewStatus == "warning":
			notify = append(notify, ev)
		case ev.NewStatus == "passing" && (ev.OldStatus == "failing" || ev.OldStatus == "warning"):
			notify = append(notify, ev)
		}
	}
	if len(notify) == 0 {
		return
	}
	ids := make([]string, 0, len(notify))
	for _, ev := range notify {
		ids = append(ids, ev.CheckID)
	}
	severities, _ := s.store.CheckSeverities(ctx, ids)

	subject := fmt.Sprintf("[PC-Inventar] Check-Meldung auf %s", device.Hostname)
	notifiedIDs := map[string]bool{}
	for _, ch := range channels {
		minRank := severityRank(ch.MinSeverity)
		var b strings.Builder
		fmt.Fprintf(&b, "Statuswechsel auf Gerät %q:\n\n", device.Hostname)
		matched := 0
		for _, ev := range notify {
			if severityRank(eventSeverity(ev, severities[ev.CheckID])) < minRank {
				continue // erfüllt den Mindest-Schweregrad dieses Kanals nicht
			}
			name := ev.CheckName
			if name == "" {
				name = ev.CheckID
			}
			fmt.Fprintf(&b, "  • [%s] %s: %s\n", eventLabel(ev.NewStatus), name, ev.Output)
			notifiedIDs[ev.ID] = true
			matched++
		}
		if matched == 0 {
			continue
		}
		alert.Dispatch(s.log, ch, alert.Notification{Subject: subject, Body: b.String()})
	}
	if len(notifiedIDs) > 0 {
		ids := make([]string, 0, len(notifiedIDs))
		for id := range notifiedIDs {
			ids = append(ids, id)
		}
		if err := s.store.MarkEventsNotified(ctx, ids, time.Now()); err != nil {
			s.log.Warn("ereignisse als benachrichtigt markieren", "err", err)
		}
	}
}
