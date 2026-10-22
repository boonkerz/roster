package store

import (
	"context"
	"encoding/json"
	"time"
)

// UserPrefs liefert alle Einstellungen eines Benutzers (Schlüssel -> JSON-Wert).
func (s *Store) UserPrefs(ctx context.Context, userID string) (map[string]json.RawMessage, error) {
	rows, err := s.db.QueryContext(ctx, s.rebind(`SELECT key, value FROM user_prefs WHERE user_id=?`), userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]json.RawMessage{}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		out[k] = json.RawMessage(v)
	}
	return out, rows.Err()
}

// SetUserPref setzt (Upsert) eine Einstellung; value muss gültiges JSON sein.
func (s *Store) SetUserPref(ctx context.Context, userID, key string, value json.RawMessage) error {
	now := time.Now().UTC()
	res, err := s.db.ExecContext(ctx, s.rebind(`
		UPDATE user_prefs SET value=?, updated_at=? WHERE user_id=? AND key=?`), string(value), now, userID, key)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		_, err = s.db.ExecContext(ctx, s.rebind(`
			INSERT INTO user_prefs (user_id, key, value, updated_at) VALUES (?, ?, ?, ?)`), userID, key, string(value), now)
	}
	return err
}

// DeleteUserPref entfernt eine Einstellung (kein Fehler, wenn sie nicht existiert).
func (s *Store) DeleteUserPref(ctx context.Context, userID, key string) error {
	_, err := s.db.ExecContext(ctx, s.rebind(`DELETE FROM user_prefs WHERE user_id=? AND key=?`), userID, key)
	return err
}
