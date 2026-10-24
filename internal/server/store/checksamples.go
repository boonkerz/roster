package store

import (
	"context"
	"database/sql"
	"time"
)

// CheckPoint ist ein Punkt im Check-Verlauf: bei Rohdaten ein Checkin, bei
// zusammengefassten Zeiträumen ein Zeitfenster (Mittel sowie Min/Max des Werts,
// Status = schlechtester im Fenster).
type CheckPoint struct {
	TS     int64   `json:"ts"`
	Value  float64 `json:"value"`
	Min    float64 `json:"min"`
	Max    float64 `json:"max"`
	Status string  `json:"status"`
	N      int     `json:"n"`
}

// insertCheckSample hängt ein Ergebnis an den Verlauf an (wird aus SaveCheckResults gerufen).
func (s *Store) insertCheckSample(ctx context.Context, deviceID, checkID string, ts time.Time, status string, value float64) error {
	_, err := s.db.ExecContext(ctx, s.rebind(
		`INSERT INTO check_samples (device_id, check_id, ts, status, value) VALUES (?, ?, ?, ?, ?)`),
		deviceID, checkID, ts.UnixMilli(), status, value)
	return err
}

// CheckHistory liefert den Verlauf eines Checks seit sinceMillis, in Fenstern von
// bucketMillis zusammengefasst (<=0: eine Minute, damit Rohdaten erhalten bleiben).
// Der Status eines Fensters ist der schlechteste darin (failing > warning > unknown > passing).
func (s *Store) CheckHistory(ctx context.Context, deviceID, checkID string, sinceMillis, bucketMillis int64) ([]CheckPoint, error) {
	if bucketMillis <= 0 {
		bucketMillis = 60 * 1000
	}
	rows, err := s.db.QueryContext(ctx, s.rebind(`
		SELECT (ts/?)*? AS bucket, AVG(value), MIN(value), MAX(value), COUNT(*),
		       MAX(CASE status WHEN 'failing' THEN 3 WHEN 'warning' THEN 2 WHEN 'passing' THEN 0 ELSE 1 END)
		FROM check_samples WHERE device_id=? AND check_id=? AND ts>=?
		GROUP BY ts/? ORDER BY bucket`),
		bucketMillis, bucketMillis, deviceID, checkID, sinceMillis, bucketMillis)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CheckPoint
	for rows.Next() {
		var p CheckPoint
		var avg, mn, mx sql.NullFloat64
		var rank int
		if err := rows.Scan(&p.TS, &avg, &mn, &mx, &p.N, &rank); err != nil {
			return nil, err
		}
		p.Value, p.Min, p.Max = avg.Float64, mn.Float64, mx.Float64
		p.Status = [...]string{"passing", "unknown", "warning", "failing"}[rank]
		out = append(out, p)
	}
	return out, rows.Err()
}

// PruneCheckSamples löscht Verlaufszeilen älter als retention (<=0: nie).
func (s *Store) PruneCheckSamples(ctx context.Context, retention time.Duration) error {
	if retention <= 0 {
		return nil
	}
	cutoff := time.Now().Add(-retention).UnixMilli()
	_, err := s.db.ExecContext(ctx, s.rebind(`DELETE FROM check_samples WHERE ts < ?`), cutoff)
	return err
}
