package store

import "context"

// Daten-Scope pro Benutzer (Kunden/Standorte). Keine Einträge = unbeschränkt.

// GetUserScope liefert die zugeordneten Kunden- und Standort-IDs eines Benutzers.
func (s *Store) GetUserScope(ctx context.Context, userID string) (clientIDs, siteIDs []string, err error) {
	rows, err := s.db.QueryContext(ctx, s.rebind(`SELECT target_type, target_id FROM user_scopes WHERE user_id = ?`), userID)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	clientIDs, siteIDs = []string{}, []string{}
	for rows.Next() {
		var typ, id string
		if err := rows.Scan(&typ, &id); err != nil {
			return nil, nil, err
		}
		if typ == "client" {
			clientIDs = append(clientIDs, id)
		} else if typ == "site" {
			siteIDs = append(siteIDs, id)
		}
	}
	return clientIDs, siteIDs, rows.Err()
}

// SetUserScope ersetzt den Scope eines Benutzers vollständig (leere Listen = unbeschränkt).
func (s *Store) SetUserScope(ctx context.Context, userID string, clientIDs, siteIDs []string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, s.rebind(`DELETE FROM user_scopes WHERE user_id = ?`), userID); err != nil {
		return err
	}
	ins := s.rebind(`INSERT INTO user_scopes (user_id, target_type, target_id) VALUES (?, ?, ?)`)
	for _, id := range clientIDs {
		if id == "" {
			continue
		}
		if _, err := tx.ExecContext(ctx, ins, userID, "client", id); err != nil {
			return err
		}
	}
	for _, id := range siteIDs {
		if id == "" {
			continue
		}
		if _, err := tx.ExecContext(ctx, ins, userID, "site", id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// AllowedSites liefert die für den Benutzer sichtbaren Standort-IDs. unrestricted=true
// bedeutet „alle Standorte" (keine Scope-Einträge). Ist unrestricted=false und die
// Menge leer, sieht der Benutzer keine Geräte.
func (s *Store) AllowedSites(ctx context.Context, userID string) (sites map[string]bool, unrestricted bool, err error) {
	clientIDs, siteIDs, err := s.GetUserScope(ctx, userID)
	if err != nil {
		return nil, false, err
	}
	if len(clientIDs) == 0 && len(siteIDs) == 0 {
		return nil, true, nil // unbeschränkt
	}
	sites = map[string]bool{}
	for _, id := range siteIDs {
		sites[id] = true
	}
	// Standorte der zugeordneten Kunden auflösen.
	for _, cid := range clientIDs {
		rows, err := s.db.QueryContext(ctx, s.rebind(`SELECT id FROM sites WHERE client_id = ?`), cid)
		if err != nil {
			return nil, false, err
		}
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return nil, false, err
			}
			sites[id] = true
		}
		rows.Close()
	}
	return sites, false, nil
}

// DeviceSiteID liefert die Site-ID eines Geräts (leer, wenn ohne Standort) und ob das
// Gerät existiert.
func (s *Store) DeviceSiteID(ctx context.Context, deviceID string) (siteID string, exists bool, err error) {
	var sid *string
	err = s.db.QueryRowContext(ctx, s.rebind(`SELECT site_id FROM devices WHERE id = ?`), deviceID).Scan(&sid)
	if err != nil {
		if err.Error() == "sql: no rows in result set" {
			return "", false, nil
		}
		return "", false, err
	}
	if sid != nil {
		siteID = *sid
	}
	return siteID, true, nil
}
