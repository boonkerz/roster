package api

import (
	"context"
	"time"

	"github.com/boonkerz/roster/internal/server/model"
	"github.com/boonkerz/roster/internal/shared"
)

// Aufgeschobene Alarme: Hat ein Check beim Wechsel auf „failing" eine Auto-Remediation
// ausgelöst, wird der Alarm NICHT sofort verschickt. Erst wenn der Check nach der
// Remediation (Skript fertig bzw. Reboot-Schonfrist vorbei) immer noch fehlschlägt,
// geht die Meldung raus – mit dem Hinweis, dass die Selbstheilung nicht geholfen hat.
// Heilt sich das Gerät, bleibt es still; auch die „behoben"-Meldung entfällt, denn der
// Fehler wurde nie gemeldet. So gibt es je Störung eine Meldung statt drei.

const (
	// deferredGraceScript: nach Abschluss des Remediation-Skripts muss der Check noch
	// einmal gelaufen sein, bevor sein Ergebnis zählt (Ergebnisse kommen mit dem Checkin).
	deferredGraceScript = 90 * time.Second
	// deferredGraceReboot: ein Proxmox-Reboot liefert keinen Befehl, auf den man warten
	// könnte – fester Vorlauf, bis der Gast wieder oben ist.
	deferredGraceReboot = 5 * time.Minute
	// deferredMaxWait: spätestens dann wird gemeldet, auch wenn das Skript nie fertig
	// wurde (Agent hängt, Befehl verloren).
	deferredMaxWait = 2 * time.Hour
)

// remediationTrigger beschreibt eine gerade ausgelöste Remediation.
type remediationTrigger struct {
	event     model.CheckEvent
	commandID string // Remediation-Skript; leer = nur Proxmox-Reboot
}

// deferAlerts legt für ausgelöste Remediationen aufgeschobene Alarme an und liefert
// die Ereignis-IDs, die alertTransitions deshalb überspringen soll.
func (s *Server) deferAlerts(ctx context.Context, device *model.Device, triggers []remediationTrigger) map[string]bool {
	skip := map[string]bool{}
	if len(triggers) == 0 {
		return skip
	}
	// Einstellbar (Benachrichtigungen): aus = sofort melden wie früher, auch wenn
	// gerade eine Remediation läuft.
	if cfg, err := s.store.GetAlertConfig(ctx); err != nil || !cfg.AlertAfterRemediation {
		return skip
	}
	now := time.Now()
	for _, t := range triggers {
		notBefore := now.Add(deferredGraceReboot)
		if t.commandID != "" {
			notBefore = now.Add(deferredGraceScript)
		}
		d := &model.DeferredAlert{
			DeviceID: device.ID, CheckID: t.event.CheckID, CheckName: t.event.CheckName,
			EventID: t.event.ID, CommandID: t.commandID, NotBefore: notBefore, CreatedAt: now,
		}
		if err := s.store.UpsertDeferredAlert(ctx, d); err != nil {
			s.log.Warn("alarm aufschieben", "check", t.event.CheckID, "err", err)
			continue // dann lieber sofort melden als gar nicht
		}
		skip[t.event.ID] = true
	}
	return skip
}

// deferredAction ist die Entscheidung je offenem Alarm bei einem neuen Check-Ergebnis.
type deferredAction int

const (
	deferredKeep deferredAction = iota // weiter warten
	deferredDrop                       // Check ist wieder gut – still verwerfen
	deferredFire                       // immer noch kaputt – jetzt melden
)

// decideDeferred entscheidet rein aus Zustand und Zeit (testbar ohne Store).
// cmdDone/cmdRanAt: Stand des Remediation-Befehls (bei Proxmox-Reboot: done=true, nil).
func decideDeferred(d model.DeferredAlert, status string, cmdDone bool, cmdRanAt *time.Time, now time.Time) deferredAction {
	if status == "passing" {
		return deferredDrop
	}
	if status != "failing" && status != "warning" {
		return deferredKeep // unbekannt: abwarten
	}
	if now.Sub(d.CreatedAt) >= deferredMaxWait {
		return deferredFire
	}
	if !cmdDone || now.Before(d.NotBefore) {
		return deferredKeep
	}
	// Nach dem Skript muss der Check noch einmal gelaufen sein: ein Ergebnis, das
	// unmittelbar nach dem Befehl eintrifft, stammt meist noch von davor.
	if cmdRanAt != nil && now.Sub(*cmdRanAt) < deferredGraceScript {
		return deferredKeep
	}
	return deferredFire
}

// resolveDeferredAlerts prüft bei jedem Checkin die offenen aufgeschobenen Alarme des
// Geräts gegen die frischen Check-Ergebnisse.
func (s *Server) resolveDeferredAlerts(ctx context.Context, device *model.Device, results []shared.CheckResult) {
	pending, err := s.store.DeferredAlertsForDevice(ctx, device.ID)
	if err != nil || len(pending) == 0 {
		return
	}
	byCheck := map[string]shared.CheckResult{}
	for _, r := range results {
		byCheck[r.CheckID] = r
	}
	now := time.Now()
	var fire []model.CheckEvent
	for _, d := range pending {
		r, ok := byCheck[d.CheckID]
		if !ok {
			// Check nicht mehr gemeldet (entfernt/abgewählt) – nach der Höchstdauer aufgeben.
			if now.Sub(d.CreatedAt) >= deferredMaxWait {
				_ = s.store.DeleteDeferredAlert(ctx, d.ID)
			}
			continue
		}
		cmdDone, cmdRanAt := true, (*time.Time)(nil)
		if d.CommandID != "" {
			if c, cerr := s.store.CommandByID(ctx, d.CommandID); cerr == nil {
				cmdDone, cmdRanAt = c.Status == "done", c.RanAt
			}
		}
		switch decideDeferred(d, r.Status, cmdDone, cmdRanAt, now) {
		case deferredDrop:
			_ = s.store.DeleteDeferredAlert(ctx, d.ID)
			s.log.Info("auto-remediation erfolgreich – kein alarm", "device", device.Hostname, "check", d.CheckName)
		case deferredFire:
			_ = s.store.DeleteDeferredAlert(ctx, d.ID)
			name := d.CheckName
			fire = append(fire, model.CheckEvent{
				ID: d.EventID, DeviceID: device.ID, CheckID: d.CheckID, CheckName: name,
				OldStatus: "passing", NewStatus: r.Status,
				Output: r.Output + " – Auto-Remediation hat nicht geholfen",
			})
		}
	}
	if len(fire) > 0 {
		s.notifyCheckEvents(ctx, device, fire)
	}
}
