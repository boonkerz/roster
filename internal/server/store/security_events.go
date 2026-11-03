package store

import (
	"context"
	"database/sql"
	"github.com/boonkerz/roster/internal/server/geoip"
	"sort"
	"strings"
	"time"

	"github.com/boonkerz/roster/internal/server/model"
	"github.com/boonkerz/roster/internal/shared"
)

// Einstellungs-Schlüssel der Anmeldungs-Überwachung (app_settings).
const (
	SettingSecBFThreshold      = "sec_bf_threshold"       // Fehlversuche je IP (Standard 10)
	SettingSecBFWindowMin      = "sec_bf_window_min"      // Fenster in Minuten (Standard 10)
	SettingSecNewIPAlert       = "sec_new_ip_alert"       // "0" = aus
	SettingSecNewIPIgnorePriv  = "sec_new_ip_ignore_priv" // "1" = private IPs ignorieren
	SettingSecLearnDays        = "sec_learn_days"         // Lernphase (Standard 7)
	SettingSecNeverBlock       = "sec_never_block"        // CIDRs, nie sperren (Phase 8)
	SettingSecResponseCooldown = "sec_response_cooldown_min"
	SettingSecInvalidUserMax   = "sec_invalid_user_max"   // Fehlversuche mit unbekanntem Benutzer je IP in 24 h (0 = aus)
	SettingSecScanThreshold    = "sec_scan_threshold"     // verdächtige Anfragen je IP im Fenster (Standard 20)
	SettingSecAutoBlock        = "sec_auto_block"         // "1" = Brute-Force/Scan automatisch sperren (ohne Richtlinien-Regel)
	SettingSecAutoBlockMinutes = "sec_auto_block_minutes" // Dauer der automatischen Sperre (0 = dauerhaft)
	SettingSecFleetBlock       = "sec_fleet_block"        // "1" = automatische Sperren auf allen überwachten Geräten
	SettingSecSubnetIPs        = "sec_subnet_ips"         // ab so vielen IPs aus einem /24 das ganze Netz sperren (0 = aus)
	SettingSecBlockPrivate     = "sec_block_private"      // "1" = auch private/interne Adressen (RFC 1918, Docker-Netze) automatisch sperren
)

// InsertAuthEvents speichert Anmeldeereignisse; Duplikate (gleiche record_id …) werden
// still übersprungen. Liefert die tatsächlich neuen Ereignisse (chronologisch).
func (s *Store) InsertAuthEvents(ctx context.Context, deviceID string, events []shared.AuthEvent) ([]shared.AuthEvent, error) {
	if len(events) == 0 {
		return nil, nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback() //nolint:errcheck
	var fresh []shared.AuthEvent
	for _, e := range events {
		if e.Kind == "" || e.Time.IsZero() {
			continue
		}
		res, err := tx.ExecContext(ctx, s.rebind(`
			INSERT INTO auth_events (id, device_id, ts, kind, username, source_ip, service, detail, record_id)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT (device_id, record_id, ts, kind, username, source_ip) DO NOTHING`),
			NewID(), deviceID, e.Time.UTC(), e.Kind, e.User, e.SourceIP, e.Service, truncate(e.Detail, 500), e.RecordID)
		if err != nil {
			return nil, err
		}
		if n, _ := res.RowsAffected(); n > 0 {
			fresh = append(fresh, e)
		}
	}
	// Beginn der Überwachung merken (Lernphase).
	if _, err := tx.ExecContext(ctx, s.rebind(`UPDATE devices SET auth_monitor_since=? WHERE id=? AND auth_monitor_since IS NULL`), time.Now().UTC(), deviceID); err != nil {
		return nil, err
	}
	return fresh, tx.Commit()
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

// FailedLoginsSince liefert je Quell-IP die Zeitpunkte fehlgeschlagener Anmeldungen seit since.
func (s *Store) FailedLoginsSince(ctx context.Context, deviceID string, since time.Time) (map[string][]time.Time, error) {
	return s.eventTimesSince(ctx, deviceID, since, "('login_failed','invalid_user')")
}

// InvalidUserSince liefert je Quell-IP die Zeitpunkte von Versuchen mit unbekanntem Benutzer.
func (s *Store) InvalidUserSince(ctx context.Context, deviceID string, since time.Time) (map[string][]time.Time, error) {
	return s.eventTimesSince(ctx, deviceID, since, "('invalid_user')")
}

// ProbesSince liefert je Quell-IP die Zeitpunkte verdächtiger Anfragen (Web-Scanner …).
func (s *Store) ProbesSince(ctx context.Context, deviceID string, since time.Time) (map[string][]time.Time, error) {
	return s.eventTimesSince(ctx, deviceID, since, "('probe')")
}

func (s *Store) eventTimesSince(ctx context.Context, deviceID string, since time.Time, kinds string) (map[string][]time.Time, error) {
	rows, err := s.db.QueryContext(ctx, s.rebind(`
		SELECT source_ip, ts FROM auth_events WHERE device_id=? AND ts >= ? AND kind IN `+kinds+` AND source_ip <> ''`),
		deviceID, since.UTC())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string][]time.Time{}
	for rows.Next() {
		var ip string
		var ts time.Time
		if err := rows.Scan(&ip, &ts); err != nil {
			return nil, err
		}
		out[ip] = append(out[ip], ts)
	}
	return out, rows.Err()
}

// RecentIncidentIPs liefert je Quell-IP den letzten Vorfall einer Art seit since.
func (s *Store) RecentIncidentIPs(ctx context.Context, deviceID, kind string, since time.Time) (map[string]time.Time, error) {
	// Kein MAX(ts): Aggregat-Ausdrücke kommen aus SQLite ohne Zeittyp zurück; stattdessen
	// sortiert lesen und je IP den letzten Wert behalten.
	rows, err := s.db.QueryContext(ctx, s.rebind(`
		SELECT source_ip, ts FROM security_incidents WHERE device_id=? AND kind=? AND ts >= ? ORDER BY ts`),
		deviceID, kind, since.UTC())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]time.Time{}
	for rows.Next() {
		var ip string
		var ts time.Time
		if err := rows.Scan(&ip, &ts); err != nil {
			return nil, err
		}
		out[ip] = ts
	}
	return out, rows.Err()
}

// KnownIPs liefert die bekannten (Benutzer, IP)-Paare eines Geräts als Set "user@ip".
func (s *Store) KnownIPs(ctx context.Context, deviceID string) (map[string]bool, error) {
	rows, err := s.db.QueryContext(ctx, s.rebind(`SELECT username, ip FROM device_known_ips WHERE device_id=?`), deviceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var u, ip string
		if err := rows.Scan(&u, &ip); err != nil {
			return nil, err
		}
		out[u+"@"+ip] = true
	}
	return out, rows.Err()
}

// UpsertKnownIPs merkt sich erfolgreiche Anmeldungen (Benutzer, IP).
func (s *Store) UpsertKnownIPs(ctx context.Context, deviceID string, events []shared.AuthEvent) error {
	now := time.Now().UTC()
	for _, e := range events {
		if e.Kind != "login_ok" || e.SourceIP == "" {
			continue
		}
		res, err := s.db.ExecContext(ctx, s.rebind(`UPDATE device_known_ips SET last_seen=? WHERE device_id=? AND username=? AND ip=?`), now, deviceID, e.User, e.SourceIP)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			if _, err := s.db.ExecContext(ctx, s.rebind(`INSERT INTO device_known_ips (device_id, username, ip, first_seen, last_seen) VALUES (?, ?, ?, ?, ?)`),
				deviceID, e.User, e.SourceIP, now, now); err != nil {
				return err
			}
		}
	}
	return nil
}

// AuthMonitorSince liefert den Beginn der Überwachung (Zero, wenn noch nie).
func (s *Store) AuthMonitorSince(ctx context.Context, deviceID string) time.Time {
	var t sql.NullTime
	_ = s.db.QueryRowContext(ctx, s.rebind(`SELECT auth_monitor_since FROM devices WHERE id=?`), deviceID).Scan(&t)
	if t.Valid {
		return t.Time
	}
	return time.Time{}
}

// InsertIncident legt einen Vorfall an.
func (s *Store) InsertIncident(ctx context.Context, inc *model.SecurityIncident) error {
	if inc.ID == "" {
		inc.ID = NewID()
	}
	if inc.Status == "" {
		inc.Status = "open"
	}
	_, err := s.db.ExecContext(ctx, s.rebind(`
		INSERT INTO security_incidents (id, device_id, ts, kind, severity, source_ip, username, title, detail, status, response, response_status, response_command_id)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`),
		inc.ID, inc.DeviceID, inc.TS.UTC(), inc.Kind, inc.Severity, inc.SourceIP, inc.Username, inc.Title, truncate(inc.Detail, 2000), inc.Status,
		inc.Response, inc.ResponseStatus, inc.ResponseCommandID)
	return err
}

// SetIncidentResponse vermerkt die automatische Reaktion an einem Vorfall.
func (s *Store) SetIncidentResponse(ctx context.Context, id, response, status, commandID string) error {
	_, err := s.db.ExecContext(ctx, s.rebind(`UPDATE security_incidents SET response=?, response_status=?, response_command_id=? WHERE id=?`), response, status, commandID, id)
	return err
}

// IncidentFilter grenzt die Vorfallsliste ein.
type IncidentFilter struct {
	Allowed  map[string]bool
	DeviceID string
	ClientID string
	SiteID   string
	Since    time.Time
	Status   string
	Kind     string
	Severity string
	Query    string
	Country  string // ISO-Code der Quell-IP; "?" = unbekannt/keine (wird nach dem Laden über GeoIP gefiltert)
	Limit    int
}

const incidentCols = `i.id, i.device_id, d.hostname, COALESCE(c.name,''), COALESCE(st.name,''), i.ts, i.kind, i.severity, i.source_ip, i.username,
	i.title, i.detail, i.status, i.response, i.response_status, i.response_command_id, i.acknowledged_by, i.acknowledged_at`

func scanIncident(rows *sql.Rows) (model.SecurityIncident, error) {
	var i model.SecurityIncident
	var ackAt sql.NullTime
	err := rows.Scan(&i.ID, &i.DeviceID, &i.Hostname, &i.ClientName, &i.SiteName, &i.TS, &i.Kind, &i.Severity, &i.SourceIP, &i.Username,
		&i.Title, &i.Detail, &i.Status, &i.Response, &i.ResponseStatus, &i.ResponseCommandID, &i.AcknowledgedBy, &ackAt)
	if ackAt.Valid {
		t := ackAt.Time
		i.AcknowledgedAt = &t
	}
	i.Country = geoip.Country(i.SourceIP)
	return i, err
}

// Incidents liefert Vorfälle (neueste zuerst).
func (s *Store) Incidents(ctx context.Context, f IncidentFilter) ([]model.SecurityIncident, error) {
	scope, args, empty := scopeWhere(f.Allowed)
	if empty {
		return []model.SecurityIncident{}, nil
	}
	q := `SELECT ` + incidentCols + ` FROM security_incidents i JOIN devices d ON d.id = i.device_id
		LEFT JOIN sites st ON st.id = d.site_id LEFT JOIN clients c ON c.id = st.client_id WHERE d.revoked = FALSE` + scope
	if f.DeviceID != "" {
		q += ` AND i.device_id = ?`
		args = append(args, f.DeviceID)
	}
	if f.ClientID != "" {
		q += ` AND st.client_id = ?`
		args = append(args, f.ClientID)
	}
	if f.SiteID != "" {
		q += ` AND d.site_id = ?`
		args = append(args, f.SiteID)
	}
	if !f.Since.IsZero() {
		q += ` AND i.ts >= ?`
		args = append(args, f.Since.UTC())
	}
	if f.Status != "" {
		q += ` AND i.status = ?`
		args = append(args, f.Status)
	}
	if f.Kind != "" {
		q += ` AND i.kind = ?`
		args = append(args, f.Kind)
	}
	if f.Severity != "" {
		q += ` AND i.severity = ?`
		args = append(args, f.Severity)
	}
	if t := strings.TrimSpace(f.Query); t != "" {
		like := "%" + strings.ToLower(t) + "%"
		q += ` AND (LOWER(i.title) LIKE ? OR LOWER(i.source_ip) LIKE ? OR LOWER(i.username) LIKE ? OR LOWER(d.hostname) LIKE ?)`
		args = append(args, like, like, like, like)
	}
	limit := f.Limit
	if limit <= 0 || limit > 5000 {
		limit = 500
	}
	country := strings.ToUpper(strings.TrimSpace(f.Country))
	queryLimit := limit
	if country != "" {
		// Land steht nicht in der Tabelle (GeoIP beim Lesen) → großzügig laden, danach filtern.
		queryLimit = 50000
	}
	q += ` ORDER BY i.ts DESC LIMIT ?`
	args = append(args, queryLimit)
	rows, err := s.db.QueryContext(ctx, s.rebind(q), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.SecurityIncident{}
	for rows.Next() {
		i, err := scanIncident(rows)
		if err != nil {
			return nil, err
		}
		if country != "" {
			if country == "?" {
				if i.SourceIP == "" || i.Country != "" {
					continue
				}
			} else if i.Country != country {
				continue
			}
		}
		out = append(out, i)
		if len(out) >= limit {
			break
		}
	}
	return out, rows.Err()
}

// GetIncident liest einen Vorfall.
func (s *Store) GetIncident(ctx context.Context, id string) (*model.SecurityIncident, error) {
	rows, err := s.db.QueryContext(ctx, s.rebind(`SELECT `+incidentCols+` FROM security_incidents i JOIN devices d ON d.id = i.device_id
		LEFT JOIN sites st ON st.id = d.site_id LEFT JOIN clients c ON c.id = st.client_id WHERE i.id = ?`), id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, ErrNotFound
	}
	i, err := scanIncident(rows)
	if err != nil {
		return nil, err
	}
	return &i, nil
}

// SetIncidentStatus setzt open | acknowledged | resolved (mit Bearbeiter).
func (s *Store) SetIncidentStatus(ctx context.Context, id, status, by string) error {
	var at any
	if status != "open" {
		at = time.Now().UTC()
	} else {
		by = ""
	}
	return s.affect(s.db.ExecContext(ctx, s.rebind(`UPDATE security_incidents SET status=?, acknowledged_by=?, acknowledged_at=? WHERE id=?`), status, by, at, id))
}

// AuthEventFilter grenzt die Rohereignisse ein.
type AuthEventFilter struct {
	Allowed  map[string]bool
	DeviceID string
	ClientID string
	SiteID   string
	Since    time.Time
	Kind     string
	IP       string
	User     string
	Query    string
	Limit    int
}

// AuthEvents liefert Rohereignisse (neueste zuerst).
func (s *Store) AuthEvents(ctx context.Context, f AuthEventFilter) ([]model.AuthEvent, error) {
	scope, args, empty := scopeWhere(f.Allowed)
	if empty {
		return []model.AuthEvent{}, nil
	}
	q := `SELECT a.id, a.device_id, d.hostname, COALESCE(c.name,''), a.ts, a.kind, a.username, a.source_ip, a.service, a.detail
		FROM auth_events a JOIN devices d ON d.id = a.device_id
		LEFT JOIN sites st ON st.id = d.site_id LEFT JOIN clients c ON c.id = st.client_id WHERE d.revoked = FALSE` + scope
	if f.DeviceID != "" {
		q += ` AND a.device_id = ?`
		args = append(args, f.DeviceID)
	}
	if f.ClientID != "" {
		q += ` AND st.client_id = ?`
		args = append(args, f.ClientID)
	}
	if f.SiteID != "" {
		q += ` AND d.site_id = ?`
		args = append(args, f.SiteID)
	}
	if !f.Since.IsZero() {
		q += ` AND a.ts >= ?`
		args = append(args, f.Since.UTC())
	}
	if f.Kind != "" {
		if f.Kind == "failed" {
			q += ` AND a.kind IN ('login_failed','invalid_user')`
		} else {
			q += ` AND a.kind = ?`
			args = append(args, f.Kind)
		}
	}
	if f.IP != "" {
		q += ` AND a.source_ip = ?`
		args = append(args, f.IP)
	}
	if f.User != "" {
		q += ` AND a.username = ?`
		args = append(args, f.User)
	}
	if t := strings.TrimSpace(f.Query); t != "" {
		like := "%" + strings.ToLower(t) + "%"
		q += ` AND (LOWER(a.username) LIKE ? OR a.source_ip LIKE ? OR LOWER(d.hostname) LIKE ? OR LOWER(a.detail) LIKE ?)`
		args = append(args, like, like, like, like)
	}
	limit := f.Limit
	if limit <= 0 || limit > 5000 {
		limit = 1000
	}
	q += ` ORDER BY a.ts DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, s.rebind(q), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.AuthEvent{}
	for rows.Next() {
		var e model.AuthEvent
		if err := rows.Scan(&e.ID, &e.DeviceID, &e.Hostname, &e.ClientName, &e.TS, &e.Kind, &e.Username, &e.SourceIP, &e.Service, &e.Detail); err != nil {
			return nil, err
		}
		e.Country = geoip.Country(e.SourceIP)
		out = append(out, e)
	}
	return out, rows.Err()
}

// SecuritySummary zählt offene Vorfälle, Fehlversuche (24 h) und die häufigsten Quell-IPs.
func (s *Store) SecuritySummary(ctx context.Context, allowed map[string]bool) (model.SecuritySummary, error) {
	var sum model.SecuritySummary
	sum.TopIPs = []model.IPCount{}
	scope, args, empty := scopeWhere(allowed)
	if empty {
		return sum, nil
	}
	day := time.Now().Add(-24 * time.Hour).UTC()
	if err := s.db.QueryRowContext(ctx, s.rebind(`
		SELECT COALESCE(SUM(CASE WHEN i.status='open' THEN 1 ELSE 0 END),0),
			COALESCE(SUM(CASE WHEN i.status='open' AND i.severity='critical' THEN 1 ELSE 0 END),0)
		FROM security_incidents i JOIN devices d ON d.id = i.device_id WHERE d.revoked = FALSE`+scope), args...).Scan(&sum.OpenIncidents, &sum.CriticalOpen); err != nil {
		return sum, err
	}
	args2 := append([]any{day}, args...)
	if err := s.db.QueryRowContext(ctx, s.rebind(`
		SELECT COUNT(*) FROM auth_events a JOIN devices d ON d.id = a.device_id
		WHERE a.kind IN ('login_failed','invalid_user') AND a.ts >= ? AND d.revoked = FALSE`+scope), args2...).Scan(&sum.FailedLogins24h); err != nil {
		return sum, err
	}
	if err := s.db.QueryRowContext(ctx, s.rebind(`SELECT COUNT(*) FROM devices d WHERE d.auth_monitor_since IS NOT NULL AND d.revoked = FALSE`+scope), args...).Scan(&sum.DevicesMonitored); err != nil {
		return sum, err
	}
	rows, err := s.db.QueryContext(ctx, s.rebind(`
		SELECT a.source_ip, COUNT(*) AS n FROM auth_events a JOIN devices d ON d.id = a.device_id
		WHERE a.kind IN ('login_failed','invalid_user') AND a.source_ip <> '' AND a.ts >= ? AND d.revoked = FALSE`+scope+`
		GROUP BY a.source_ip ORDER BY n DESC LIMIT 10`), args2...)
	if err != nil {
		return sum, err
	}
	defer rows.Close()
	for rows.Next() {
		var c model.IPCount
		if err := rows.Scan(&c.IP, &c.Count); err != nil {
			return sum, err
		}
		c.Country = geoip.Country(c.IP)
		sum.TopIPs = append(sum.TopIPs, c)
	}
	if err := rows.Err(); err != nil {
		return sum, err
	}
	rows.Close()
	// Herkunftsländer: alle Quell-IPs der Fehlversuche (24 h) zählen, dann je Land bündeln.
	attack, err := s.ipCounts(ctx, `SELECT a.source_ip, COUNT(*) FROM auth_events a JOIN devices d ON d.id = a.device_id
		WHERE a.kind IN ('login_failed','invalid_user','probe') AND a.source_ip <> '' AND a.ts >= ? AND d.revoked = FALSE`+scope+` GROUP BY a.source_ip`, args2...)
	if err != nil {
		return sum, err
	}
	sum.TopCountries = CountriesOf(attack)
	month := append([]any{time.Now().Add(-30 * 24 * time.Hour).UTC()}, args...)
	// Ohne GROUP BY, damit SQLite den Zeitstempel-Index nutzt (mit GROUP BY wählt der Planer
	// ohne Statistik den IP-Index und liest die gesamte – mit Flottensperre große – Tabelle);
	// gezählt wird in Go.
	blocked, err := s.ipCountsGrouped(ctx, `SELECT b.ip FROM ip_blocks b JOIN devices d ON d.id = b.device_id
		WHERE b.created_at >= ? AND d.revoked = FALSE`+scope, month...)
	if err != nil {
		return sum, err
	}
	sum.BlockedCountries = CountriesOf(blocked)
	return sum, nil
}

// ipCounts führt eine Abfrage „IP, Anzahl“ aus.
func (s *Store) ipCounts(ctx context.Context, q string, args ...any) ([]model.IPCount, error) {
	rows, err := s.db.QueryContext(ctx, s.rebind(q), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.IPCount
	for rows.Next() {
		var c model.IPCount
		if err := rows.Scan(&c.IP, &c.Count); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ipCountsGrouped führt eine Abfrage „IP“ (eine Zeile je Vorkommen) aus und zählt je IP.
func (s *Store) ipCountsGrouped(ctx context.Context, q string, args ...any) ([]model.IPCount, error) {
	rows, err := s.db.QueryContext(ctx, s.rebind(q), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	idx := map[string]int{}
	var out []model.IPCount
	for rows.Next() {
		var ip string
		if err := rows.Scan(&ip); err != nil {
			return nil, err
		}
		i, ok := idx[ip]
		if !ok {
			idx[ip] = len(out)
			out = append(out, model.IPCount{IP: ip})
			i = len(out) - 1
		}
		out[i].Count++
	}
	return out, rows.Err()
}

// CountriesOf bündelt IP-Zähler nach Land (absteigend nach Zahl der IPs, unbekannt zuletzt).
func CountriesOf(ips []model.IPCount) []model.CountryCount {
	idx := map[string]int{}
	out := []model.CountryCount{}
	for _, c := range ips {
		cc := geoip.Country(c.IP)
		i, ok := idx[cc]
		if !ok {
			idx[cc] = len(out)
			out = append(out, model.CountryCount{Country: cc})
			i = len(out) - 1
		}
		out[i].IPs++
		out[i].Count += c.Count
	}
	sort.SliceStable(out, func(i, j int) bool {
		if (out[i].Country == "") != (out[j].Country == "") {
			return out[i].Country != ""
		}
		if out[i].IPs != out[j].IPs {
			return out[i].IPs > out[j].IPs
		}
		return out[i].Count > out[j].Count
	})
	return out
}

// SiteIPs liefert je Standort die (unterschiedlichen) IPs einer Abfrage – für die
// Länder-Kennzahlen des Dashboards. kind: "blocked" (Sperren der letzten 30 Tage) oder
// "attack" (Quell-IPs von Fehlversuchen/verdächtigen Anfragen der letzten 7 Tage).
func (s *Store) SiteIPs(ctx context.Context, allowed map[string]bool, kind string) (map[string][]string, error) {
	scope, args, empty := scopeWhere(allowed)
	out := map[string][]string{}
	if empty {
		return out, nil
	}
	var q string
	var since time.Time
	switch kind {
	case "blocked":
		since = time.Now().Add(-30 * 24 * time.Hour).UTC()
		q = `SELECT DISTINCT COALESCE(d.site_id,''), b.ip FROM ip_blocks b JOIN devices d ON d.id = b.device_id
			WHERE b.created_at >= ? AND d.revoked = FALSE` + scope
	default:
		since = time.Now().Add(-7 * 24 * time.Hour).UTC()
		q = `SELECT DISTINCT COALESCE(d.site_id,''), a.source_ip FROM auth_events a JOIN devices d ON d.id = a.device_id
			WHERE a.kind IN ('login_failed','invalid_user','probe') AND a.source_ip <> '' AND a.ts >= ? AND d.revoked = FALSE` + scope
	}
	rows, err := s.db.QueryContext(ctx, s.rebind(q), append([]any{since}, args...)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var site, ip string
		if err := rows.Scan(&site, &ip); err != nil {
			return nil, err
		}
		out[site] = append(out[site], ip)
	}
	return out, rows.Err()
}

// PruneSecurityEvents löscht Rohereignisse älter als eventCutoff und Vorfälle älter als incidentCutoff.
func (s *Store) PruneSecurityEvents(ctx context.Context, eventCutoff, incidentCutoff time.Time) error {
	if _, err := s.db.ExecContext(ctx, s.rebind(`DELETE FROM auth_events WHERE ts < ?`), eventCutoff.UTC()); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, s.rebind(`DELETE FROM security_incidents WHERE ts < ? AND status <> 'open'`), incidentCutoff.UTC())
	return err
}
