package store

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/boonkerz/roster/internal/server/model"
	"github.com/boonkerz/roster/internal/shared"
)

// Container-Sicherheit: Image-Paketlisten, deren OSV-Treffer und Datenbank-Bestände.

// UpsertImageScans speichert neue Paketlisten je Image und markiert sie für den OSV-Abgleich.
func (s *Store) UpsertImageScans(ctx context.Context, deviceID string, scans []shared.ImageScan) error {
	now := time.Now().UTC()
	for _, sc := range scans {
		if sc.ImageID == "" {
			continue
		}
		pk, _ := json.Marshal(sc.Packages)
		services := strings.Join(sc.Services, ",")
		res, err := s.db.ExecContext(ctx, s.rebind(`UPDATE docker_image_scans
			SET image=?, os=?, os_version=?, os_name=?, pkg=?, packages=?, package_count=?, services=?, error=?, scanned_at=?, vuln_scanned_at=NULL
			WHERE device_id=? AND image_id=?`),
			sc.Image, sc.OS, sc.OSVersion, sc.OSName, sc.Pkg, string(pk), len(sc.Packages), services, sc.Error, now, deviceID, sc.ImageID)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			if _, err := s.db.ExecContext(ctx, s.rebind(`INSERT INTO docker_image_scans
				(device_id, image_id, image, os, os_version, os_name, pkg, packages, package_count, services, error, scanned_at)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`),
				deviceID, sc.ImageID, sc.Image, sc.OS, sc.OSVersion, sc.OSName, sc.Pkg, string(pk), len(sc.Packages), services, sc.Error, now); err != nil {
				return err
			}
		}
	}
	return nil
}

// PruneImageScans entfernt Scans und Treffer von Images, die das Gerät nicht mehr hat.
func (s *Store) PruneImageScans(ctx context.Context, deviceID string) error {
	for _, q := range []string{
		`DELETE FROM container_vulnerabilities WHERE device_id=? AND image_id NOT IN (SELECT image_id FROM docker_images WHERE device_id=?)`,
		`DELETE FROM docker_image_scans WHERE device_id=? AND image_id NOT IN (SELECT image_id FROM docker_images WHERE device_id=?)`,
	} {
		if _, err := s.db.ExecContext(ctx, s.rebind(q), deviceID, deviceID); err != nil {
			return err
		}
	}
	return nil
}

// ImageScansFor liefert die gescannten Images eines Geräts inkl. Trefferzahlen.
func (s *Store) ImageScansFor(ctx context.Context, deviceID string) ([]model.DockerImageScan, error) {
	rows, err := s.db.QueryContext(ctx, s.rebind(`
		SELECT s.image_id, s.image, s.os, s.os_version, s.os_name, s.pkg, s.package_count, s.services, s.error, s.scanned_at, s.vuln_scanned_at,
		       (SELECT COUNT(*) FROM container_vulnerabilities v WHERE v.device_id=s.device_id AND v.image_id=s.image_id),
		       (SELECT COUNT(*) FROM container_vulnerabilities v WHERE v.device_id=s.device_id AND v.image_id=s.image_id AND v.severity='CRITICAL'),
		       (SELECT COUNT(*) FROM container_vulnerabilities v WHERE v.device_id=s.device_id AND v.image_id=s.image_id AND v.severity='HIGH')
		FROM docker_image_scans s WHERE s.device_id=? ORDER BY s.image`), deviceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.DockerImageScan
	for rows.Next() {
		var sc model.DockerImageScan
		var services string
		if err := rows.Scan(&sc.ImageID, &sc.Image, &sc.OS, &sc.OSVersion, &sc.OSName, &sc.Pkg, &sc.PackageCount, &services, &sc.Error, &sc.ScannedAt, &sc.VulnScannedAt,
			&sc.Vulns, &sc.Critical, &sc.High); err != nil {
			return nil, err
		}
		if services != "" {
			sc.Services = strings.Split(services, ",")
		}
		out = append(out, sc)
	}
	return out, rows.Err()
}

// PendingImageScan ist ein Image, dessen OSV-Abgleich aussteht oder älter als einen Tag ist.
type PendingImageScan struct {
	DeviceID, ImageID, Image, OS, OSVersion, Pkg string
	Packages                                     []shared.ImagePackage
}

// PendingImageScans liefert bis zu limit Images für den OSV-Abgleich (neue zuerst).
func (s *Store) PendingImageScans(ctx context.Context, limit int) ([]PendingImageScan, error) {
	cutoff := time.Now().UTC().Add(-24 * time.Hour)
	rows, err := s.db.QueryContext(ctx, s.rebind(`
		SELECT s.device_id, s.image_id, s.image, s.os, s.os_version, s.pkg, s.packages
		FROM docker_image_scans s JOIN devices d ON d.id=s.device_id
		WHERE d.revoked=0 AND s.error='' AND s.package_count>0 AND (s.vuln_scanned_at IS NULL OR s.vuln_scanned_at < ?)
		ORDER BY s.vuln_scanned_at IS NOT NULL, s.scanned_at LIMIT ?`), cutoff, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PendingImageScan
	for rows.Next() {
		var p PendingImageScan
		var pk string
		if err := rows.Scan(&p.DeviceID, &p.ImageID, &p.Image, &p.OS, &p.OSVersion, &p.Pkg, &pk); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(pk), &p.Packages)
		out = append(out, p)
	}
	return out, rows.Err()
}

// ReplaceContainerVulns ersetzt die Treffer eines Images und vermerkt den Abgleich.
func (s *Store) ReplaceContainerVulns(ctx context.Context, deviceID, imageID string, vulns []model.ContainerVulnerability) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	if _, err := tx.ExecContext(ctx, s.rebind(`DELETE FROM container_vulnerabilities WHERE device_id=? AND image_id=?`), deviceID, imageID); err != nil {
		return err
	}
	now := time.Now().UTC()
	seen := map[string]bool{}
	for _, v := range vulns {
		key := v.Package + "\x00" + v.VulnID
		if seen[key] {
			continue
		}
		seen[key] = true
		if _, err := tx.ExecContext(ctx, s.rebind(`INSERT INTO container_vulnerabilities
			(device_id, image_id, image, package, version, vuln_id, severity, summary, fixed, url, detected_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`),
			deviceID, imageID, v.Image, v.Package, v.Version, v.VulnID, v.Severity, v.Summary, v.Fixed, v.URL, now); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, s.rebind(`UPDATE docker_image_scans SET vuln_scanned_at=? WHERE device_id=? AND image_id=?`), now, deviceID, imageID); err != nil {
		return err
	}
	return tx.Commit()
}

const containerVulnOrder = `ORDER BY CASE v.severity WHEN 'CRITICAL' THEN 0 WHEN 'HIGH' THEN 1 WHEN 'MEDIUM' THEN 2 WHEN 'LOW' THEN 3 ELSE 4 END, v.image, v.package`

func scanContainerVuln(rows scanner) (model.ContainerVulnerability, error) {
	var v model.ContainerVulnerability
	err := rows.Scan(&v.DeviceID, &v.Hostname, &v.ImageID, &v.Image, &v.Package, &v.Version, &v.VulnID, &v.Severity, &v.Summary, &v.Fixed, &v.URL, &v.DetectedAt)
	return v, err
}

// ContainerVulnsFor liefert die Image-Schwachstellen eines Geräts (kritische zuerst).
func (s *Store) ContainerVulnsFor(ctx context.Context, deviceID string) ([]model.ContainerVulnerability, error) {
	rows, err := s.db.QueryContext(ctx, s.rebind(`
		SELECT v.device_id, '', v.image_id, v.image, v.package, v.version, v.vuln_id, v.severity, v.summary, v.fixed, v.url, v.detected_at
		FROM container_vulnerabilities v WHERE v.device_id=? `+containerVulnOrder), deviceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.ContainerVulnerability
	for rows.Next() {
		v, err := scanContainerVuln(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// AllContainerVulns liefert alle Image-Schwachstellen der Flotte inkl. Hostname.
func (s *Store) AllContainerVulns(ctx context.Context) ([]model.ContainerVulnerability, error) {
	rows, err := s.db.QueryContext(ctx, s.rebind(`
		SELECT v.device_id, d.hostname, v.image_id, v.image, v.package, v.version, v.vuln_id, v.severity, v.summary, v.fixed, v.url, v.detected_at
		FROM container_vulnerabilities v JOIN devices d ON d.id=v.device_id
		WHERE d.revoked=0 `+containerVulnOrder))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.ContainerVulnerability
	for rows.Next() {
		v, err := scanContainerVuln(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// ReplaceDockerDBs ersetzt den Datenbank-Bestand eines Geräts und liefert den vorherigen
// Stand (für die Abweichungs-Erkennung des Aufrufers).
func (s *Store) ReplaceDockerDBs(ctx context.Context, deviceID string, dbs []shared.DockerDatabase) ([]model.DockerDatabase, error) {
	old, err := s.DockerDBsFor(ctx, deviceID)
	if err != nil {
		return nil, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback() //nolint:errcheck
	if _, err := tx.ExecContext(ctx, s.rebind(`DELETE FROM docker_dbs WHERE device_id=?`), deviceID); err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	for _, d := range dbs {
		if _, err := tx.ExecContext(ctx, s.rebind(`INSERT INTO docker_dbs (device_id, container_id, container, engine, name, collections, error, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`), deviceID, d.ContainerID, d.Container, d.Engine, d.Name, d.Collections, d.Error, now); err != nil {
			return nil, err
		}
	}
	return old, tx.Commit()
}

// DockerDBsFor liefert den gespeicherten Datenbank-Bestand eines Geräts.
func (s *Store) DockerDBsFor(ctx context.Context, deviceID string) ([]model.DockerDatabase, error) {
	rows, err := s.db.QueryContext(ctx, s.rebind(`SELECT container_id, container, engine, name, collections, error, updated_at
		FROM docker_dbs WHERE device_id=? ORDER BY container, name`), deviceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.DockerDatabase
	for rows.Next() {
		var d model.DockerDatabase
		if err := rows.Scan(&d.ContainerID, &d.Container, &d.Engine, &d.Name, &d.Collections, &d.Error, &d.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}
