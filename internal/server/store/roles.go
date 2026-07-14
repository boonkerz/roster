package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/boonkerz/roster/internal/server/model"
)

// Custom-Rollen: wiederverwendbare Rechte-Sets. permissions wird als JSON-Array
// gespeichert.

// ListCustomRoles liefert alle Custom-Rollen inkl. Anzahl zugeordneter Benutzer.
func (s *Store) ListCustomRoles(ctx context.Context) ([]model.CustomRole, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT r.id, r.name, r.permissions, r.created_at,
		       (SELECT COUNT(*) FROM users u WHERE u.custom_role_id = r.id)
		FROM custom_roles r ORDER BY r.name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.CustomRole
	for rows.Next() {
		var r model.CustomRole
		var perms string
		if err := rows.Scan(&r.ID, &r.Name, &perms, &r.CreatedAt, &r.UserCount); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(perms), &r.Permissions)
		if r.Permissions == nil {
			r.Permissions = []string{}
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// CustomRolePermissions liefert das Permission-Set einer Custom-Rolle (nil, wenn
// die ID leer/unbekannt ist).
func (s *Store) CustomRolePermissions(ctx context.Context, roleID string) ([]string, error) {
	if roleID == "" {
		return nil, nil
	}
	var perms string
	err := s.db.QueryRowContext(ctx, s.rebind(`SELECT permissions FROM custom_roles WHERE id = ?`), roleID).Scan(&perms)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []string
	_ = json.Unmarshal([]byte(perms), &out)
	return out, nil
}

// CreateCustomRole legt eine Custom-Rolle an. permissions wird auf gültige Keys gefiltert.
func (s *Store) CreateCustomRole(ctx context.Context, name string, permissions []string) (*model.CustomRole, error) {
	r := &model.CustomRole{ID: NewID(), Name: name, Permissions: filterPerms(permissions), CreatedAt: time.Now().UTC()}
	pj, _ := json.Marshal(r.Permissions)
	_, err := s.db.ExecContext(ctx, s.rebind(`INSERT INTO custom_roles (id, name, permissions, created_at) VALUES (?, ?, ?, ?)`),
		r.ID, r.Name, string(pj), r.CreatedAt)
	if err != nil {
		return nil, err
	}
	return r, nil
}

// UpdateCustomRole ändert Name und Rechte einer Custom-Rolle.
func (s *Store) UpdateCustomRole(ctx context.Context, id, name string, permissions []string) error {
	pj, _ := json.Marshal(filterPerms(permissions))
	res, err := s.db.ExecContext(ctx, s.rebind(`UPDATE custom_roles SET name = ?, permissions = ? WHERE id = ?`),
		name, string(pj), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteCustomRole entfernt eine Custom-Rolle (zugeordnete Benutzer fallen auf ihre
// eingebaute Rolle zurück – FK ON DELETE SET NULL).
func (s *Store) DeleteCustomRole(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, s.rebind(`DELETE FROM custom_roles WHERE id = ?`), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// filterPerms behält nur bekannte Permission-Keys (verhindert Müll in der DB).
func filterPerms(in []string) []string {
	out := []string{}
	for _, p := range in {
		if model.HasPermission(model.AllPermissions, p) && !model.HasPermission(out, p) {
			out = append(out, p)
		}
	}
	return out
}
