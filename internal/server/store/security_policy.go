package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"

	"github.com/boonkerz/roster/internal/server/model"
	"github.com/boonkerz/roster/internal/shared"
)

// Bereich „Sicherheitsüberwachung“ einer Richtlinie: policy_security (Schalter/Intervalle)
// und policy_fim_rules (überwachte Pfade). Für das Agent-Bundle werden alle zugewiesenen
// Richtlinien vereinigt (Regeln: Union; Schalter: ODER; Intervalle: Minimum).

func defaultPolicySecurity() model.PolicySecurity {
	return model.PolicySecurity{FIMIntervalMin: 60, ComplianceProfile: "server", ComplianceDisabled: []string{}, ComplianceIntervalH: 24}
}

// securityOf liest die Schalter einer Richtlinie (Standardwerte, wenn keine Zeile existiert).
func (s *Store) securityOf(ctx context.Context, policyID string) (model.PolicySecurity, error) {
	sec := defaultPolicySecurity()
	var disabled string
	err := s.db.QueryRowContext(ctx, s.rebind(`
		SELECT fim_interval_min, auth_monitor, compliance_enabled, compliance_profile, compliance_disabled, compliance_interval_h, fim_whodata, state_monitor, rootcheck
		FROM policy_security WHERE policy_id=?`), policyID).Scan(&sec.FIMIntervalMin, &sec.AuthMonitor, &sec.ComplianceEnabled,
		&sec.ComplianceProfile, &disabled, &sec.ComplianceIntervalH, &sec.FIMWhoData, &sec.StateMonitor, &sec.Rootcheck)
	if errors.Is(err, sql.ErrNoRows) {
		return sec, nil
	}
	if err != nil {
		return sec, err
	}
	sec.ComplianceDisabled = kindsSplit(disabled)
	if sec.ComplianceDisabled == nil {
		sec.ComplianceDisabled = []string{}
	}
	return sec, nil
}

// SetPolicySecurity speichert die Schalter (Upsert).
func (s *Store) SetPolicySecurity(ctx context.Context, policyID string, sec model.PolicySecurity) error {
	disabled := kindsJoin(sec.ComplianceDisabled)
	res, err := s.db.ExecContext(ctx, s.rebind(`
		UPDATE policy_security SET fim_interval_min=?, auth_monitor=?, compliance_enabled=?, compliance_profile=?,
			compliance_disabled=?, compliance_interval_h=?, fim_whodata=?, state_monitor=?, rootcheck=? WHERE policy_id=?`),
		sec.FIMIntervalMin, sec.AuthMonitor, sec.ComplianceEnabled, sec.ComplianceProfile, disabled, sec.ComplianceIntervalH, sec.FIMWhoData, sec.StateMonitor, sec.Rootcheck, policyID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		// Richtlinie muss existieren (FK) – sonst ErrNotFound.
		var exists int
		if err := s.db.QueryRowContext(ctx, s.rebind(`SELECT COUNT(*) FROM policies WHERE id=?`), policyID).Scan(&exists); err != nil || exists == 0 {
			return ErrNotFound
		}
		_, err = s.db.ExecContext(ctx, s.rebind(`
			INSERT INTO policy_security (policy_id, fim_interval_min, auth_monitor, compliance_enabled, compliance_profile, compliance_disabled, compliance_interval_h, fim_whodata, state_monitor, rootcheck)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`),
			policyID, sec.FIMIntervalMin, sec.AuthMonitor, sec.ComplianceEnabled, sec.ComplianceProfile, disabled, sec.ComplianceIntervalH, sec.FIMWhoData, sec.StateMonitor, sec.Rootcheck)
	}
	return err
}

func scanFIMRule(row scanner) (model.PolicyFIMRule, error) {
	var r model.PolicyFIMRule
	var exclude, platforms string
	if err := row.Scan(&r.ID, &r.PolicyID, &r.Path, &r.Recursive, &exclude, &platforms, &r.Alert); err != nil {
		return r, err
	}
	r.Exclude = ignoreSplit(exclude)
	if r.Exclude == nil {
		r.Exclude = []string{}
	}
	_ = json.Unmarshal([]byte(platforms), &r.Platforms)
	if r.Platforms == nil {
		r.Platforms = []string{}
	}
	return r, nil
}

// fimRulesOf liefert die FIM-Regeln einer Richtlinie.
func (s *Store) fimRulesOf(ctx context.Context, policyID string) ([]model.PolicyFIMRule, error) {
	rows, err := s.db.QueryContext(ctx, s.rebind(`
		SELECT id, policy_id, path, recursive, exclude, platforms, alert FROM policy_fim_rules WHERE policy_id=? ORDER BY path`), policyID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.PolicyFIMRule{}
	for rows.Next() {
		r, err := scanFIMRule(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// GetFIMRule liest eine Regel.
func (s *Store) GetFIMRule(ctx context.Context, id string) (*model.PolicyFIMRule, error) {
	row := s.db.QueryRowContext(ctx, s.rebind(`
		SELECT id, policy_id, path, recursive, exclude, platforms, alert FROM policy_fim_rules WHERE id=?`), id)
	r, err := scanFIMRule(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// SaveFIMRule legt eine Regel an (ID leer) oder ändert sie.
func (s *Store) SaveFIMRule(ctx context.Context, r *model.PolicyFIMRule) error {
	platforms, _ := json.Marshal(r.Platforms)
	if r.ID == "" {
		r.ID = NewID()
		_, err := s.db.ExecContext(ctx, s.rebind(`
			INSERT INTO policy_fim_rules (id, policy_id, path, recursive, exclude, platforms, alert) VALUES (?, ?, ?, ?, ?, ?, ?)`),
			r.ID, r.PolicyID, r.Path, r.Recursive, ignoreJoin(r.Exclude), string(platforms), r.Alert)
		return err
	}
	return s.affect(s.db.ExecContext(ctx, s.rebind(`
		UPDATE policy_fim_rules SET path=?, recursive=?, exclude=?, platforms=?, alert=? WHERE id=?`),
		r.Path, r.Recursive, ignoreJoin(r.Exclude), string(platforms), r.Alert, r.ID))
}

// DeleteFIMRule entfernt eine Regel.
func (s *Store) DeleteFIMRule(ctx context.Context, id string) error {
	return s.affect(s.db.ExecContext(ctx, s.rebind(`DELETE FROM policy_fim_rules WHERE id=?`), id))
}

// effectiveSecurity vereinigt den Sicherheitsbereich der angegebenen Richtlinien zum
// Agent-Bundle; nil, wenn nichts aktiv ist.
func (s *Store) effectiveSecurity(ctx context.Context, policyIDs []string) (*shared.SecuritySpec, error) {
	if len(policyIDs) == 0 {
		return nil, nil
	}
	in, args := placeholders(policyIDs)
	spec := &shared.SecuritySpec{}
	fimInterval, compInterval := 0, 0
	whoData := false
	var compDisabled map[string]int
	compCount := 0
	rows, err := s.db.QueryContext(ctx, s.rebind(`
		SELECT ps.fim_interval_min, ps.auth_monitor, ps.compliance_enabled, ps.compliance_profile, ps.compliance_disabled, ps.compliance_interval_h, ps.fim_whodata, ps.state_monitor, ps.rootcheck
		FROM policy_security ps JOIN policies p ON p.id = ps.policy_id WHERE ps.policy_id IN (`+in+`) ORDER BY p.name`), args...)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var fi, ci int
		var auth, comp, who, state, root bool
		var profile, disabled string
		if err := rows.Scan(&fi, &auth, &comp, &profile, &disabled, &ci, &who, &state, &root); err != nil {
			rows.Close()
			return nil, err
		}
		if fi > 0 && (fimInterval == 0 || fi < fimInterval) {
			fimInterval = fi
		}
		spec.AuthMonitor = spec.AuthMonitor || auth
		spec.StateMonitor = spec.StateMonitor || state
		spec.Rootcheck = spec.Rootcheck || root
		whoData = whoData || who
		if comp {
			compCount++
			if spec.Compliance == nil {
				spec.Compliance = &shared.ComplianceSpec{Enabled: true, Profile: profile}
			}
			if profile == "workstation" {
				spec.Compliance.Profile = "workstation" // strengere Sicht gewinnt bei Mischung
			}
			if ci > 0 && (compInterval == 0 || ci < compInterval) {
				compInterval = ci
			}
			if compDisabled == nil {
				compDisabled = map[string]int{}
			}
			for _, d := range kindsSplit(disabled) {
				compDisabled[d]++
			}
		}
	}
	rows.Close()
	if spec.Compliance != nil {
		spec.Compliance.IntervalH = compInterval
		if spec.Compliance.IntervalH == 0 {
			spec.Compliance.IntervalH = 24
		}
		// Eine Regel ist nur dann abgeschaltet, wenn ALLE Compliance-Richtlinien sie abschalten.
		for id, n := range compDisabled {
			if n == compCount {
				spec.Compliance.Disabled = append(spec.Compliance.Disabled, id)
			}
		}
	}
	rr, err := s.db.QueryContext(ctx, s.rebind(`
		SELECT id, policy_id, path, recursive, exclude, platforms, alert FROM policy_fim_rules WHERE policy_id IN (`+in+`) ORDER BY path`), args...)
	if err != nil {
		return nil, err
	}
	defer rr.Close()
	seen := map[string]bool{}
	var rules []shared.FIMRule
	for rr.Next() {
		r, err := scanFIMRule(rr)
		if err != nil {
			return nil, err
		}
		key := r.Path + "|" + boolStr(r.Recursive) + "|" + strings.Join(r.Platforms, ",")
		if seen[key] {
			continue
		}
		seen[key] = true
		rules = append(rules, shared.FIMRule{ID: r.ID, Path: r.Path, Recursive: r.Recursive, Exclude: r.Exclude, Platforms: r.Platforms, Alert: r.Alert})
	}
	if len(rules) > 0 {
		if fimInterval == 0 {
			fimInterval = 60
		}
		spec.FIM = &shared.FIMSpec{IntervalMin: fimInterval, Rules: rules, WhoData: whoData}
	}
	rr.Close()
	if spec.LogSources, err = s.effectiveLogSources(ctx, policyIDs); err != nil {
		return nil, err
	}
	if spec.FIM == nil && !spec.AuthMonitor && spec.Compliance == nil && len(spec.LogSources) == 0 {
		return nil, nil
	}
	return spec, nil
}

func boolStr(b bool) string {
	if b {
		return "1"
	}
	return "0"
}
