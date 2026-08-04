package api

import (
	"context"
	"fmt"
	"time"

	"github.com/boonkerz/roster/internal/server/alert"
	"github.com/boonkerz/roster/internal/server/model"
)

// RunOfflineLoop meldet einmalig, wenn ein verwaltetes Gerät die Offline-Schwelle
// überschreitet – ein Fall, der sonst nirgends alarmiert wird – und einmalig die
// Rückkehr online. Der „bereits gemeldet"-Zustand wird nur im Prozess gehalten
// (kein Dauerfeuer). Nach einem Neustart wird ein weiterhin offline gebliebenes
// Gerät einmal erneut gemeldet (bewusst, als Erinnerung).
func (s *Server) RunOfflineLoop(ctx context.Context) {
	alerted := map[string]bool{} // deviceID -> bereits als offline gemeldet
	tick := func() {
		cfg, err := s.store.GetAlertConfig(ctx)
		if err != nil || !cfg.Enabled {
			return
		}
		devices, err := s.store.ListDevices(ctx, nil)
		if err != nil {
			return
		}
		for i := range devices {
			d := &devices[i]
			switch s.computeStatus(d) {
			case "offline":
				if !alerted[d.ID] && s.notifyOffline(ctx, d, true) {
					alerted[d.ID] = true
				}
			case "online":
				if alerted[d.ID] {
					s.notifyOffline(ctx, d, false)
					delete(alerted, d.ID)
				}
			}
		}
	}
	tick()
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			tick()
		}
	}
}

// notifyOffline verschickt die Offline- bzw. Recovery-Meldung an die zutreffenden
// Kanäle (respektiert Wartungsfenster und Mindest-Schweregrad wie die übrigen
// Dispatch-Pfade). Liefert true, wenn mindestens ein Kanal beliefert wurde.
func (s *Server) notifyOffline(ctx context.Context, d *model.Device, offline bool) bool {
	if inMaint, err := s.store.DeviceInMaintenance(ctx, d.ID, time.Now()); err == nil && inMaint {
		return false
	}
	channels, err := s.store.ChannelsForDevice(ctx, d.ID)
	if err != nil || len(channels) == 0 {
		return false
	}
	// Offline gilt als kritisch, die Rückkehr als Warnung.
	sev := "critical"
	var subject, body string
	if offline {
		last := "unbekannt"
		if d.LastSeen != nil {
			last = d.LastSeen.Format("2006-01-02 15:04:05")
		}
		subject = fmt.Sprintf("[Roster] Gerät offline: %s", d.Hostname)
		body = fmt.Sprintf("Gerät %q ist offline (zuletzt gesehen: %s).", d.Hostname, last)
	} else {
		sev = "warning"
		subject = fmt.Sprintf("[Roster] Gerät wieder online: %s", d.Hostname)
		body = fmt.Sprintf("Gerät %q ist wieder online.", d.Hostname)
	}
	sent := false
	for _, ch := range channels {
		if severityRank(sev) < severityRank(ch.MinSeverity) {
			continue // Kanal will nur höhere Schweregrade
		}
		alert.Dispatch(s.log, ch, alert.Notification{Subject: subject, Body: body})
		sent = true
	}
	return sent
}
