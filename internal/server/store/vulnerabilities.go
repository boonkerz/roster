package store

import (
	"context"
	"time"

	"github.com/boonkerz/roster/internal/server/model"
)

// ReplaceVulnerabilities ersetzt die Schwachstellen eines Geräts (Ergebnis eines Scans).
func (s *Store) ReplaceVulnerabilities(ctx context.Context, deviceID string, vulns []model.Vulnerability) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	if _, err := tx.ExecContext(ctx, s.rebind(`DELETE FROM vulnerabilities WHERE device_id=?`), deviceID); err != nil {
		return err
	}
	now := time.Now().UTC()
	for _, v := range vulns {
		if _, err := tx.ExecContext(ctx, s.rebind(`
			INSERT INTO vulnerabilities (device_id, package, version, vuln_id, severity, summary, fixed, url, detected_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(device_id, package, vuln_id) DO NOTHING`),
			deviceID, v.Package, v.Version, v.VulnID, v.Severity, v.Summary, v.Fixed, v.URL, now); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func scanVuln(rows scanner) (model.Vulnerability, error) {
	var v model.Vulnerability
	err := rows.Scan(&v.Package, &v.Version, &v.VulnID, &v.Severity, &v.Summary, &v.Fixed, &v.URL, &v.DetectedAt)
	return v, err
}

// VulnerabilitiesFor liefert die Schwachstellen eines Geräts (kritische zuerst).
func (s *Store) VulnerabilitiesFor(ctx context.Context, deviceID string) ([]model.Vulnerability, error) {
	rows, err := s.db.QueryContext(ctx, s.rebind(`
		SELECT package, version, vuln_id, severity, summary, fixed, url, detected_at
		FROM vulnerabilities WHERE device_id=?
		ORDER BY CASE severity WHEN 'CRITICAL' THEN 0 WHEN 'HIGH' THEN 1 WHEN 'MEDIUM' THEN 2 WHEN 'LOW' THEN 3 ELSE 4 END, package`), deviceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Vulnerability
	for rows.Next() {
		v, err := scanVuln(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// AllVulnerabilities liefert alle Schwachstellen der Flotte inkl. Hostname.
func (s *Store) AllVulnerabilities(ctx context.Context) ([]model.Vulnerability, error) {
	rows, err := s.db.QueryContext(ctx, s.rebind(`
		SELECT d.id, d.hostname, v.package, v.version, v.vuln_id, v.severity, v.summary, v.fixed, v.url, v.detected_at
		FROM vulnerabilities v JOIN devices d ON d.id=v.device_id
		WHERE d.revoked=0
		ORDER BY CASE v.severity WHEN 'CRITICAL' THEN 0 WHEN 'HIGH' THEN 1 WHEN 'MEDIUM' THEN 2 WHEN 'LOW' THEN 3 ELSE 4 END, d.hostname`))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Vulnerability
	for rows.Next() {
		var v model.Vulnerability
		if err := rows.Scan(&v.DeviceID, &v.Hostname, &v.Package, &v.Version, &v.VulnID, &v.Severity, &v.Summary, &v.Fixed, &v.URL, &v.DetectedAt); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// VulnerabilityCount liefert die Anzahl Schwachstellen je Gerät (für Badges).
func (s *Store) VulnerabilityCount(ctx context.Context) (map[string]int, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT device_id, COUNT(*) FROM vulnerabilities GROUP BY device_id`)
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

// KnownVulnDetails liefert zu Schwachstellen-IDs bereits gespeicherte Details (Stufe,
// Zusammenfassung, behoben in) aus früheren Scans beliebiger Geräte – spart die erneute
// Detail-Abfrage bei OSV. Nur Zeilen mit Inhalt zählen.
func (s *Store) KnownVulnDetails(ctx context.Context, ids []string) (map[string]cveKnown, error) {
	out := map[string]cveKnown{}
	for start := 0; start < len(ids); start += 400 {
		end := start + 400
		if end > len(ids) {
			end = len(ids)
		}
		chunk := ids[start:end]
		ph, args := placeholders(chunk)
		rows, err := s.db.QueryContext(ctx, s.rebind(`SELECT vuln_id, severity, summary, fixed FROM vulnerabilities
			WHERE vuln_id IN (`+ph+`) AND (severity<>'' OR summary<>'')`), args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var id string
			var k cveKnown
			if err := rows.Scan(&id, &k.Severity, &k.Summary, &k.Fixed); err != nil {
				rows.Close()
				return nil, err
			}
			if prev, ok := out[id]; !ok || (prev.Severity == "" && k.Severity != "") {
				out[id] = k
			}
		}
		rows.Close()
	}
	return out, nil
}

// cveKnown spiegelt cve.Known (ohne Import-Zyklus store -> cve).
type cveKnown struct{ Severity, Summary, Fixed string }
