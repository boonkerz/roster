package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/boonkerz/roster/internal/server/model"
)

// ErrNotFound wird zurückgegeben, wenn ein Datensatz nicht existiert.
var ErrNotFound = errors.New("nicht gefunden")

// CountUsers zählt alle Benutzer (für den Seed-Admin-Check).
func (s *Store) CountUsers(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&n)
	return n, err
}

// CreateUser legt einen Benutzer an.
func (s *Store) CreateUser(ctx context.Context, u *model.User) error {
	if u.CreatedAt.IsZero() {
		u.CreatedAt = time.Now().UTC()
	}
	_, err := s.db.ExecContext(ctx, s.rebind(`
		INSERT INTO users (id, username, email, password_hash, role, custom_role_id, auth_source, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`),
		u.ID, u.Username, u.Email, u.PasswordHash, string(u.Role), nullStr(u.CustomRoleID), string(u.AuthSource), u.CreatedAt)
	return err
}

// UpdateUserRole setzt Rolle und (optionale) Custom-Rolle eines Benutzers.
func (s *Store) UpdateUserRole(ctx context.Context, id string, role model.Role, customRoleID string) error {
	res, err := s.db.ExecContext(ctx, s.rebind(`UPDATE users SET role = ?, custom_role_id = ? WHERE id = ?`),
		string(role), nullStr(customRoleID), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// nullStr wandelt einen leeren String in SQL-NULL (für optionale FK-Spalten).
func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// GetUserByUsername lädt einen Benutzer anhand des Login-Namens.
func (s *Store) GetUserByUsername(ctx context.Context, username string) (*model.User, error) {
	row := s.db.QueryRowContext(ctx, s.rebind(`
		SELECT id, username, email, password_hash, role, custom_role_id, auth_source, theme, created_at, last_login, totp_secret, totp_enabled
		FROM users WHERE username = ?`), username)
	return scanUser(row)
}

// GetUserByID lädt einen Benutzer anhand der ID.
func (s *Store) GetUserByID(ctx context.Context, id string) (*model.User, error) {
	row := s.db.QueryRowContext(ctx, s.rebind(`
		SELECT id, username, email, password_hash, role, custom_role_id, auth_source, theme, created_at, last_login, totp_secret, totp_enabled
		FROM users WHERE id = ?`), id)
	return scanUser(row)
}

// ListUsers liefert alle Benutzer (ohne Passwort-Hash im JSON).
func (s *Store) ListUsers(ctx context.Context) ([]model.User, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, username, email, password_hash, role, custom_role_id, auth_source, theme, created_at, last_login, totp_secret, totp_enabled
		FROM users ORDER BY username`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *u)
	}
	return out, rows.Err()
}

// CountAdmins zählt die Benutzer mit Admin-Rolle (für den Last-Admin-Schutz).
func (s *Store) CountAdmins(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, s.rebind(`SELECT COUNT(*) FROM users WHERE role = ?`), string(model.RoleAdmin)).Scan(&n)
	return n, err
}

// DeleteUser entfernt einen Benutzer samt seiner Sessions und Scope-Zuordnungen.
func (s *Store) DeleteUser(ctx context.Context, id string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, q := range []string{
		`DELETE FROM sessions WHERE user_id = ?`,
		`DELETE FROM user_scopes WHERE user_id = ?`,
	} {
		if _, err := tx.ExecContext(ctx, s.rebind(q), id); err != nil {
			return err
		}
	}
	res, err := tx.ExecContext(ctx, s.rebind(`DELETE FROM users WHERE id = ?`), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return tx.Commit()
}

// SetUserPassword setzt den Passwort-Hash eines Benutzers (z.B. CLI-Reset).
func (s *Store) SetUserPassword(ctx context.Context, id, passwordHash string) error {
	res, err := s.db.ExecContext(ctx, s.rebind(`UPDATE users SET password_hash = ? WHERE id = ?`), passwordHash, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// UpdateUserTheme speichert die Theme-Präferenz eines Benutzers.
func (s *Store) UpdateUserTheme(ctx context.Context, id, theme string) error {
	res, err := s.db.ExecContext(ctx, s.rebind(`UPDATE users SET theme = ? WHERE id = ?`), theme, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// UpdateLastLogin setzt den Zeitpunkt der letzten Anmeldung.
func (s *Store) UpdateLastLogin(ctx context.Context, id string, t time.Time) error {
	_, err := s.db.ExecContext(ctx, s.rebind(`UPDATE users SET last_login = ? WHERE id = ?`), t.UTC(), id)
	return err
}

type scanner interface {
	Scan(dest ...any) error
}

func scanUser(row scanner) (*model.User, error) {
	var u model.User
	var role, src string
	var customRoleID sql.NullString
	var lastLogin sql.NullTime
	err := row.Scan(&u.ID, &u.Username, &u.Email, &u.PasswordHash, &role, &customRoleID, &src, &u.Theme, &u.CreatedAt, &lastLogin, &u.TOTPSecret, &u.TOTPEnabled)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	u.Role = model.Role(role)
	u.CustomRoleID = customRoleID.String
	u.AuthSource = model.AuthSource(src)
	if lastLogin.Valid {
		t := lastLogin.Time
		u.LastLogin = &t
	}
	return &u, nil
}

// --- Sessions ---

// CreateSession speichert eine Session (token bereits gehasht).
func (s *Store) CreateSession(ctx context.Context, tokenHash, userID string, expires time.Time) error {
	_, err := s.db.ExecContext(ctx, s.rebind(`
		INSERT INTO sessions (token_hash, user_id, created_at, expires_at) VALUES (?, ?, ?, ?)`),
		tokenHash, userID, time.Now().UTC(), expires.UTC())
	return err
}

// UserBySession liefert den Benutzer zu einem gültigen, nicht abgelaufenen Session-Token-Hash.
func (s *Store) UserBySession(ctx context.Context, tokenHash string) (*model.User, error) {
	row := s.db.QueryRowContext(ctx, s.rebind(`
		SELECT u.id, u.username, u.email, u.password_hash, u.role, u.custom_role_id, u.auth_source, u.theme, u.created_at, u.last_login, u.totp_secret, u.totp_enabled
		FROM sessions sess JOIN users u ON u.id = sess.user_id
		WHERE sess.token_hash = ? AND sess.expires_at > ?`), tokenHash, time.Now().UTC())
	return scanUser(row)
}

// DeleteSession meldet eine Session ab.
func (s *Store) DeleteSession(ctx context.Context, tokenHash string) error {
	_, err := s.db.ExecContext(ctx, s.rebind(`DELETE FROM sessions WHERE token_hash = ?`), tokenHash)
	return err
}
