package store

import (
	"context"
	"strings"
	"time"

	"github.com/boonkerz/roster/internal/server/model"
	"github.com/boonkerz/roster/internal/shared"
)

// InsertFIMEvents speichert gemeldete Datei-Änderungen eines Geräts.
func (s *Store) InsertFIMEvents(ctx context.Context, deviceID string, events []shared.FIMEvent) error {
	if len(events) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	for _, e := range events {
		ts := e.Time
		if ts.IsZero() {
			ts = time.Now()
		}
		if _, err := tx.ExecContext(ctx, s.rebind(`
			INSERT INTO fim_events (id, device_id, ts, rule_id, path, change, old_hash, new_hash, old_mode, new_mode, old_owner, new_owner, size, alert, acknowledged, who_user, who_process)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`),
			NewID(), deviceID, ts.UTC(), e.RuleID, e.Path, e.Change, e.OldHash, e.NewHash, e.OldMode, e.NewMode, e.OldOwner, e.NewOwner, e.Size, e.Alert,
			e.Change == "baseline" || e.Change == "limit", truncate(e.User, 120), truncate(e.Process, 200)); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// FIMFilter grenzt die Ereignisliste ein.
type FIMFilter struct {
	Allowed  map[string]bool
	DeviceID string
	ClientID string
	SiteID   string
	Since    time.Time
	Change   string
	Query    string
	OpenOnly bool
	Limit    int
}

const fimCols = `f.id, f.device_id, d.hostname, COALESCE(c.name,''), f.ts, f.rule_id, f.path, f.change,
	f.old_hash, f.new_hash, f.old_mode, f.new_mode, f.old_owner, f.new_owner, f.size, f.alert, f.acknowledged, f.who_user, f.who_process`

// FIMEvents liefert Datei-Änderungen (neueste zuerst).
func (s *Store) FIMEvents(ctx context.Context, f FIMFilter) ([]model.FIMEvent, error) {
	scope, args, empty := scopeWhere(f.Allowed)
	if empty {
		return []model.FIMEvent{}, nil
	}
	q := `SELECT ` + fimCols + ` FROM fim_events f JOIN devices d ON d.id = f.device_id
		LEFT JOIN sites st ON st.id = d.site_id LEFT JOIN clients c ON c.id = st.client_id
		WHERE d.revoked = FALSE` + scope
	if f.DeviceID != "" {
		q += ` AND f.device_id = ?`
		args = append(args, f.DeviceID)
	}
	if f.ClientID != "" {
		q += ` AND st.client_id = ?`
		args = append(args, f.ClientID)
	}
	if f.SiteID != "" {
		q += ` AND d.site_id = ?`
		args = append(args, f.SiteID)
	}
	if !f.Since.IsZero() {
		q += ` AND f.ts >= ?`
		args = append(args, f.Since.UTC())
	}
	if f.Change != "" {
		q += ` AND f.change = ?`
		args = append(args, f.Change)
	}
	if f.OpenOnly {
		q += ` AND f.acknowledged = FALSE`
	}
	if t := strings.TrimSpace(f.Query); t != "" {
		like := "%" + strings.ToLower(t) + "%"
		q += ` AND (LOWER(f.path) LIKE ? OR LOWER(d.hostname) LIKE ?)`
		args = append(args, like, like)
	}
	limit := f.Limit
	if limit <= 0 || limit > 5000 {
		limit = 1000
	}
	q += ` ORDER BY f.ts DESC, f.path LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, s.rebind(q), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.FIMEvent{}
	for rows.Next() {
		var e model.FIMEvent
		if err := rows.Scan(&e.ID, &e.DeviceID, &e.Hostname, &e.ClientName, &e.TS, &e.RuleID, &e.Path, &e.Change,
			&e.OldHash, &e.NewHash, &e.OldMode, &e.NewMode, &e.OldOwner, &e.NewOwner, &e.Size, &e.Alert, &e.Acknowledged, &e.User, &e.Process); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// AckFIMEvents bestätigt Ereignisse (nur innerhalb des Scopes). Liefert die Anzahl.
func (s *Store) AckFIMEvents(ctx context.Context, ids []string, allowed map[string]bool) (int, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	scope, sargs, empty := scopeWhere(allowed)
	if empty {
		return 0, nil
	}
	marks, args := placeholders(ids)
	q := `UPDATE fim_events SET acknowledged = TRUE WHERE id IN (` + marks + `)`
	if scope != "" {
		q += ` AND device_id IN (SELECT d.id FROM devices d WHERE 1=1` + scope + `)`
		args = append(args, sargs...)
	}
	res, err := s.db.ExecContext(ctx, s.rebind(q), args...)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// PruneFIMEvents löscht Ereignisse älter als cutoff.
func (s *Store) PruneFIMEvents(ctx context.Context, cutoff time.Time) error {
	_, err := s.db.ExecContext(ctx, s.rebind(`DELETE FROM fim_events WHERE ts < ?`), cutoff.UTC())
	return err
}
