package store

import (
	"context"
	"database/sql"
	"time"

	"github.com/boonkerz/roster/internal/server/model"
)

// CreateUserAPIToken speichert ein User-API-Token (bereits gehasht).
func (s *Store) CreateUserAPIToken(ctx context.Context, t *model.UserAPIToken, tokenHash string) error {
	if t.CreatedAt.IsZero() {
		t.CreatedAt = time.Now().UTC()
	}
	var exp any
	if t.ExpiresAt != nil {
		exp = t.ExpiresAt.UTC()
	}
	_, err := s.db.ExecContext(ctx, s.rebind(`
		INSERT INTO user_api_tokens (id, user_id, label, token_hash, created_at, expires_at, revoked)
		VALUES (?, ?, ?, ?, ?, ?, FALSE)`),
		t.ID, t.UserID, t.Label, tokenHash, t.CreatedAt, exp)
	return err
}

// UserByAPIToken liefert den Benutzer zu einem gültigen (nicht widerrufenen, nicht
// abgelaufenen) API-Token-Hash und aktualisiert last_used_at (best effort).
func (s *Store) UserByAPIToken(ctx context.Context, tokenHash string) (*model.User, error) {
	row := s.db.QueryRowContext(ctx, s.rebind(`
		SELECT u.id, u.username, u.email, u.password_hash, u.role, u.custom_role_id, u.auth_source, u.theme, u.created_at, u.last_login, u.totp_secret, u.totp_enabled
		FROM user_api_tokens t JOIN users u ON u.id = t.user_id
		WHERE t.token_hash = ? AND t.revoked = FALSE AND (t.expires_at IS NULL OR t.expires_at > ?)`),
		tokenHash, time.Now().UTC())
	u, err := scanUser(row)
	if err != nil {
		return nil, err
	}
	// Zeitstempel der letzten Nutzung nachziehen – Fehler bewusst ignoriert.
	_, _ = s.db.ExecContext(ctx, s.rebind(`UPDATE user_api_tokens SET last_used_at = ? WHERE token_hash = ?`),
		time.Now().UTC(), tokenHash)
	return u, nil
}

// ListUserAPITokens liefert die Tokens eines Benutzers (ohne Klartext/Hash).
func (s *Store) ListUserAPITokens(ctx context.Context, userID string) ([]model.UserAPIToken, error) {
	rows, err := s.db.QueryContext(ctx, s.rebind(`
		SELECT id, user_id, label, created_at, last_used_at, expires_at, revoked
		FROM user_api_tokens WHERE user_id = ? AND revoked = FALSE ORDER BY created_at DESC`), userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.UserAPIToken
	for rows.Next() {
		var t model.UserAPIToken
		var lastUsed, exp sql.NullTime
		if err := rows.Scan(&t.ID, &t.UserID, &t.Label, &t.CreatedAt, &lastUsed, &exp, &t.Revoked); err != nil {
			return nil, err
		}
		if lastUsed.Valid {
			v := lastUsed.Time
			t.LastUsedAt = &v
		}
		if exp.Valid {
			v := exp.Time
			t.ExpiresAt = &v
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// RevokeUserAPIToken widerruft ein Token des Benutzers (nur eigene Tokens).
func (s *Store) RevokeUserAPIToken(ctx context.Context, id, userID string) error {
	res, err := s.db.ExecContext(ctx, s.rebind(`
		UPDATE user_api_tokens SET revoked = TRUE WHERE id = ? AND user_id = ?`), id, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}
