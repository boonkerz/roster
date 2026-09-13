package store

import (
	"context"
	"database/sql"
	"time"
)

// MetricPoint ist ein (aggregierter) Punkt der Auslastungs-Historie.
type MetricPoint struct {
	TS   int64    `json:"ts"` // Unix-Millisekunden (Bucket-Beginn)
	CPU  float64  `json:"cpu"`
	Mem  float64  `json:"mem"`
	Disk float64  `json:"disk"`
	Temp *float64 `json:"temp,omitempty"` // Leittemperatur °C (nil = keine Sensordaten im Bucket)
}

// InsertMetricsSample speichert eine Auslastungs-Momentaufnahme.
// temp ist die Leittemperatur in °C oder nil (Gerät ohne Sensoren).
func (s *Store) InsertMetricsSample(ctx context.Context, deviceID string, tsMillis int64, cpu, mem, disk float64, temp *float64) error {
	_, err := s.db.ExecContext(ctx, s.rebind(
		`INSERT INTO metrics_samples (device_id, ts, cpu, mem, disk, temp) VALUES (?, ?, ?, ?, ?, ?)`),
		deviceID, tsMillis, cpu, mem, disk, temp)
	return err
}

// MetricsHistory liefert die Historie ab sinceMillis, auf Zeit-Buckets (bucketMillis)
// gemittelt.
func (s *Store) MetricsHistory(ctx context.Context, deviceID string, sinceMillis, bucketMillis int64) ([]MetricPoint, error) {
	if bucketMillis <= 0 {
		bucketMillis = 5 * 60 * 1000
	}
	rows, err := s.db.QueryContext(ctx, s.rebind(`
		SELECT (ts/?)*? AS bucket, AVG(cpu), AVG(mem), AVG(disk), AVG(temp)
		FROM metrics_samples WHERE device_id=? AND ts>=?
		GROUP BY ts/? ORDER BY bucket`),
		bucketMillis, bucketMillis, deviceID, sinceMillis, bucketMillis)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []MetricPoint
	for rows.Next() {
		var p MetricPoint
		var temp sql.NullFloat64 // AVG über NULL-Werte bleibt NULL
		if err := rows.Scan(&p.TS, &p.CPU, &p.Mem, &p.Disk, &temp); err != nil {
			return nil, err
		}
		if temp.Valid {
			v := temp.Float64
			p.Temp = &v
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// PruneMetrics löscht Samples älter als retention.
func (s *Store) PruneMetrics(ctx context.Context, retention time.Duration) error {
	if retention <= 0 {
		return nil
	}
	cutoff := time.Now().Add(-retention).UnixMilli()
	_, err := s.db.ExecContext(ctx, s.rebind(`DELETE FROM metrics_samples WHERE ts < ?`), cutoff)
	return err
}
