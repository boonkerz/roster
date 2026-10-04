package store

import (
	"context"
	"time"

	"github.com/boonkerz/roster/internal/server/model"
)

// UpsertDeferredAlert merkt einen aufgeschobenen Check-Alarm (eine Zeile je
// Gerät/Check; ein erneuter Fehlschlag ersetzt die alte Zeile).
func (s *Store) UpsertDeferredAlert(ctx context.Context, d *model.DeferredAlert) error {
	if d.ID == "" {
		d.ID = NewID()
	}
	if d.CreatedAt.IsZero() {
		d.CreatedAt = time.Now().UTC()
	}
	if _, err := s.db.ExecContext(ctx, s.rebind(
		`DELETE FROM deferred_check_alerts WHERE device_id=? AND check_id=?`), d.DeviceID, d.CheckID); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, s.rebind(`
		INSERT INTO deferred_check_alerts (id, device_id, check_id, check_name, event_id, command_id, not_before, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`),
		d.ID, d.DeviceID, d.CheckID, d.CheckName, d.EventID, d.CommandID, d.NotBefore.UTC(), d.CreatedAt.UTC())
	return err
}

// DeferredAlertsForDevice liefert die offenen aufgeschobenen Alarme eines Geräts.
func (s *Store) DeferredAlertsForDevice(ctx context.Context, deviceID string) ([]model.DeferredAlert, error) {
	rows, err := s.db.QueryContext(ctx, s.rebind(`
		SELECT id, device_id, check_id, check_name, event_id, command_id, not_before, created_at
		FROM deferred_check_alerts WHERE device_id=? ORDER BY created_at`), deviceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.DeferredAlert
	for rows.Next() {
		var d model.DeferredAlert
		if err := rows.Scan(&d.ID, &d.DeviceID, &d.CheckID, &d.CheckName, &d.EventID, &d.CommandID, &d.NotBefore, &d.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// DeleteDeferredAlert entfernt einen aufgeschobenen Alarm (gemeldet oder erledigt).
func (s *Store) DeleteDeferredAlert(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, s.rebind(`DELETE FROM deferred_check_alerts WHERE id=?`), id)
	return err
}

// DeleteDeferredAlertFor entfernt den aufgeschobenen Alarm eines Gerät/Check-Paars
// und meldet, ob es einen gab (→ Wiederherstellung still schlucken).
func (s *Store) DeleteDeferredAlertFor(ctx context.Context, deviceID, checkID string) (bool, error) {
	res, err := s.db.ExecContext(ctx, s.rebind(
		`DELETE FROM deferred_check_alerts WHERE device_id=? AND check_id=?`), deviceID, checkID)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}
