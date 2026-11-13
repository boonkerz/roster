package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/boonkerz/roster/internal/server/geoip"
	"github.com/boonkerz/roster/internal/server/threat"
	"net"
	"strings"
	"time"

	"github.com/boonkerz/roster/internal/server/model"
)

func scanResponse(row scanner) (model.PolicyResponse, error) {
	var r model.PolicyResponse
	var scriptID sql.NullString
	var platforms string
	if err := row.Scan(&r.ID, &r.PolicyID, &r.Trigger, &r.Action, &r.Minutes, &scriptID, &platforms, &r.Enabled); err != nil {
		return r, err
	}
	r.ScriptID = scriptID.String
	_ = json.Unmarshal([]byte(platforms), &r.Platforms)
	if r.Platforms == nil {
		r.Platforms = []string{}
	}
	return r, nil
}

const responseCols = `id, policy_id, trigger, action, minutes, script_id, platforms, enabled`

// responsesOf liefert die Reaktionsregeln einer Richtlinie.
func (s *Store) responsesOf(ctx context.Context, policyID string) ([]model.PolicyResponse, error) {
	rows, err := s.db.QueryContext(ctx, s.rebind(`SELECT `+responseCols+` FROM policy_responses WHERE policy_id=? ORDER BY trigger, action`), policyID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.PolicyResponse{}
	for rows.Next() {
		r, err := scanResponse(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// GetResponse liest eine Regel.
func (s *Store) GetResponse(ctx context.Context, id string) (*model.PolicyResponse, error) {
	r, err := scanResponse(s.db.QueryRowContext(ctx, s.rebind(`SELECT `+responseCols+` FROM policy_responses WHERE id=?`), id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// SaveResponse legt an (ID leer) oder ändert.
func (s *Store) SaveResponse(ctx context.Context, r *model.PolicyResponse) error {
	platforms, _ := json.Marshal(r.Platforms)
	var scriptID any
	if r.ScriptID != "" {
		scriptID = r.ScriptID
	}
	if r.ID == "" {
		r.ID = NewID()
		_, err := s.db.ExecContext(ctx, s.rebind(`INSERT INTO policy_responses (`+responseCols+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`),
			r.ID, r.PolicyID, r.Trigger, r.Action, r.Minutes, scriptID, string(platforms), r.Enabled)
		return err
	}
	return s.affect(s.db.ExecContext(ctx, s.rebind(`UPDATE policy_responses SET trigger=?, action=?, minutes=?, script_id=?, platforms=?, enabled=? WHERE id=?`),
		r.Trigger, r.Action, r.Minutes, scriptID, string(platforms), r.Enabled, r.ID))
}

// DeleteResponse entfernt eine Regel.
func (s *Store) DeleteResponse(ctx context.Context, id string) error {
	return s.affect(s.db.ExecContext(ctx, s.rebind(`DELETE FROM policy_responses WHERE id=?`), id))
}

// ResponsesForDevice liefert die aktiven Reaktionsregeln aller dem Gerät zugewiesenen
// Richtlinien (Gerät, Standort, Client).
func (s *Store) ResponsesForDevice(ctx context.Context, deviceID string) ([]model.PolicyResponse, error) {
	rows, err := s.db.QueryContext(ctx, s.rebind(`
		SELECT DISTINCT r.id, r.policy_id, r.trigger, r.action, r.minutes, r.script_id, r.platforms, r.enabled
		FROM policy_responses r
		JOIN policy_assignments a ON a.policy_id = r.policy_id
		JOIN devices d ON d.id = ?
		LEFT JOIN sites s ON s.id = d.site_id
		WHERE r.enabled = TRUE AND (
			(a.target_type='device' AND a.target_id=d.id) OR
			(a.target_type='site' AND a.target_id=d.site_id) OR
			(a.target_type='client' AND a.target_id=s.client_id))
		ORDER BY r.trigger, r.action`), deviceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.PolicyResponse
	for rows.Next() {
		r, err := scanResponse(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// --- IP-Sperren ---

// InsertIPBlock legt eine Sperre (pending) an.
func (s *Store) InsertIPBlock(ctx context.Context, b *model.IPBlock) error {
	if b.ID == "" {
		b.ID = NewID()
	}
	if b.Status == "" {
		b.Status = "pending"
	}
	b.CreatedAt = time.Now().UTC()
	var until any
	if b.Until != nil {
		until = b.Until.UTC()
	}
	_, err := s.db.ExecContext(ctx, s.rebind(`
		INSERT INTO ip_blocks (id, device_id, ip, reason, incident_id, until_at, status, detail, command_id, created_by, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`),
		b.ID, b.DeviceID, b.IP, b.Reason, b.IncidentID, until, b.Status, b.Detail, b.CommandID, b.CreatedBy, b.CreatedAt)
	return err
}

// BlockIPOnDevices reiht eine Sperre von target auf allen angegebenen Geräten in einer
// einzigen Transaktion ein (Befehl + ip_blocks-Eintrag je Gerät). Geräte, auf denen target
// bereits wirksam oder eingereiht ist, werden übersprungen. Liefert die IDs der Geräte, die
// einen neuen Befehl bekommen haben (zum Wecken geparkter Polls).
func (s *Store) BlockIPOnDevices(ctx context.Context, deviceIDs []string, target string, minutes int, until *time.Time, reason, incidentID, by, label string) ([]string, error) {
	if len(deviceIDs) == 0 {
		return nil, nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback() //nolint:errcheck
	rows, err := tx.QueryContext(ctx, s.rebind(`SELECT DISTINCT device_id FROM ip_blocks
		WHERE ip=? AND status IN ('pending','active') AND (until_at IS NULL OR until_at > ?)`), target, time.Now().UTC())
	if err != nil {
		return nil, err
	}
	blocked := map[string]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		blocked[id] = true
	}
	rows.Close()
	var u any
	if until != nil {
		u = until.UTC()
	}
	now := time.Now().UTC()
	var queued []string
	for _, d := range deviceIDs {
		if blocked[d] {
			continue
		}
		cmdID, err := s.insertCommand(ctx, tx, d, "block_ip", label, map[string]any{"ip": target, "minutes": minutes})
		if err != nil {
			return nil, err
		}
		if _, err := tx.ExecContext(ctx, s.rebind(`
			INSERT INTO ip_blocks (id, device_id, ip, reason, incident_id, until_at, status, detail, command_id, created_by, created_at)
			VALUES (?, ?, ?, ?, ?, ?, 'pending', '', ?, ?, ?)`),
			NewID(), d, target, reason, incidentID, u, cmdID, by, now); err != nil {
			return nil, err
		}
		queued = append(queued, d)
	}
	return queued, tx.Commit()
}

// ExpireUndeliveredBlocks räumt Sperrbefehle auf, die ein Gerät noch nicht abgeholt hat,
// obwohl die Sperre schon abgelaufen wäre (Gerät war offline): Befehl erledigt, Sperre
// abgelaufen. Sonst arbeitet ein zurückkehrendes Gerät hunderte wirkungslose Sperren ab.
// Liefert die Zahl der bereinigten Befehle.
func (s *Store) ExpireUndeliveredBlocks(ctx context.Context, deviceID string) (int64, error) {
	now := time.Now().UTC()
	// Über die (wenigen) offenen Befehle des Geräts, nicht über seine Sperren – von denen
	// hat ein Gerät mit Flottensperre schnell tausende.
	res, err := s.db.ExecContext(ctx, s.rebind(`UPDATE commands SET status='done', result='Sperre vor Zustellung abgelaufen (Gerät war offline)'
		WHERE device_id=? AND status='pending' AND type='block_ip'
		  AND EXISTS (SELECT 1 FROM ip_blocks b WHERE b.command_id = commands.id AND b.until_at IS NOT NULL AND b.until_at < ?)`),
		deviceID, now)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	if n > 0 {
		_, err = s.db.ExecContext(ctx, s.rebind(`UPDATE ip_blocks SET status='expired', detail='vor Zustellung abgelaufen (Gerät war offline)'
			WHERE status='pending' AND device_id=? AND until_at IS NOT NULL AND until_at < ?`), deviceID, now)
	}
	return n, err
}

// RenewIPBlock setzt eine bestehende Sperre neu an (neue Dauer/Begründung, neuer Befehl,
// Status pending) – statt eines Duplikats, wenn dieselbe IP erneut gesperrt wird.
func (s *Store) RenewIPBlock(ctx context.Context, id string, until *time.Time, reason, commandID, by string) error {
	var u any
	if until != nil {
		u = until.UTC()
	}
	_, err := s.db.ExecContext(ctx, s.rebind(`UPDATE ip_blocks SET until_at=?, reason=?, command_id=?, created_by=?, status='pending', detail='', created_at=? WHERE id=?`),
		u, reason, commandID, by, time.Now().UTC(), id)
	return err
}

// ActiveIPBlock liefert eine pending/aktive Sperre für Gerät+IP (nil = keine).
func (s *Store) ActiveIPBlock(ctx context.Context, deviceID, ip string) (*model.IPBlock, error) {
	rows, err := s.db.QueryContext(ctx, s.rebind(`SELECT `+ipBlockCols+` FROM ip_blocks b JOIN devices d ON d.id=b.device_id
		WHERE b.device_id=? AND b.ip=? AND b.status IN ('pending','active') AND (b.until_at IS NULL OR b.until_at > ?)
		ORDER BY b.created_at DESC LIMIT 1`), deviceID, ip, time.Now().UTC())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, nil
	}
	b, err := scanIPBlock(rows)
	if err != nil {
		return nil, err
	}
	return &b, nil
}

const ipBlockCols = `b.id, b.device_id, d.hostname, b.ip, b.reason, b.incident_id, b.until_at, b.status, b.detail, b.command_id, b.created_by, b.created_at`

func scanIPBlock(rows *sql.Rows) (model.IPBlock, error) {
	var b model.IPBlock
	var until sql.NullTime
	err := rows.Scan(&b.ID, &b.DeviceID, &b.Hostname, &b.IP, &b.Reason, &b.IncidentID, &until, &b.Status, &b.Detail, &b.CommandID, &b.CreatedBy, &b.CreatedAt)
	if until.Valid {
		t := until.Time
		b.Until = &t
	}
	if b.Status == "active" && b.Until != nil && time.Now().After(*b.Until) {
		b.Status = "expired"
	}
	b.Country = geoip.Country(b.IP)
	b.Threat = threat.Lookup(b.IP)
	return b, err
}

// IPBlockFilter grenzt die Sperrenliste ein.
type IPBlockFilter struct {
	Allowed  map[string]bool
	DeviceID string
	Query    string // IP/Netz, Hostname, Grund oder Ersteller (Teiltreffer)
	Status   string // active (wirksam oder eingereiht) | expired | released | failed | "" = alle
	Limit    int
}

// IPBlocks listet Sperren (Scope, optional je Gerät), neueste zuerst.
func (s *Store) IPBlocks(ctx context.Context, allowed map[string]bool, deviceID string, limit int) ([]model.IPBlock, error) {
	return s.IPBlocksFiltered(ctx, IPBlockFilter{Allowed: allowed, DeviceID: deviceID, Limit: limit})
}

// IPBlocksFiltered listet Sperren nach Filter, neueste zuerst.
func (s *Store) IPBlocksFiltered(ctx context.Context, f IPBlockFilter) ([]model.IPBlock, error) {
	scope, args, empty := scopeWhere(f.Allowed)
	if empty {
		return []model.IPBlock{}, nil
	}
	q := `SELECT ` + ipBlockCols + ` FROM ip_blocks b JOIN devices d ON d.id=b.device_id WHERE d.revoked = FALSE` + scope
	if f.DeviceID != "" {
		q += ` AND b.device_id = ?`
		args = append(args, f.DeviceID)
	}
	qs := strings.TrimSpace(f.Query)
	var qIP net.IP // Suche nach einer IP: auch Netzsperren finden, die sie enthalten
	if qs != "" {
		like := "%" + qs + "%"
		if qIP = net.ParseIP(qs); qIP != nil {
			q += ` AND (b.ip LIKE ? OR d.hostname LIKE ? OR b.reason LIKE ? OR b.created_by LIKE ? OR b.ip LIKE '%/%')`
		} else {
			q += ` AND (b.ip LIKE ? OR d.hostname LIKE ? OR b.reason LIKE ? OR b.created_by LIKE ?)`
		}
		args = append(args, like, like, like, like)
	}
	now := time.Now().UTC()
	switch f.Status {
	case "active":
		q += ` AND b.status IN ('active','pending') AND (b.until_at IS NULL OR b.until_at > ?)`
		args = append(args, now)
	case "expired":
		q += ` AND ((b.status = 'active' AND b.until_at IS NOT NULL AND b.until_at <= ?) OR b.status = 'expired')`
		args = append(args, now)
	case "":
	default:
		q += ` AND b.status = ?`
		args = append(args, f.Status)
	}
	limit := f.Limit
	if limit <= 0 || limit > 5000 {
		limit = 500
	}
	q += ` ORDER BY b.created_at DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, s.rebind(q), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.IPBlock{}
	for rows.Next() {
		b, err := scanIPBlock(rows)
		if err != nil {
			return nil, err
		}
		if qIP != nil && !blockMatchesIP(b, qs, qIP) {
			continue
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// blockMatchesIP: Texttreffer wie in SQL oder eine Netzsperre, die die gesuchte IP enthält.
func blockMatchesIP(b model.IPBlock, qs string, ip net.IP) bool {
	l := strings.ToLower(qs)
	for _, f := range []string{b.IP, b.Hostname, b.Reason, b.CreatedBy} {
		if strings.Contains(strings.ToLower(f), l) {
			return true
		}
	}
	if _, n, err := net.ParseCIDR(b.IP); err == nil && n.Contains(ip) {
		return true
	}
	return false
}

// GetIPBlock liest eine Sperre.
func (s *Store) GetIPBlock(ctx context.Context, id string) (*model.IPBlock, error) {
	rows, err := s.db.QueryContext(ctx, s.rebind(`SELECT `+ipBlockCols+` FROM ip_blocks b JOIN devices d ON d.id=b.device_id WHERE b.id=?`), id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, ErrNotFound
	}
	b, err := scanIPBlock(rows)
	return &b, err
}

// SetIPBlockStatus setzt Status/Detail einer Sperre.
func (s *Store) SetIPBlockStatus(ctx context.Context, id, status, detail string) error {
	_, err := s.db.ExecContext(ctx, s.rebind(`UPDATE ip_blocks SET status=?, detail=? WHERE id=?`), status, truncate(detail, 500), id)
	return err
}

// IPBlockByCommand findet die Sperre zu einem Befehl.
func (s *Store) IPBlockByCommand(ctx context.Context, commandID string) (*model.IPBlock, error) {
	rows, err := s.db.QueryContext(ctx, s.rebind(`SELECT `+ipBlockCols+` FROM ip_blocks b JOIN devices d ON d.id=b.device_id WHERE b.command_id=?`), commandID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, nil
	}
	b, err := scanIPBlock(rows)
	return &b, err
}

// IncidentByResponseCommand findet den Vorfall zu einem Reaktionsbefehl.
func (s *Store) IncidentByResponseCommand(ctx context.Context, commandID string) (*model.SecurityIncident, error) {
	rows, err := s.db.QueryContext(ctx, s.rebind(`SELECT `+incidentCols+` FROM security_incidents i JOIN devices d ON d.id = i.device_id
		LEFT JOIN sites st ON st.id = d.site_id LEFT JOIN clients c ON c.id = st.client_id WHERE i.response_command_id = ?`), commandID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, nil
	}
	i, err := scanIncident(rows)
	return &i, err
}

// RecentResponse meldet, ob für Gerät+Reaktion (z. B. "block_ip 1.2.3.4") innerhalb des
// Cooldowns schon reagiert wurde.
func (s *Store) RecentResponse(ctx context.Context, deviceID, response string, since time.Time) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, s.rebind(`SELECT COUNT(*) FROM security_incidents WHERE device_id=? AND response=? AND ts >= ?`), deviceID, response, since.UTC()).Scan(&n)
	return n > 0, err
}
