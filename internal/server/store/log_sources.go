package store

import (
	"context"
	"database/sql"
	"errors"
	"net"
	"strings"
	"time"

	"github.com/boonkerz/roster/internal/server/model"
	"github.com/boonkerz/roster/internal/shared"
)

// Log-Quellen einer Richtlinie (policy_log_sources): zusätzliche Protokolle, aus denen der
// Agent Anmelde-/Angriffsereignisse liest. Für das Bundle werden die Quellen aller
// zugewiesenen Richtlinien vereinigt (dedupliziert nach Typ+Ziel+Format).

const logSourceCols = `id, policy_id, name, type, target, format, pattern, kind, enabled`

func scanLogSource(row scanner) (model.PolicyLogSource, error) {
	var l model.PolicyLogSource
	err := row.Scan(&l.ID, &l.PolicyID, &l.Name, &l.Type, &l.Target, &l.Format, &l.Pattern, &l.Kind, &l.Enabled)
	return l, err
}

// logSourcesOf liefert die Log-Quellen einer Richtlinie.
func (s *Store) logSourcesOf(ctx context.Context, policyID string) ([]model.PolicyLogSource, error) {
	rows, err := s.db.QueryContext(ctx, s.rebind(`SELECT `+logSourceCols+` FROM policy_log_sources WHERE policy_id=? ORDER BY name, target`), policyID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.PolicyLogSource{}
	for rows.Next() {
		l, err := scanLogSource(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// GetLogSource liest eine Quelle.
func (s *Store) GetLogSource(ctx context.Context, id string) (*model.PolicyLogSource, error) {
	l, err := scanLogSource(s.db.QueryRowContext(ctx, s.rebind(`SELECT `+logSourceCols+` FROM policy_log_sources WHERE id=?`), id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &l, nil
}

// SaveLogSource legt an (ID leer) oder ändert.
func (s *Store) SaveLogSource(ctx context.Context, l *model.PolicyLogSource) error {
	if l.ID == "" {
		l.ID = NewID()
		_, err := s.db.ExecContext(ctx, s.rebind(`INSERT INTO policy_log_sources (`+logSourceCols+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`),
			l.ID, l.PolicyID, l.Name, l.Type, l.Target, l.Format, l.Pattern, l.Kind, l.Enabled)
		return err
	}
	return s.affect(s.db.ExecContext(ctx, s.rebind(`UPDATE policy_log_sources SET name=?, type=?, target=?, format=?, pattern=?, kind=?, enabled=? WHERE id=?`),
		l.Name, l.Type, l.Target, l.Format, l.Pattern, l.Kind, l.Enabled, l.ID))
}

// DeleteLogSource entfernt eine Quelle.
func (s *Store) DeleteLogSource(ctx context.Context, id string) error {
	return s.affect(s.db.ExecContext(ctx, s.rebind(`DELETE FROM policy_log_sources WHERE id=?`), id))
}

// effectiveLogSources vereinigt die aktiven Quellen der Richtlinien für das Agent-Bundle.
func (s *Store) effectiveLogSources(ctx context.Context, policyIDs []string) ([]shared.LogSource, error) {
	if len(policyIDs) == 0 {
		return nil, nil
	}
	in, args := placeholders(policyIDs)
	rows, err := s.db.QueryContext(ctx, s.rebind(`SELECT `+logSourceCols+` FROM policy_log_sources WHERE enabled = TRUE AND policy_id IN (`+in+`) ORDER BY name, target`), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	seen := map[string]bool{}
	var out []shared.LogSource
	for rows.Next() {
		l, err := scanLogSource(rows)
		if err != nil {
			return nil, err
		}
		key := l.Type + "|" + l.Target + "|" + l.Format + "|" + l.Pattern
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, shared.LogSource{ID: l.ID, Name: l.Name, Type: l.Type, Target: l.Target, Format: l.Format, Pattern: l.Pattern, Kind: l.Kind})
	}
	return out, rows.Err()
}

// MonitoredDeviceIDs liefert alle verwalteten Geräte, deren wirksame Richtlinien die
// Anmeldungs-Überwachung oder eine Log-Quelle aktiviert haben (Ziel für Flotten-Sperren),
// mit Hostname und OS.
func (s *Store) MonitoredDeviceIDs(ctx context.Context) ([]model.Device, error) {
	rows, err := s.db.QueryContext(ctx, s.rebind(`
		SELECT DISTINCT d.id, d.hostname, d.os FROM devices d
		LEFT JOIN sites st ON st.id = d.site_id
		JOIN policy_assignments a ON (a.target_type='device' AND a.target_id=d.id)
			OR (a.target_type='site' AND a.target_id=d.site_id)
			OR (a.target_type='client' AND a.target_id=st.client_id)
		WHERE d.revoked = FALSE AND d.managed = TRUE AND (
			EXISTS (SELECT 1 FROM policy_security ps WHERE ps.policy_id = a.policy_id AND ps.auth_monitor = TRUE)
			OR EXISTS (SELECT 1 FROM policy_log_sources ls WHERE ls.policy_id = a.policy_id AND ls.enabled = TRUE))
		ORDER BY d.hostname`))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Device
	for rows.Next() {
		var d model.Device
		if err := rows.Scan(&d.ID, &d.Hostname, &d.OS); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// FailSourcesInSubnet zählt die verschiedenen Quell-IPs eines Geräts mit Fehlversuchen
// seit since, die im selben IPv4-/24-Netz wie ip liegen (für Netzbereichs-Sperren).
func (s *Store) FailSourcesInSubnet(ctx context.Context, deviceID, ip string, since time.Time) (int, error) {
	p := net.ParseIP(ip).To4()
	if p == nil {
		return 0, nil
	}
	prefix := strings.Join(strings.Split(p.String(), ".")[:3], ".") + "."
	var n int
	err := s.db.QueryRowContext(ctx, s.rebind(`
		SELECT COUNT(DISTINCT source_ip) FROM auth_events
		WHERE device_id=? AND ts >= ? AND kind IN ('login_failed','invalid_user','probe') AND source_ip LIKE ? AND source_ip NOT LIKE ?`),
		deviceID, since.UTC(), prefix+"%", prefix+"%.%").Scan(&n)
	return n, err
}
