package store

import (
	"context"
	"time"
)

// StatSample ist eine Zeile der Statistik-Historie (siehe Migration 0055).
type StatSample struct {
	TS     int64
	SiteID string
	Kind   string
	Key    string
	Label  string
	Value  float64
}

// StatPoint ist ein Punkt der Historie, über die erlaubten Standorte summiert.
type StatPoint struct {
	TS    int64   `json:"ts"`
	Label string  `json:"label"`
	Value float64 `json:"value"`
}

// CheckStatus ist der aktuelle Status eines Checks auf einem Gerät.
type CheckStatus struct {
	DeviceID string
	CheckID  string
	Status   string
}

// ReplaceStatSamples ersetzt alle Samples eines Zeitpunkts durch die übergebenen –
// ein erneuter Snapshot innerhalb derselben Stunde (Neustart) erzeugt so keine Dubletten.
func (s *Store) ReplaceStatSamples(ctx context.Context, ts int64, samples []StatSample) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, s.rebind(`DELETE FROM stat_samples WHERE ts=?`), ts); err != nil {
		return err
	}
	stmt, err := tx.PrepareContext(ctx, s.rebind(
		`INSERT INTO stat_samples (ts, site_id, kind, key, label, value) VALUES (?, ?, ?, ?, ?, ?)`))
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, p := range samples {
		if _, err := stmt.ExecContext(ctx, ts, p.SiteID, p.Kind, p.Key, p.Label, p.Value); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// StatHistory liefert die Historie einer Kennzahl ab sinceMillis, je Zeitpunkt und
// Label über die erlaubten Standorte summiert (allowed nil = alle).
func (s *Store) StatHistory(ctx context.Context, kind, key string, sinceMillis int64, allowed map[string]bool) ([]StatPoint, error) {
	query := `SELECT ts, label, SUM(value) FROM stat_samples WHERE kind=? AND key=? AND ts>=?`
	args := []any{kind, key, sinceMillis}
	if allowed != nil {
		if len(allowed) == 0 {
			return []StatPoint{}, nil
		}
		ids := make([]string, 0, len(allowed))
		for id := range allowed {
			ids = append(ids, id)
		}
		marks, a := placeholders(ids)
		query += ` AND site_id IN (` + marks + `)`
		args = append(args, a...)
	}
	query += ` GROUP BY ts, label ORDER BY ts, label`
	rows, err := s.db.QueryContext(ctx, s.rebind(query), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []StatPoint{}
	for rows.Next() {
		var p StatPoint
		if err := rows.Scan(&p.TS, &p.Label, &p.Value); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// PruneStats löscht Samples älter als retention.
func (s *Store) PruneStats(ctx context.Context, retention time.Duration) error {
	if retention <= 0 {
		return nil
	}
	cutoff := time.Now().Add(-retention).UnixMilli()
	_, err := s.db.ExecContext(ctx, s.rebind(`DELETE FROM stat_samples WHERE ts<?`), cutoff)
	return err
}

// AllCheckStatuses liefert den aktuellen Status jedes Checks auf jedem Gerät
// (nur Checks, die es noch gibt).
func (s *Store) AllCheckStatuses(ctx context.Context) ([]CheckStatus, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT cr.device_id, cr.check_id, cr.status
		FROM check_results cr JOIN policy_checks pc ON pc.id=cr.check_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CheckStatus
	for rows.Next() {
		var c CheckStatus
		if err := rows.Scan(&c.DeviceID, &c.CheckID, &c.Status); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
