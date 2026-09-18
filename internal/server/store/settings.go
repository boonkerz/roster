package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// Globale Einstellungen als Schlüssel-Wert-Paare (Tabelle app_settings). Werte sind
// immer Text; die Auswertung macht der Aufrufer.

// SettingTimezone ist der IANA-Name der Zeitzone, in der geplant wird (Backups auf dem
// Server, Tasks/Checks auf den Agenten). Leer = Systemzeit des jeweiligen Geräts.
const SettingTimezone = "timezone"

// GetSetting liefert den gespeicherten Wert oder "" wenn der Schlüssel fehlt.
func (s *Store) GetSetting(ctx context.Context, key string) (string, error) {
	var v string
	err := s.db.QueryRowContext(ctx, s.rebind(`SELECT value FROM app_settings WHERE key=?`), key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return v, err
}

// SetSetting speichert einen Wert (Upsert – dialektneutral wie SaveAlertConfig).
func (s *Store) SetSetting(ctx context.Context, key, value string) error {
	now := time.Now().UTC()
	res, err := s.db.ExecContext(ctx, s.rebind(`UPDATE app_settings SET value=?, updated_at=? WHERE key=?`), value, now, key)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		_, err = s.db.ExecContext(ctx, s.rebind(`INSERT INTO app_settings (key, value, updated_at) VALUES (?, ?, ?)`), key, value, now)
	}
	return err
}
