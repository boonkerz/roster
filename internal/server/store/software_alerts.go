package store

import (
	"context"
	"database/sql"
	"path"
	"strings"

	"github.com/boonkerz/roster/internal/server/model"
)

// Software-Änderungs-Benachrichtigungen: global gibt es den Standard
// (alert_config.alert_software), Richtlinien können ihn mit „on“/„off“ überstimmen
// (Vorrang Gerät > Standort > Client) und zusätzlich filtern; das Gerät selbst hat
// mit devices.software_alerts das letzte Wort über den Schalter.

// SoftwareAlertSetting ist die für ein Gerät wirksame Einstellung.
type SoftwareAlertSetting struct {
	Enabled    bool     `json:"enabled"`
	Inherited  bool     `json:"inherited"`             // Wert ohne Geräte-Override (Richtlinie bzw. Standard)
	Source     string   `json:"source"`                // device | policy | global
	PolicyID   string   `json:"policy_id,omitempty"`   // gewinnende Richtlinie (auch wenn das Gerät überschreibt)
	PolicyName string   `json:"policy_name,omitempty"` //
	Kinds      []string `json:"kinds"`                 // leer = alle Änderungsarten
	Ignore     []string `json:"ignore"`                // Glob-Muster, die nicht gemeldet werden
}

// SetPolicyNotifications speichert den Bereich „Benachrichtigungen“ einer Richtlinie.
func (s *Store) SetPolicyNotifications(ctx context.Context, policyID string, n model.PolicyNotifications) error {
	return s.affect(s.db.ExecContext(ctx, s.rebind(
		`UPDATE policies SET software_alerts=?, software_alert_kinds=?, software_ignore=? WHERE id=?`),
		n.SoftwareAlerts, kindsJoin(n.SoftwareAlertKinds), ignoreJoin(n.SoftwareIgnore), policyID))
}

// SetDeviceSoftwareAlerts setzt den Geräte-Override ("" = wie Richtlinie, on, off).
func (s *Store) SetDeviceSoftwareAlerts(ctx context.Context, id, mode string) error {
	return s.affect(s.db.ExecContext(ctx, s.rebind(`UPDATE devices SET software_alerts=? WHERE id=?`), mode, id))
}

// ResolveSoftwareAlerts ermittelt die wirksame Einstellung eines Geräts:
// gewinnende Richtlinie (Gerät > Standort > Client, nur solche mit on/off), sonst der
// globale Standard; ein Geräte-Override überschreibt nur den Schalter, nicht die Filter.
func (s *Store) ResolveSoftwareAlerts(ctx context.Context, deviceID string) (SoftwareAlertSetting, error) {
	var out SoftwareAlertSetting
	var siteID, clientID sql.NullString
	var devMode string
	if err := s.db.QueryRowContext(ctx, s.rebind(
		`SELECT d.site_id, s.client_id, d.software_alerts FROM devices d LEFT JOIN sites s ON s.id=d.site_id WHERE d.id=?`),
		deviceID).Scan(&siteID, &clientID, &devMode); err != nil {
		return out, err
	}

	var polID, polName, polMode, kinds, ignore string
	err := s.db.QueryRowContext(ctx, s.rebind(`
		SELECT p.id, p.name, p.software_alerts, p.software_alert_kinds, p.software_ignore
		FROM policy_assignments a JOIN policies p ON p.id = a.policy_id
		WHERE p.software_alerts <> '' AND (
			(a.target_type='device' AND a.target_id=?) OR
			(a.target_type='site'   AND a.target_id=?) OR
			(a.target_type='client' AND a.target_id=?))
		ORDER BY CASE a.target_type WHEN 'device' THEN 0 WHEN 'site' THEN 1 ELSE 2 END, p.name
		LIMIT 1`), deviceID, siteID.String, clientID.String).Scan(&polID, &polName, &polMode, &kinds, &ignore)
	switch {
	case err == nil:
		out.Enabled = polMode == "on"
		out.Source = "policy"
		out.PolicyID, out.PolicyName = polID, polName
		out.Kinds = kindsSplit(kinds)
		out.Ignore = ignoreSplit(ignore)
	case err == sql.ErrNoRows:
		cfg, err := s.GetAlertConfig(ctx)
		if err != nil {
			return out, err
		}
		out.Enabled = cfg.AlertSoftware
		out.Source = "global"
	default:
		return out, err
	}
	if out.Kinds == nil {
		out.Kinds = []string{}
	}
	if out.Ignore == nil {
		out.Ignore = []string{}
	}
	out.Inherited = out.Enabled
	switch devMode {
	case "on", "off":
		out.Enabled = devMode == "on"
		out.Source = "device"
	}
	return out, nil
}

// Filter behält nur die Ereignisse, die nach Änderungsart und Ignorier-Mustern
// gemeldet werden sollen.
func (st SoftwareAlertSetting) Filter(events []model.SoftwareEvent) []model.SoftwareEvent {
	out := events[:0:0]
	for _, ev := range events {
		if len(st.Kinds) > 0 && !containsString(st.Kinds, ev.Change) {
			continue
		}
		ignored := false
		for _, pat := range st.Ignore {
			if matchSoftwareName(pat, ev.Name) {
				ignored = true
				break
			}
		}
		if !ignored {
			out = append(out, ev)
		}
	}
	return out
}

// matchSoftwareName vergleicht ein Glob-Muster ohne Rücksicht auf Groß-/Kleinschreibung.
// path.Match ist plattformneutral (filepath.Match würde unter Windows „\“ als Trenner werten).
func matchSoftwareName(pattern, name string) bool {
	ok, err := path.Match(strings.ToLower(strings.TrimSpace(pattern)), strings.ToLower(name))
	return err == nil && ok
}

func containsString(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

func kindsJoin(kinds []string) string { return strings.Join(kinds, ",") }

func kindsSplit(s string) []string {
	var out []string
	for _, k := range strings.Split(s, ",") {
		if k = strings.TrimSpace(k); k != "" {
			out = append(out, k)
		}
	}
	return out
}

func ignoreJoin(patterns []string) string { return strings.Join(patterns, "\n") }

func ignoreSplit(s string) []string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			out = append(out, l)
		}
	}
	return out
}
