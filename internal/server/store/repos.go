package store

import (
	"context"
	"strings"

	"github.com/boonkerz/roster/internal/server/model"
	"github.com/boonkerz/roster/internal/shared"
)

// ReplacePackageRepos ersetzt die Paketquellen eines Geräts (Replace-on-Checkin).
func (s *Store) ReplacePackageRepos(ctx context.Context, deviceID string, repos []shared.PackageRepo) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	if _, err := tx.ExecContext(ctx, s.rebind(`DELETE FROM package_repos WHERE device_id=?`), deviceID); err != nil {
		return err
	}
	for _, r := range repos {
		if _, err := tx.ExecContext(ctx, s.rebind(`
			INSERT INTO package_repos (device_id, repo_id, name, url, type, enabled, gpg_check, suite, third_party)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`),
			deviceID, r.ID, r.Name, r.URL, r.Type, r.Enabled, r.GPGCheck, r.Suite, r.ThirdParty); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// PackageReposFor liefert die Paketquellen eines Geräts (Fremd-Repos zuerst).
func (s *Store) PackageReposFor(ctx context.Context, deviceID string) ([]model.PackageRepo, error) {
	rows, err := s.db.QueryContext(ctx, s.rebind(`
		SELECT repo_id, name, url, type, enabled, gpg_check, suite, third_party
		FROM package_repos WHERE device_id=? ORDER BY third_party DESC, type, name`), deviceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.PackageRepo
	for rows.Next() {
		var r model.PackageRepo
		if err := rows.Scan(&r.ID, &r.Name, &r.URL, &r.Type, &r.Enabled, &r.GPGCheck, &r.Suite, &r.ThirdParty); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// scopeWhere ergänzt den Daten-Scope (Standort-IDs) als SQL-Bedingung auf d.site_id.
// empty=true, wenn der Scope leer ist (dann gar nicht erst abfragen).
func scopeWhere(allowed map[string]bool) (cond string, args []any, empty bool) {
	if allowed == nil {
		return "", nil, false
	}
	if len(allowed) == 0 {
		return "", nil, true
	}
	ids := make([]string, 0, len(allowed))
	for id := range allowed {
		ids = append(ids, id)
	}
	marks, a := placeholders(ids)
	return ` AND d.site_id IN (` + marks + `)`, a, false
}

// RepoFilter grenzt die flottenweite Repo-Liste ein.
type RepoFilter struct {
	Allowed        map[string]bool
	Query          string
	ThirdPartyOnly bool
	DisabledOnly   bool
	NoGPGOnly      bool
	Type           string
}

// ReposFleet liefert Paketquellen aller Geräte (Fremd-Repos zuerst).
func (s *Store) ReposFleet(ctx context.Context, f RepoFilter) ([]model.PackageRepo, error) {
	scope, args, empty := scopeWhere(f.Allowed)
	if empty {
		return []model.PackageRepo{}, nil
	}
	q := `SELECT r.device_id, d.hostname, COALESCE(c.name,''), r.repo_id, r.name, r.url, r.type, r.enabled, r.gpg_check, r.suite, r.third_party
		FROM package_repos r
		JOIN devices d ON d.id = r.device_id
		LEFT JOIN sites st ON st.id = d.site_id
		LEFT JOIN clients c ON c.id = st.client_id
		WHERE d.revoked = FALSE` + scope
	if f.ThirdPartyOnly {
		q += ` AND r.third_party = TRUE`
	}
	if f.DisabledOnly {
		q += ` AND r.enabled = FALSE`
	}
	if f.NoGPGOnly {
		q += ` AND r.gpg_check = FALSE`
	}
	if f.Type != "" {
		q += ` AND r.type = ?`
		args = append(args, f.Type)
	}
	if t := strings.TrimSpace(f.Query); t != "" {
		like := "%" + strings.ToLower(t) + "%"
		q += ` AND (LOWER(r.name) LIKE ? OR LOWER(r.url) LIKE ? OR LOWER(d.hostname) LIKE ?)`
		args = append(args, like, like, like)
	}
	q += ` ORDER BY r.third_party DESC, r.url, d.hostname`
	rows, err := s.db.QueryContext(ctx, s.rebind(q), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.PackageRepo{}
	for rows.Next() {
		var r model.PackageRepo
		if err := rows.Scan(&r.DeviceID, &r.Hostname, &r.ClientName, &r.ID, &r.Name, &r.URL, &r.Type, &r.Enabled, &r.GPGCheck, &r.Suite, &r.ThirdParty); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// PackagesFleet aggregiert die installierte Software über alle Geräte: je Name und
// Version die Anzahl Geräte. q filtert auf den Namen (Teilstring); ohne q die 500
// häufigsten Pakete.
func (s *Store) PackagesFleet(ctx context.Context, allowed map[string]bool, q string, limit int) ([]model.PackageVersionCount, error) {
	scope, args, empty := scopeWhere(allowed)
	if empty {
		return []model.PackageVersionCount{}, nil
	}
	sql := `SELECT sw.name, sw.version, COUNT(DISTINCT sw.device_id)
		FROM software sw JOIN devices d ON d.id = sw.device_id
		WHERE d.revoked = FALSE` + scope
	if t := strings.TrimSpace(q); t != "" {
		sql += ` AND LOWER(sw.name) LIKE ?`
		args = append(args, "%"+strings.ToLower(t)+"%")
	}
	if limit <= 0 || limit > 5000 {
		limit = 2000
	}
	sql += ` GROUP BY sw.name, sw.version ORDER BY LOWER(sw.name), sw.version LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, s.rebind(sql), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.PackageVersionCount{}
	for rows.Next() {
		var p model.PackageVersionCount
		if err := rows.Scan(&p.Name, &p.Version, &p.Devices); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// PackageDevices liefert die Geräte, auf denen ein Paket (optional in einer Version)
// installiert ist.
func (s *Store) PackageDevices(ctx context.Context, allowed map[string]bool, name, version string) ([]model.PackageDevice, error) {
	scope, args, empty := scopeWhere(allowed)
	if empty {
		return []model.PackageDevice{}, nil
	}
	sql := `SELECT d.id, d.hostname, d.os, COALESCE(c.name,''), COALESCE(st.name,''), sw.version
		FROM software sw JOIN devices d ON d.id = sw.device_id
		LEFT JOIN sites st ON st.id = d.site_id
		LEFT JOIN clients c ON c.id = st.client_id
		WHERE d.revoked = FALSE AND sw.name = ?` + scope
	args = append([]any{name}, args...)
	if version != "" {
		sql += ` AND sw.version = ?`
		args = append(args, version)
	}
	sql += ` ORDER BY d.hostname LIMIT 2000`
	rows, err := s.db.QueryContext(ctx, s.rebind(sql), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.PackageDevice{}
	for rows.Next() {
		var p model.PackageDevice
		if err := rows.Scan(&p.DeviceID, &p.Hostname, &p.OS, &p.ClientName, &p.SiteName, &p.Version); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
