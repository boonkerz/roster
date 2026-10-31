package store

import (
	"context"
	"database/sql"
	"time"

	"github.com/boonkerz/roster/internal/server/model"
	"github.com/boonkerz/roster/internal/shared"
)

// ReplaceCompliance ersetzt die Ergebnisse eines Geräts und schreibt den Score
// (bestanden / (bestanden + fehlgeschlagen) · 100; na/error zählen nicht).
func (s *Store) ReplaceCompliance(ctx context.Context, deviceID string, rep *shared.ComplianceReport) error {
	if rep == nil {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	if _, err := tx.ExecContext(ctx, s.rebind(`DELETE FROM compliance_results WHERE device_id=?`), deviceID); err != nil {
		return err
	}
	at := rep.ScannedAt.UTC()
	if at.IsZero() {
		at = time.Now().UTC()
	}
	pass, fail := 0, 0
	for _, r := range rep.Results {
		switch r.Status {
		case "pass":
			pass++
		case "fail":
			fail++
		}
		if _, err := tx.ExecContext(ctx, s.rebind(`
			INSERT INTO compliance_results (device_id, rule_id, status, evidence, checked_at) VALUES (?, ?, ?, ?, ?)`),
			deviceID, r.RuleID, r.Status, r.Evidence, at); err != nil {
			return err
		}
	}
	score := 100
	if pass+fail > 0 {
		score = pass * 100 / (pass + fail)
	}
	if _, err := tx.ExecContext(ctx, s.rebind(`UPDATE devices SET compliance_score=?, compliance_checked_at=?, compliance_profile=? WHERE id=?`),
		score, at, rep.Profile, deviceID); err != nil {
		return err
	}
	return tx.Commit()
}

// ComplianceFor liefert die Ergebnisse eines Geräts.
func (s *Store) ComplianceFor(ctx context.Context, deviceID string) ([]model.ComplianceResult, error) {
	rows, err := s.db.QueryContext(ctx, s.rebind(`SELECT rule_id, status, evidence, checked_at FROM compliance_results WHERE device_id=? ORDER BY rule_id`), deviceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.ComplianceResult{}
	for rows.Next() {
		var r model.ComplianceResult
		if err := rows.Scan(&r.RuleID, &r.Status, &r.Evidence, &r.CheckedAt); err != nil {
			return nil, err
		}
		r.DeviceID = deviceID
		out = append(out, r)
	}
	return out, rows.Err()
}

// ComplianceFleet liefert je geprüftem Gerät Score und Zähler (Scope).
func (s *Store) ComplianceFleet(ctx context.Context, allowed map[string]bool) ([]model.ComplianceDevice, error) {
	scope, args, empty := scopeWhere(allowed)
	if empty {
		return []model.ComplianceDevice{}, nil
	}
	rows, err := s.db.QueryContext(ctx, s.rebind(`
		SELECT d.id, d.hostname, d.os, COALESCE(c.name,''), COALESCE(st.name,''), d.compliance_score, d.compliance_checked_at, d.compliance_profile,
			COALESCE(SUM(CASE WHEN r.status='pass' THEN 1 ELSE 0 END),0),
			COALESCE(SUM(CASE WHEN r.status='fail' THEN 1 ELSE 0 END),0),
			COALESCE(SUM(CASE WHEN r.status IN ('na','error') THEN 1 ELSE 0 END),0)
		FROM devices d
		LEFT JOIN sites st ON st.id = d.site_id LEFT JOIN clients c ON c.id = st.client_id
		LEFT JOIN compliance_results r ON r.device_id = d.id
		WHERE d.revoked = FALSE AND d.compliance_score IS NOT NULL`+scope+`
		GROUP BY d.id, d.hostname, d.os, c.name, st.name, d.compliance_score, d.compliance_checked_at, d.compliance_profile
		ORDER BY d.compliance_score, d.hostname`), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.ComplianceDevice{}
	for rows.Next() {
		var d model.ComplianceDevice
		var score sql.NullInt64
		var at sql.NullTime
		if err := rows.Scan(&d.DeviceID, &d.Hostname, &d.OS, &d.ClientName, &d.SiteName, &score, &at, &d.Profile, &d.Passed, &d.Failed, &d.Skipped); err != nil {
			return nil, err
		}
		d.Score = int(score.Int64)
		if at.Valid {
			d.CheckedAt = at.Time
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// ComplianceByRule liefert je Regel die Zahl bestandener/fehlgeschlagener Geräte (Scope).
func (s *Store) ComplianceByRule(ctx context.Context, allowed map[string]bool) ([]model.ComplianceRuleStat, error) {
	scope, args, empty := scopeWhere(allowed)
	if empty {
		return []model.ComplianceRuleStat{}, nil
	}
	rows, err := s.db.QueryContext(ctx, s.rebind(`
		SELECT r.rule_id,
			COALESCE(SUM(CASE WHEN r.status='pass' THEN 1 ELSE 0 END),0),
			COALESCE(SUM(CASE WHEN r.status='fail' THEN 1 ELSE 0 END),0),
			COALESCE(SUM(CASE WHEN r.status IN ('na','error') THEN 1 ELSE 0 END),0)
		FROM compliance_results r JOIN devices d ON d.id = r.device_id
		WHERE d.revoked = FALSE`+scope+` GROUP BY r.rule_id ORDER BY r.rule_id`), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.ComplianceRuleStat{}
	for rows.Next() {
		var r model.ComplianceRuleStat
		if err := rows.Scan(&r.RuleID, &r.Passed, &r.Failed, &r.Skipped); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ComplianceRuleDevices liefert die Geräte mit einem Status für eine Regel.
func (s *Store) ComplianceRuleDevices(ctx context.Context, allowed map[string]bool, ruleID, status string) ([]model.ComplianceResult, error) {
	scope, args, empty := scopeWhere(allowed)
	if empty {
		return []model.ComplianceResult{}, nil
	}
	q := `SELECT r.device_id, d.hostname, r.status, r.evidence, r.checked_at FROM compliance_results r JOIN devices d ON d.id = r.device_id
		WHERE d.revoked = FALSE AND r.rule_id = ?` + scope
	args = append([]any{ruleID}, args...)
	if status != "" {
		q += ` AND r.status = ?`
		args = append(args, status)
	}
	q += ` ORDER BY d.hostname`
	rows, err := s.db.QueryContext(ctx, s.rebind(q), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.ComplianceResult{}
	for rows.Next() {
		var r model.ComplianceResult
		if err := rows.Scan(&r.DeviceID, &r.Hostname, &r.Status, &r.Evidence, &r.CheckedAt); err != nil {
			return nil, err
		}
		r.RuleID = ruleID
		out = append(out, r)
	}
	return out, rows.Err()
}
