package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/thomaspeterson/pc-inventory/internal/server/model"
)

// CreateGroup legt eine Gruppe an.
func (s *Store) CreateGroup(ctx context.Context, g *model.Group) error {
	_, err := s.db.ExecContext(ctx, s.rebind(`
		INSERT INTO groups (id, name, description, parent_id, rule) VALUES (?, ?, ?, ?, ?)`),
		g.ID, g.Name, g.Description, nullString(g.ParentID), g.Rule)
	return err
}

// UpdateGroup ändert Name/Beschreibung/Elterngruppe/Regel.
func (s *Store) UpdateGroup(ctx context.Context, g *model.Group) error {
	res, err := s.db.ExecContext(ctx, s.rebind(`
		UPDATE groups SET name=?, description=?, parent_id=?, rule=? WHERE id=?`),
		g.Name, g.Description, nullString(g.ParentID), g.Rule, g.ID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteGroup entfernt eine Gruppe (Zuordnungen werden per FK gelöst).
func (s *Store) DeleteGroup(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, s.rebind(`DELETE FROM groups WHERE id = ?`), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// ListGroups liefert alle Gruppen inkl. Geräteanzahl. Bei Smart Groups wird die
// Anzahl dynamisch aus der Regel ermittelt (offlineCutoff für Status-Bedingungen).
func (s *Store) ListGroups(ctx context.Context, offlineCutoff time.Time) ([]model.Group, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT g.id, g.name, g.description, g.parent_id,
			(SELECT COUNT(*) FROM device_groups dg WHERE dg.group_id = g.id), g.rule
		FROM groups g ORDER BY g.name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Group
	for rows.Next() {
		g, err := scanGroup(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *g)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		if where, args, ok := smartWhere(out[i].Rule, offlineCutoff); ok {
			var n int
			q := "SELECT COUNT(*) FROM devices d WHERE d.revoked=0 AND " + where
			if err := s.db.QueryRowContext(ctx, s.rebind(q), args...).Scan(&n); err == nil {
				out[i].DeviceCount = n
			}
		}
	}
	return out, nil
}

// SetDeviceGroups ersetzt die Gruppenzugehörigkeit eines Geräts.
func (s *Store) SetDeviceGroups(ctx context.Context, deviceID string, groupIDs []string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	if _, err := tx.ExecContext(ctx, s.rebind(`DELETE FROM device_groups WHERE device_id = ?`), deviceID); err != nil {
		return err
	}
	for _, gid := range groupIDs {
		if _, err := tx.ExecContext(ctx, s.rebind(`
			INSERT INTO device_groups (device_id, group_id) VALUES (?, ?)`), deviceID, gid); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) groupsForDevice(ctx context.Context, deviceID string) ([]model.Group, error) {
	rows, err := s.db.QueryContext(ctx, s.rebind(`
		SELECT g.id, g.name, g.description, g.parent_id, 0, g.rule
		FROM groups g JOIN device_groups dg ON dg.group_id = g.id
		WHERE dg.device_id = ? ORDER BY g.name`), deviceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Group
	for rows.Next() {
		g, err := scanGroup(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *g)
	}
	return out, rows.Err()
}

func scanGroup(row scanner) (*model.Group, error) {
	var g model.Group
	var parent sql.NullString
	if err := row.Scan(&g.ID, &g.Name, &g.Description, &parent, &g.DeviceCount, &g.Rule); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if parent.Valid {
		p := parent.String
		g.ParentID = &p
	}
	return &g, nil
}

func nullString(p *string) any {
	if p == nil || *p == "" {
		return nil
	}
	return *p
}
