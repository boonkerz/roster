package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/boonkerz/roster/internal/server/model"
)

// GetAlertConfig liefert die Alert-Konfiguration (Defaults, falls noch keine gespeichert).
func (s *Store) GetAlertConfig(ctx context.Context) (model.AlertConfig, error) {
	var c model.AlertConfig
	err := s.db.QueryRowContext(ctx, `
		SELECT enabled, alert_software, alert_after_remediation, smtp_host, smtp_port, smtp_user, smtp_pass, smtp_from, smtp_tls, recipient, webhook_url
		FROM alert_config WHERE id=1`).
		Scan(&c.Enabled, &c.AlertSoftware, &c.AlertAfterRemediation, &c.SMTPHost, &c.SMTPPort, &c.SMTPUser, &c.SMTPPass, &c.SMTPFrom, &c.SMTPTLS, &c.Recipient, &c.WebhookURL)
	if errors.Is(err, sql.ErrNoRows) {
		return model.AlertConfig{SMTPPort: 587, SMTPTLS: true, AlertAfterRemediation: true}, nil
	}
	return c, err
}

// SaveAlertConfig speichert die Alert-Konfiguration (Upsert auf id=1).
func (s *Store) SaveAlertConfig(ctx context.Context, c model.AlertConfig) error {
	res, err := s.db.ExecContext(ctx, s.rebind(`
		UPDATE alert_config SET enabled=?, alert_software=?, alert_after_remediation=?, smtp_host=?, smtp_port=?, smtp_user=?, smtp_pass=?,
			smtp_from=?, smtp_tls=?, recipient=?, webhook_url=? WHERE id=1`),
		c.Enabled, c.AlertSoftware, c.AlertAfterRemediation, c.SMTPHost, c.SMTPPort, c.SMTPUser, c.SMTPPass, c.SMTPFrom, c.SMTPTLS, c.Recipient, c.WebhookURL)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		_, err = s.db.ExecContext(ctx, s.rebind(`
			INSERT INTO alert_config (id, enabled, alert_software, alert_after_remediation, smtp_host, smtp_port, smtp_user, smtp_pass, smtp_from, smtp_tls, recipient, webhook_url)
			VALUES (1, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`),
			c.Enabled, c.AlertSoftware, c.AlertAfterRemediation, c.SMTPHost, c.SMTPPort, c.SMTPUser, c.SMTPPass, c.SMTPFrom, c.SMTPTLS, c.Recipient, c.WebhookURL)
	}
	return err
}

// AlertChannels liefert alle konfigurierten Benachrichtigungskanäle inkl. Geltungsbereich.
func (s *Store) AlertChannels(ctx context.Context) ([]model.AlertChannel, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, type, name, enabled, config, min_severity FROM alert_channels ORDER BY name`)
	if err != nil {
		return nil, err
	}
	var out []model.AlertChannel
	for rows.Next() {
		ch, cfg := model.AlertChannel{}, ""
		if err := rows.Scan(&ch.ID, &ch.Type, &ch.Name, &ch.Enabled, &cfg, &ch.MinSeverity); err != nil {
			rows.Close()
			return nil, err
		}
		ch.Config = map[string]string{}
		_ = json.Unmarshal([]byte(cfg), &ch.Config)
		out = append(out, ch)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// Zuweisungen erst NACH dem Schließen der Channel-Rows laden – sonst belegt die
	// offene Schleife die einzige SQLite-Verbindung und die verschachtelte Query
	// blockiert (SetMaxOpenConns(1)).
	for i := range out {
		if out[i].Assignments, err = s.channelAssignments(ctx, out[i].ID); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// AlertChannel liefert einen einzelnen Kanal inkl. Geltungsbereich.
func (s *Store) AlertChannel(ctx context.Context, id string) (model.AlertChannel, error) {
	var ch model.AlertChannel
	var cfg string
	err := s.db.QueryRowContext(ctx, s.rebind(`SELECT id, type, name, enabled, config, min_severity FROM alert_channels WHERE id=?`), id).
		Scan(&ch.ID, &ch.Type, &ch.Name, &ch.Enabled, &cfg, &ch.MinSeverity)
	if errors.Is(err, sql.ErrNoRows) {
		return ch, ErrNotFound
	}
	if err != nil {
		return ch, err
	}
	ch.Config = map[string]string{}
	_ = json.Unmarshal([]byte(cfg), &ch.Config)
	if ch.Assignments, err = s.channelAssignments(ctx, ch.ID); err != nil {
		return ch, err
	}
	return ch, nil
}

func (s *Store) channelAssignments(ctx context.Context, channelID string) ([]model.ChannelScope, error) {
	rows, err := s.db.QueryContext(ctx, s.rebind(`
		SELECT target_type, target_id FROM alert_channel_assignments WHERE channel_id=?`), channelID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.ChannelScope{}
	for rows.Next() {
		var a model.ChannelScope
		if err := rows.Scan(&a.TargetType, &a.TargetID); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// SaveAlertChannel legt einen Kanal an oder aktualisiert ihn (Upsert, portabel) und
// ersetzt seinen Geltungsbereich.
func (s *Store) SaveAlertChannel(ctx context.Context, ch model.AlertChannel) error {
	cfg, err := json.Marshal(ch.Config)
	if err != nil {
		return err
	}
	sev := ch.MinSeverity
	if sev != "critical" {
		sev = "warning"
	}
	res, err := s.db.ExecContext(ctx, s.rebind(`
		UPDATE alert_channels SET type=?, name=?, enabled=?, config=?, min_severity=? WHERE id=?`),
		ch.Type, ch.Name, ch.Enabled, string(cfg), sev, ch.ID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		if _, err = s.db.ExecContext(ctx, s.rebind(`
			INSERT INTO alert_channels (id, type, name, enabled, config, min_severity) VALUES (?, ?, ?, ?, ?, ?)`),
			ch.ID, ch.Type, ch.Name, ch.Enabled, string(cfg), sev); err != nil {
			return err
		}
	}
	// Geltungsbereich vollständig ersetzen.
	if _, err = s.db.ExecContext(ctx, s.rebind(`DELETE FROM alert_channel_assignments WHERE channel_id=?`), ch.ID); err != nil {
		return err
	}
	for _, a := range ch.Assignments {
		if a.TargetType == "" || a.TargetID == "" {
			continue
		}
		if _, err = s.db.ExecContext(ctx, s.rebind(`
			INSERT INTO alert_channel_assignments (id, channel_id, target_type, target_id) VALUES (?, ?, ?, ?)`),
			newID(), ch.ID, a.TargetType, a.TargetID); err != nil {
			return err
		}
	}
	return nil
}

// ChannelsForDevice liefert die aktiven Kanäle, die für ein Gerät gelten: Kanäle
// ohne Geltungsbereich (global) sowie solche, deren Zuweisung auf Gerät/Site/Client passt.
func (s *Store) ChannelsForDevice(ctx context.Context, deviceID string) ([]model.AlertChannel, error) {
	var siteID, clientID sql.NullString
	if err := s.db.QueryRowContext(ctx, s.rebind(`
		SELECT d.site_id, s.client_id FROM devices d LEFT JOIN sites s ON s.id=d.site_id WHERE d.id=?`),
		deviceID).Scan(&siteID, &clientID); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, s.rebind(`
		SELECT c.id, c.type, c.name, c.enabled, c.config, c.min_severity
		FROM alert_channels c
		WHERE c.enabled=1 AND (
			NOT EXISTS (SELECT 1 FROM alert_channel_assignments a WHERE a.channel_id=c.id)
			OR EXISTS (SELECT 1 FROM alert_channel_assignments a WHERE a.channel_id=c.id AND (
				(a.target_type='device' AND a.target_id=?) OR
				(a.target_type='site'   AND a.target_id=?) OR
				(a.target_type='client' AND a.target_id=?)))
		)`), deviceID, siteID.String, clientID.String)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.AlertChannel
	for rows.Next() {
		ch, cfg := model.AlertChannel{}, ""
		if err := rows.Scan(&ch.ID, &ch.Type, &ch.Name, &ch.Enabled, &cfg, &ch.MinSeverity); err != nil {
			return nil, err
		}
		ch.Config = map[string]string{}
		_ = json.Unmarshal([]byte(cfg), &ch.Config)
		out = append(out, ch)
	}
	return out, rows.Err()
}

// DeleteAlertChannel entfernt einen Kanal.
func (s *Store) DeleteAlertChannel(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, s.rebind(`DELETE FROM alert_channels WHERE id=?`), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}
