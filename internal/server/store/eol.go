package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/boonkerz/roster/internal/server/model"
)

// Einstellungs-Schlüssel der EOL-Überwachung (app_settings).
const (
	SettingEOLWarnDays = "eol_warn_days" // Vorwarnzeit in Tagen (Standard 90)
	SettingEOLAlerts   = "eol_alerts"    // "0" = keine Alarme, sonst an
)

// EOLCache liefert die gecachten Zyklen eines Produkts (JSON) samt Abrufzeit.
func (s *Store) EOLCache(ctx context.Context, product string) (data string, fetchedAt time.Time, err error) {
	err = s.db.QueryRowContext(ctx, s.rebind(`SELECT data, fetched_at FROM eol_cache WHERE product=?`), product).Scan(&data, &fetchedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return "", time.Time{}, nil
	}
	return data, fetchedAt, err
}

// SetEOLCache speichert die Zyklen eines Produkts.
func (s *Store) SetEOLCache(ctx context.Context, product, data string) error {
	now := time.Now().UTC()
	res, err := s.db.ExecContext(ctx, s.rebind(`UPDATE eol_cache SET data=?, fetched_at=? WHERE product=?`), data, now, product)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		_, err = s.db.ExecContext(ctx, s.rebind(`INSERT INTO eol_cache (product, data, fetched_at) VALUES (?, ?, ?)`), product, data, now)
	}
	return err
}

// UpsertDeviceEOL schreibt die Bewertung eines Geräts; liefert den vorherigen
// gemeldeten Status (alerted_status), damit der Aufrufer Statuswechsel erkennt.
func (s *Store) UpsertDeviceEOL(ctx context.Context, e model.DeviceEOL) (prevAlerted string, err error) {
	_ = s.db.QueryRowContext(ctx, s.rebind(`SELECT alerted_status FROM device_eol WHERE device_id=? AND kind=?`), e.DeviceID, e.Kind).Scan(&prevAlerted)
	now := time.Now().UTC()
	res, err := s.db.ExecContext(ctx, s.rebind(`
		UPDATE device_eol SET product=?, cycle=?, label=?, eol_date=?, support_date=?, status=?, checked_at=?
		WHERE device_id=? AND kind=?`),
		e.Product, e.Cycle, e.Label, e.EOLDate, e.SupportDate, e.Status, now, e.DeviceID, e.Kind)
	if err != nil {
		return prevAlerted, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		_, err = s.db.ExecContext(ctx, s.rebind(`
			INSERT INTO device_eol (device_id, kind, product, cycle, label, eol_date, support_date, status, checked_at, alerted_status)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, '')`),
			e.DeviceID, e.Kind, e.Product, e.Cycle, e.Label, e.EOLDate, e.SupportDate, e.Status, now)
	}
	return prevAlerted, err
}

// MarkEOLAlerted merkt sich den zuletzt gemeldeten Status (keine Doppelmeldung).
func (s *Store) MarkEOLAlerted(ctx context.Context, deviceID, kind, status string) error {
	_, err := s.db.ExecContext(ctx, s.rebind(`UPDATE device_eol SET alerted_status=? WHERE device_id=? AND kind=?`), status, deviceID, kind)
	return err
}

// DeleteDeviceEOL entfernt eine Bewertung (z. B. Rolling Release, kein Zyklus mehr zuordenbar).
func (s *Store) DeleteDeviceEOL(ctx context.Context, deviceID, kind string) error {
	_, err := s.db.ExecContext(ctx, s.rebind(`DELETE FROM device_eol WHERE device_id=? AND kind=?`), deviceID, kind)
	return err
}

const eolCols = `e.device_id, d.hostname, d.os, d.os_version, COALESCE(c.name,''), COALESCE(st.name,''),
	e.kind, e.product, e.cycle, e.label, e.eol_date, e.support_date, e.status, e.checked_at`

func scanEOL(rows *sql.Rows) (model.DeviceEOL, error) {
	var e model.DeviceEOL
	var eol, sup sql.NullTime
	err := rows.Scan(&e.DeviceID, &e.Hostname, &e.OS, &e.OSVersion, &e.ClientName, &e.SiteName,
		&e.Kind, &e.Product, &e.Cycle, &e.Label, &eol, &sup, &e.Status, &e.CheckedAt)
	if eol.Valid {
		t := eol.Time
		e.EOLDate = &t
	}
	if sup.Valid {
		t := sup.Time
		e.SupportDate = &t
	}
	return e, err
}

// DeviceEOLFor liefert die Bewertungen eines Geräts (os zuerst).
func (s *Store) DeviceEOLFor(ctx context.Context, deviceID string) ([]model.DeviceEOL, error) {
	rows, err := s.db.QueryContext(ctx, s.rebind(`SELECT `+eolCols+`
		FROM device_eol e JOIN devices d ON d.id = e.device_id
		LEFT JOIN sites st ON st.id = d.site_id LEFT JOIN clients c ON c.id = st.client_id
		WHERE e.device_id=? ORDER BY CASE e.kind WHEN 'os' THEN 0 ELSE 1 END`), deviceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.DeviceEOL{}
	for rows.Next() {
		e, err := scanEOL(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// EOLFleet liefert alle Bewertungen (Scope), früheste Support-Enden zuerst.
func (s *Store) EOLFleet(ctx context.Context, allowed map[string]bool) ([]model.DeviceEOL, error) {
	scope, args, empty := scopeWhere(allowed)
	if empty {
		return []model.DeviceEOL{}, nil
	}
	rows, err := s.db.QueryContext(ctx, s.rebind(`SELECT `+eolCols+`
		FROM device_eol e JOIN devices d ON d.id = e.device_id
		LEFT JOIN sites st ON st.id = d.site_id LEFT JOIN clients c ON c.id = st.client_id
		WHERE d.revoked = FALSE`+scope+`
		ORDER BY CASE e.status WHEN 'eol' THEN 0 WHEN 'soon' THEN 1 WHEN 'ok' THEN 2 ELSE 3 END, e.eol_date, d.hostname`), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.DeviceEOL{}
	for rows.Next() {
		e, err := scanEOL(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
