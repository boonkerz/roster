package store

import (
	"context"
	"strings"

	"github.com/boonkerz/roster/internal/server/model"
)

// PatchFilter grenzt die flottenweite Patch-Übersicht ein. Allowed=nil = alle Standorte
// (Daten-Scope), leere Map = nichts.
type PatchFilter struct {
	Allowed      map[string]bool
	ClientID     string
	SiteID       string
	Query        string // Teilstring auf Paketname oder Hostname
	SecurityOnly bool
	ApprovedOnly bool
}

// PendingPatches liefert alle ausstehenden Updates der Flotte (Gerät × Update), Sicher-
// heits-Updates und kritische zuerst, dann nach Hostname/Paket.
func (s *Store) PendingPatches(ctx context.Context, f PatchFilter) ([]model.PendingPatch, error) {
	if f.Allowed != nil && len(f.Allowed) == 0 {
		return []model.PendingPatch{}, nil
	}
	q := `SELECT u.device_id, d.hostname, d.os, COALESCE(d.site_id,''), COALESCE(st.name,''), COALESCE(c.id,''), COALESCE(c.name,''),
			u.package, u.severity, u.url, u.version, u.current_version, u.source, u.security, u.kind, u.phased,
			CASE WHEN a.name IS NULL THEN 0 ELSE 1 END
		FROM os_updates u
		JOIN devices d ON d.id = u.device_id
		LEFT JOIN sites st ON st.id = d.site_id
		LEFT JOIN clients c ON c.id = st.client_id
		LEFT JOIN patch_approvals a ON a.device_id = u.device_id AND a.name = u.package
		WHERE d.revoked = FALSE AND d.managed = TRUE`
	var args []any
	if f.Allowed != nil {
		ids := make([]string, 0, len(f.Allowed))
		for id := range f.Allowed {
			ids = append(ids, id)
		}
		marks, a := placeholders(ids)
		q += ` AND d.site_id IN (` + marks + `)`
		args = append(args, a...)
	}
	if f.ClientID != "" {
		q += ` AND st.client_id = ?`
		args = append(args, f.ClientID)
	}
	if f.SiteID != "" {
		q += ` AND d.site_id = ?`
		args = append(args, f.SiteID)
	}
	if f.SecurityOnly {
		q += ` AND u.security = TRUE`
	}
	if f.ApprovedOnly {
		q += ` AND a.name IS NOT NULL`
	}
	if t := strings.TrimSpace(f.Query); t != "" {
		q += ` AND (LOWER(u.package) LIKE ? OR LOWER(d.hostname) LIKE ?)`
		like := "%" + strings.ToLower(t) + "%"
		args = append(args, like, like)
	}
	q += ` ORDER BY CASE WHEN u.security THEN 0 ELSE 1 END,
		CASE u.severity WHEN 'Critical' THEN 0 WHEN 'Important' THEN 1 ELSE 2 END, d.hostname, u.package`
	rows, err := s.db.QueryContext(ctx, s.rebind(q), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.PendingPatch{}
	for rows.Next() {
		var p model.PendingPatch
		var approved int
		if err := rows.Scan(&p.DeviceID, &p.Hostname, &p.OS, &p.SiteID, &p.SiteName, &p.ClientID, &p.ClientName,
			&p.Name, &p.Severity, &p.URL, &p.Version, &p.CurrentVersion, &p.Source, &p.Security, &p.Kind, &p.Phased, &approved); err != nil {
			return nil, err
		}
		p.Approved = approved == 1
		out = append(out, p)
	}
	return out, rows.Err()
}

// PatchSummary zählt die Kopfzahlen der Patch-Seite innerhalb des Daten-Scopes.
func (s *Store) PatchSummary(ctx context.Context, allowed map[string]bool) (model.PatchSummary, error) {
	var sum model.PatchSummary
	if allowed != nil && len(allowed) == 0 {
		return sum, nil
	}
	where := ` WHERE d.revoked = FALSE AND d.managed = TRUE`
	var args []any
	if allowed != nil {
		ids := make([]string, 0, len(allowed))
		for id := range allowed {
			ids = append(ids, id)
		}
		marks, a := placeholders(ids)
		where += ` AND d.site_id IN (` + marks + `)`
		args = append(args, a...)
	}
	err := s.db.QueryRowContext(ctx, s.rebind(`
		SELECT COUNT(*),
			COALESCE(SUM(CASE WHEN d.updates_count IS NOT NULL THEN 1 ELSE 0 END),0),
			COALESCE(SUM(CASE WHEN d.updates_count > 0 THEN 1 ELSE 0 END),0),
			COALESCE(SUM(CASE WHEN d.security_updates_count > 0 THEN 1 ELSE 0 END),0),
			COALESCE(SUM(COALESCE(d.updates_count,0)),0),
			COALESCE(SUM(COALESCE(d.security_updates_count,0)),0)
		FROM devices d`+where), args...).Scan(&sum.DevicesTotal, &sum.DevicesChecked, &sum.DevicesPending,
		&sum.DevicesSecurity, &sum.Pending, &sum.Security)
	if err != nil {
		return sum, err
	}
	// Genehmigte, noch ausstehende Updates (unabhängig von Sicherheits-Flag).
	if err := s.db.QueryRowContext(ctx, s.rebind(`
		SELECT COUNT(*) FROM os_updates u
		JOIN patch_approvals a ON a.device_id = u.device_id AND a.name = u.package
		JOIN devices d ON d.id = u.device_id`+where), args...).Scan(&sum.Approved); err != nil {
		return sum, err
	}
	if sum.DevicesChecked > 0 {
		sum.CompliantPct = (sum.DevicesChecked - sum.DevicesSecurity) * 100 / sum.DevicesChecked
	}
	return sum, nil
}

// SecurityPatches liefert die Namen der ausstehenden Sicherheits-Updates eines Geräts
// (für „nur Sicherheits-Updates installieren“).
func (s *Store) SecurityPatches(ctx context.Context, deviceID string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, s.rebind(`
		SELECT package FROM os_updates WHERE device_id = ? AND security = TRUE ORDER BY package`), deviceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// DeviceSiteIDs liefert je Gerät die Standort-ID (für Scope-Prüfungen bei Listen).
func (s *Store) DeviceSiteIDs(ctx context.Context, ids []string) (map[string]string, error) {
	out := map[string]string{}
	if len(ids) == 0 {
		return out, nil
	}
	marks, args := placeholders(ids)
	rows, err := s.db.QueryContext(ctx, s.rebind(`SELECT id, COALESCE(site_id,'') FROM devices WHERE id IN (`+marks+`)`), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, site string
		if err := rows.Scan(&id, &site); err != nil {
			return nil, err
		}
		out[id] = site
	}
	return out, rows.Err()
}
