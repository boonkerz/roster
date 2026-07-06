package store

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/thomaspeterson/pc-inventory/internal/server/model"
	"github.com/thomaspeterson/pc-inventory/internal/shared"
)

// CommandMeta liefert Typ und Payload eines Befehls (für die Ergebnis-Nachbearbeitung).
func (s *Store) CommandMeta(ctx context.Context, id string) (string, map[string]any, error) {
	var typ, payload string
	err := s.db.QueryRowContext(ctx, s.rebind(`SELECT type, payload FROM commands WHERE id=?`), id).Scan(&typ, &payload)
	if err != nil {
		return "", nil, err
	}
	var pl map[string]any
	_ = json.Unmarshal([]byte(payload), &pl)
	return typ, pl, nil
}

// UpsertNetworkAssets fügt gefundene Hosts einer Site hinzu (Dedup über site_id+ip).
func (s *Store) UpsertNetworkAssets(ctx context.Context, siteID string, hosts []shared.NetworkHost) (int, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback() //nolint:errcheck
	now := time.Now().UTC()
	n := 0
	for _, h := range hosts {
		var ports []string
		for _, p := range h.Ports {
			ports = append(ports, strconv.Itoa(p))
		}
		if _, err := tx.ExecContext(ctx, s.rebind(`
			INSERT INTO network_assets (id, site_id, ip, mac, hostname, ports, note, first_seen, last_seen)
			VALUES (?, ?, ?, ?, ?, ?, '', ?, ?)
			ON CONFLICT(site_id, ip) DO UPDATE SET
				mac=excluded.mac, hostname=excluded.hostname, ports=excluded.ports, last_seen=excluded.last_seen`),
			newID(), siteID, h.IP, h.MAC, h.Hostname, strings.Join(ports, ","), now, now); err != nil {
			return n, err
		}
		n++
	}
	return n, tx.Commit()
}

// NetworkAssetsForSite liefert die Assets einer Site inkl. „verwaltet"-Flag (ob IP/MAC
// zu einem enrollten Gerät passt).
func (s *Store) NetworkAssetsForSite(ctx context.Context, siteID string) ([]model.NetworkAsset, error) {
	rows, err := s.db.QueryContext(ctx, s.rebind(`
		SELECT a.id, a.site_id, a.ip, a.mac, a.hostname, a.ports, a.note, a.first_seen, a.last_seen,
			EXISTS(SELECT 1 FROM interfaces i JOIN devices d ON d.id=i.device_id
				WHERE d.revoked=0 AND ((a.mac<>'' AND i.mac=a.mac) OR (a.ip<>'' AND i.ipv4 LIKE '%'||a.ip||'%')))
		FROM network_assets a WHERE a.site_id=? ORDER BY a.ip`), siteID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.NetworkAsset
	for rows.Next() {
		var a model.NetworkAsset
		if err := rows.Scan(&a.ID, &a.SiteID, &a.IP, &a.MAC, &a.Hostname, &a.Ports, &a.Note, &a.FirstSeen, &a.LastSeen, &a.Managed); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// SetNetworkAssetNote setzt die Notiz eines Assets.
func (s *Store) SetNetworkAssetNote(ctx context.Context, id, note string) error {
	return s.affect(s.db.ExecContext(ctx, s.rebind(`UPDATE network_assets SET note=? WHERE id=?`), note, id))
}

// DeleteNetworkAsset entfernt ein Asset.
func (s *Store) DeleteNetworkAsset(ctx context.Context, id string) error {
	return s.affect(s.db.ExecContext(ctx, s.rebind(`DELETE FROM network_assets WHERE id=?`), id))
}

// kindFromPorts rät den Gerätetyp aus den (CSV-)Ports (für nicht verwaltete Geräte).
func kindFromPorts(csv string) string {
	has := func(ps ...string) bool {
		set := strings.Split(csv, ",")
		for _, p := range ps {
			for _, x := range set {
				if strings.TrimSpace(x) == p {
					return true
				}
			}
		}
		return false
	}
	switch {
	case has("9100", "631", "515"):
		return "Drucker"
	case has("3389", "445", "139", "135"):
		return "Windows"
	case has("5900"):
		return "VNC"
	case has("22"):
		return "Linux/SSH"
	case has("80", "443", "8080"):
		return "Web/Gerät"
	}
	return ""
}

// AdoptNetworkAsset übernimmt ein Asset als nicht verwaltetes Gerät (ohne Agent) und
// entfernt es aus der Asset-Liste. Liefert die neue Geräte-ID.
func (s *Store) AdoptNetworkAsset(ctx context.Context, assetID string) (string, error) {
	var siteID, ip, mac, hostname, ports string
	err := s.db.QueryRowContext(ctx, s.rebind(
		`SELECT site_id, ip, mac, hostname, ports FROM network_assets WHERE id=?`), assetID).
		Scan(&siteID, &ip, &mac, &hostname, &ports)
	if err != nil {
		return "", err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback() //nolint:errcheck
	devID := newID()
	now := time.Now().UTC()
	hn := hostname
	if hn == "" {
		hn = ip
	}
	if _, err := tx.ExecContext(ctx, s.rebind(`
		INSERT INTO devices (id, hostname, os, os_version, first_seen, enrolled_at, agent_token_hash, revoked, site_id, managed)
		VALUES (?, ?, ?, '', ?, ?, '', 0, ?, 0)`),
		devID, hn, kindFromPorts(ports), now, now, siteID); err != nil {
		return "", err
	}
	if _, err := tx.ExecContext(ctx, s.rebind(`
		INSERT INTO interfaces (device_id, name, mac, ipv4, ipv6) VALUES (?, '', ?, ?, '')`),
		devID, mac, ip); err != nil {
		return "", err
	}
	if _, err := tx.ExecContext(ctx, s.rebind(`DELETE FROM network_assets WHERE id=?`), assetID); err != nil {
		return "", err
	}
	return devID, tx.Commit()
}

// AdoptAllForSite übernimmt alle noch nicht verwalteten Assets einer Site als Geräte.
func (s *Store) AdoptAllForSite(ctx context.Context, siteID string) (int, error) {
	assets, err := s.NetworkAssetsForSite(ctx, siteID)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, a := range assets {
		if a.Managed {
			continue
		}
		if _, err := s.AdoptNetworkAsset(ctx, a.ID); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}
