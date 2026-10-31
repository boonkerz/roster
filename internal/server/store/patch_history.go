package store

import (
	"context"
	"strings"
	"time"

	"github.com/boonkerz/roster/internal/server/model"
	"github.com/boonkerz/roster/internal/shared"
)

// InsertPatchHistory legt Verlaufseinträge an; bereits bekannte (gleiches Gerät, Zeit,
// Name, Aktion, Version) werden still übersprungen. Liefert die Zahl neuer Zeilen.
func (s *Store) InsertPatchHistory(ctx context.Context, deviceID string, events []shared.PatchEvent) (int, error) {
	if len(events) == 0 {
		return 0, nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	n := 0
	for _, e := range events {
		if e.Name == "" || e.Time.IsZero() {
			continue
		}
		res, err := tx.ExecContext(ctx, s.rebind(`
			INSERT INTO patch_history (id, device_id, ts, action, name, old_version, new_version, source, detail)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT (device_id, ts, name, action, new_version) DO NOTHING`),
			NewID(), deviceID, e.Time.UTC(), e.Action, e.Name, e.OldVersion, e.NewVersion, e.Source, e.Detail)
		if err != nil {
			return 0, err
		}
		if k, _ := res.RowsAffected(); k > 0 {
			n++
		}
	}
	return n, tx.Commit()
}

// PatchHistoryFilter grenzt den flottenweiten Verlauf ein.
type PatchHistoryFilter struct {
	Allowed  map[string]bool // Daten-Scope: nil = alle, leer = nichts
	DeviceID string
	ClientID string
	SiteID   string
	Since    time.Time
	Action   string // install | upgrade | remove | ""
	Query    string // Teilstring auf Name/Hostname
	Limit    int
}

// PatchHistory liefert Verlaufseinträge (neueste zuerst) mit Gerätedaten.
func (s *Store) PatchHistory(ctx context.Context, f PatchHistoryFilter) ([]model.PatchEvent, error) {
	if f.Allowed != nil && len(f.Allowed) == 0 {
		return []model.PatchEvent{}, nil
	}
	q := `SELECT h.id, h.device_id, d.hostname, COALESCE(c.name,''), COALESCE(st.name,''), h.ts, h.action, h.name,
			h.old_version, h.new_version, h.source, h.detail
		FROM patch_history h
		JOIN devices d ON d.id = h.device_id
		LEFT JOIN sites st ON st.id = d.site_id
		LEFT JOIN clients c ON c.id = st.client_id
		WHERE d.revoked = FALSE`
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
	if f.DeviceID != "" {
		q += ` AND h.device_id = ?`
		args = append(args, f.DeviceID)
	}
	if f.ClientID != "" {
		q += ` AND st.client_id = ?`
		args = append(args, f.ClientID)
	}
	if f.SiteID != "" {
		q += ` AND d.site_id = ?`
		args = append(args, f.SiteID)
	}
	if !f.Since.IsZero() {
		q += ` AND h.ts >= ?`
		args = append(args, f.Since.UTC())
	}
	if f.Action != "" {
		q += ` AND h.action = ?`
		args = append(args, f.Action)
	}
	if t := strings.TrimSpace(f.Query); t != "" {
		like := "%" + strings.ToLower(t) + "%"
		q += ` AND (LOWER(h.name) LIKE ? OR LOWER(d.hostname) LIKE ?)`
		args = append(args, like, like)
	}
	q += ` ORDER BY h.ts DESC, d.hostname, h.name`
	limit := f.Limit
	if limit <= 0 || limit > 5000 {
		limit = 2000
	}
	q += ` LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, s.rebind(q), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.PatchEvent{}
	for rows.Next() {
		var e model.PatchEvent
		if err := rows.Scan(&e.ID, &e.DeviceID, &e.Hostname, &e.ClientName, &e.SiteName, &e.TS, &e.Action, &e.Name,
			&e.OldVersion, &e.NewVersion, &e.Source, &e.Detail); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// PatchInstallCounts zählt installierte/aktualisierte Patches seit since je Gerät (Report).
func (s *Store) PatchInstallCounts(ctx context.Context, since time.Time) (map[string]int, error) {
	rows, err := s.db.QueryContext(ctx, s.rebind(`
		SELECT device_id, COUNT(*) FROM patch_history
		WHERE ts >= ? AND action IN ('install','upgrade') GROUP BY device_id`), since.UTC())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var id string
		var n int
		if err := rows.Scan(&id, &n); err != nil {
			return nil, err
		}
		out[id] = n
	}
	return out, rows.Err()
}

// PrunePatchHistory löscht Verlaufseinträge, die älter als die Aufbewahrung sind.
func (s *Store) PrunePatchHistory(ctx context.Context, retention time.Duration) error {
	_, err := s.db.ExecContext(ctx, s.rebind(`DELETE FROM patch_history WHERE ts < ?`), time.Now().Add(-retention).UTC())
	return err
}
