package store

import (
	"context"
	"database/sql"

	"github.com/boonkerz/roster/internal/server/model"
	"github.com/boonkerz/roster/internal/shared"
)

// ReplaceLogSourceStatus ersetzt den vom Agent gemeldeten Lesestatus der Log-Quellen.
func (s *Store) ReplaceLogSourceStatus(ctx context.Context, deviceID string, status []shared.LogSourceStatus) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	if _, err := tx.ExecContext(ctx, s.rebind(`DELETE FROM device_log_sources WHERE device_id=?`), deviceID); err != nil {
		return err
	}
	for _, st := range status {
		var lastEvent, lastRun any
		if !st.LastEvent.IsZero() {
			lastEvent = st.LastEvent.UTC()
		}
		if !st.LastRun.IsZero() {
			lastRun = st.LastRun.UTC()
		}
		if _, err := tx.ExecContext(ctx, s.rebind(`
			INSERT INTO device_log_sources (device_id, source_id, name, type, target, targets, events, last_event_at, last_run_at, error)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`),
			deviceID, st.ID, st.Name, st.Type, st.Target, st.Targets, st.Events, lastEvent, lastRun, truncate(st.Error, 500)); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// LogSourceStatusFor liefert den gemeldeten Status je Quelle (Schlüssel = Quell-ID).
func (s *Store) LogSourceStatusFor(ctx context.Context, deviceID string) (map[string]model.DeviceLogSource, error) {
	rows, err := s.db.QueryContext(ctx, s.rebind(`
		SELECT source_id, name, type, target, targets, events, last_event_at, last_run_at, error
		FROM device_log_sources WHERE device_id=?`), deviceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]model.DeviceLogSource{}
	for rows.Next() {
		var d model.DeviceLogSource
		var le, lr sql.NullTime
		if err := rows.Scan(&d.ID, &d.Name, &d.Type, &d.Target, &d.Targets, &d.Events, &le, &lr, &d.Error); err != nil {
			return nil, err
		}
		if le.Valid {
			t := le.Time
			d.LastEventAt = &t
		}
		if lr.Valid {
			t := lr.Time
			d.LastRunAt = &t
		}
		d.Reported = true
		out[d.ID] = d
	}
	return out, rows.Err()
}
