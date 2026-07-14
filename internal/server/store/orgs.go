package store

import (
	"context"
	"database/sql"

	"github.com/boonkerz/roster/internal/server/model"
)

// ClientTree liefert alle Clients mit ihren Sites und Geräteanzahlen. allowed begrenzt
// (falls nicht nil) auf die sichtbaren Standort-IDs; Clients ohne sichtbaren Standort
// werden weggelassen (Daten-Scope). nil = unbeschränkt.
func (s *Store) ClientTree(ctx context.Context, allowed map[string]bool) ([]model.Client, error) {
	clients, err := s.listClients(ctx)
	if err != nil {
		return nil, err
	}
	byID := map[string]*model.Client{}
	for i := range clients {
		byID[clients[i].ID] = &clients[i]
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT s.id, s.client_id, s.name,
			(SELECT COUNT(*) FROM devices d WHERE d.site_id = s.id)
		FROM sites s ORDER BY s.name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var site model.Site
		if err := rows.Scan(&site.ID, &site.ClientID, &site.Name, &site.DeviceCount); err != nil {
			return nil, err
		}
		if allowed != nil && !allowed[site.ID] {
			continue // außerhalb des Scopes
		}
		if c := byID[site.ClientID]; c != nil {
			c.Sites = append(c.Sites, site)
			c.DeviceCount += site.DeviceCount
		}
	}
	if allowed != nil { // Clients ohne sichtbaren Standort ausblenden
		filtered := make([]model.Client, 0, len(clients))
		for _, c := range clients {
			if len(c.Sites) > 0 {
				filtered = append(filtered, c)
			}
		}
		clients = filtered
	}
	return clients, rows.Err()
}

func (s *Store) listClients(ctx context.Context) ([]model.Client, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name FROM clients ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Client
	for rows.Next() {
		var c model.Client
		if err := rows.Scan(&c.ID, &c.Name); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// UnassignedDeviceCount zählt Geräte ohne Site-Zuordnung.
func (s *Store) UnassignedDeviceCount(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM devices WHERE site_id IS NULL`).Scan(&n)
	return n, err
}

func (s *Store) CreateClient(ctx context.Context, c *model.Client) error {
	_, err := s.db.ExecContext(ctx, s.rebind(`INSERT INTO clients (id, name) VALUES (?, ?)`), c.ID, c.Name)
	return err
}

func (s *Store) RenameClient(ctx context.Context, id, name string) error {
	return s.affect(s.db.ExecContext(ctx, s.rebind(`UPDATE clients SET name=? WHERE id=?`), name, id))
}

// DeleteClient löscht den Client samt Sites (FK-Cascade) und löst die Zuordnung
// betroffener Geräte/Tokens.
func (s *Store) DeleteClient(ctx context.Context, id string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	// Zuordnung der Geräte/Tokens aller Sites dieses Clients lösen (kein FK auf site_id).
	siteIDs, err := s.siteIDsOf(ctx, tx, id)
	if err != nil {
		return err
	}
	for _, sid := range siteIDs {
		if err := clearSite(ctx, tx, s, sid); err != nil {
			return err
		}
	}
	// Sites verschwinden per FK-Cascade beim Löschen des Clients.
	res, err := tx.ExecContext(ctx, s.rebind(`DELETE FROM clients WHERE id = ?`), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return tx.Commit()
}

// CountDevicesForClient zählt die (nicht widerrufenen) Geräte aller Sites eines Clients.
func (s *Store) CountDevicesForClient(ctx context.Context, clientID string) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, s.rebind(
		`SELECT COUNT(*) FROM devices d JOIN sites s ON s.id=d.site_id WHERE s.client_id=? AND d.revoked=0`), clientID).Scan(&n)
	return n, err
}

// CountDevicesForSite zählt die (nicht widerrufenen) Geräte einer Site.
func (s *Store) CountDevicesForSite(ctx context.Context, siteID string) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, s.rebind(
		`SELECT COUNT(*) FROM devices WHERE site_id=? AND revoked=0`), siteID).Scan(&n)
	return n, err
}

func (s *Store) siteIDsOf(ctx context.Context, tx *sql.Tx, clientID string) ([]string, error) {
	rows, err := tx.QueryContext(ctx, s.rebind(`SELECT id FROM sites WHERE client_id = ?`), clientID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func clearSite(ctx context.Context, tx *sql.Tx, s *Store, siteID string) error {
	if _, err := tx.ExecContext(ctx, s.rebind(`UPDATE devices SET site_id=NULL WHERE site_id=?`), siteID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, s.rebind(`UPDATE enrollment_tokens SET site_id=NULL WHERE site_id=?`), siteID); err != nil {
		return err
	}
	return nil
}

func (s *Store) CreateSite(ctx context.Context, site *model.Site) error {
	_, err := s.db.ExecContext(ctx, s.rebind(`INSERT INTO sites (id, client_id, name) VALUES (?, ?, ?)`),
		site.ID, site.ClientID, site.Name)
	return err
}

func (s *Store) RenameSite(ctx context.Context, id, name string) error {
	return s.affect(s.db.ExecContext(ctx, s.rebind(`UPDATE sites SET name=? WHERE id=?`), name, id))
}

// DeleteSite löst die Zuordnung betroffener Geräte/Tokens und löscht die Site.
func (s *Store) DeleteSite(ctx context.Context, id string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	if err := clearSite(ctx, tx, s, id); err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx, s.rebind(`DELETE FROM sites WHERE id=?`), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return tx.Commit()
}

// SetDeviceSite ordnet ein Gerät einer Site zu (oder hebt die Zuordnung auf, siteID=nil).
func (s *Store) SetDeviceSite(ctx context.Context, deviceID string, siteID *string) error {
	var arg any
	if siteID != nil {
		arg = *siteID
	}
	return s.affect(s.db.ExecContext(ctx, s.rebind(`UPDATE devices SET site_id=? WHERE id=?`), arg, deviceID))
}

// affect übersetzt ein Exec-Ergebnis ohne betroffene Zeilen in ErrNotFound.
func (s *Store) affect(res sql.Result, err error) error {
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}
