package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/boonkerz/roster/internal/server/model"
	"github.com/boonkerz/roster/internal/shared"
)

// CreateCommand legt einen Ad-hoc-Befehl für ein Gerät an (Status pending).
// Das Label wird ins Payload eingebettet; result bleibt dem Ergebnis vorbehalten.
func (s *Store) CreateCommand(ctx context.Context, deviceID, typ, label string, payload map[string]any) (string, error) {
	return s.insertCommand(ctx, s.db, deviceID, typ, label, payload)
}

// cmdExecer ist *sql.DB oder *sql.Tx.
type cmdExecer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// insertCommand legt einen offenen Befehl an (auch innerhalb einer Transaktion).
func (s *Store) insertCommand(ctx context.Context, ex cmdExecer, deviceID, typ, label string, payload map[string]any) (string, error) {
	id := newID()
	if payload == nil {
		payload = map[string]any{}
	}
	payload["label"] = label
	data, _ := json.Marshal(payload)
	_, err := ex.ExecContext(ctx, s.rebind(`
		INSERT INTO commands (id, device_id, type, payload, status, created_at, result)
		VALUES (?, ?, ?, ?, 'pending', ?, '')`),
		id, deviceID, typ, string(data), time.Now().UTC())
	return id, err
}

// PendingCommands liefert offene Befehle eines Geräts und markiert sie als gesendet.
func (s *Store) PendingCommands(ctx context.Context, deviceID string) ([]shared.Command, error) {
	rows, err := s.db.QueryContext(ctx, s.rebind(`
		SELECT id, type, payload FROM commands WHERE device_id=? AND status='pending'`), deviceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []shared.Command
	var ids []string
	for rows.Next() {
		var c shared.Command
		var payload string
		if err := rows.Scan(&c.ID, &c.Type, &payload); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(payload), &c.Payload)
		out = append(out, c)
		ids = append(ids, c.ID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	for _, id := range ids {
		_, _ = s.db.ExecContext(ctx, s.rebind(`UPDATE commands SET status='sent', sent_at=? WHERE id=?`), now, id)
	}
	return out, nil
}

// SaveCommandResults speichert die Ergebnisse ausgeführter Befehle.
func (s *Store) SaveCommandResults(ctx context.Context, results []shared.CommandResult) error {
	for _, r := range results {
		res, _ := json.Marshal(map[string]any{"exit_code": r.ExitCode, "output": r.Output})
		if _, err := s.db.ExecContext(ctx, s.rebind(`
			UPDATE commands SET status='done', result=?, sent_at=? WHERE id=?`),
			string(res), r.RanAt.UTC(), r.CommandID); err != nil {
			return err
		}
	}
	return nil
}

// UpdateCommandProgress speichert einen Zwischenstand (Output) eines noch
// laufenden Befehls – gleiche Hülle wie das Endergebnis, damit CommandByID es
// einheitlich liest. Nur für Befehle des angegebenen Geräts und solange nicht
// „done" (das Endergebnis darf nicht überschrieben werden).
func (s *Store) UpdateCommandProgress(ctx context.Context, deviceID, commandID, output string) error {
	res, _ := json.Marshal(map[string]any{"exit_code": 0, "output": output})
	_, err := s.db.ExecContext(ctx, s.rebind(`
		UPDATE commands SET result=? WHERE id=? AND device_id=? AND status<>'done'`),
		string(res), commandID, deviceID)
	return err
}

// CommandByID liefert einen einzelnen Befehl inkl. Ergebnis (für Polling).
func (s *Store) CommandByID(ctx context.Context, id string) (model.Command, error) {
	var c model.Command
	var payload, result string
	var sentAt sql.NullTime
	err := s.db.QueryRowContext(ctx, s.rebind(`
		SELECT id, device_id, type, payload, status, created_at, sent_at, result
		FROM commands WHERE id=?`), id).
		Scan(&c.ID, &c.DeviceID, &c.Type, &payload, &c.Status, &c.CreatedAt, &sentAt, &result)
	if err == sql.ErrNoRows {
		return c, ErrNotFound
	}
	if err != nil {
		return c, err
	}
	var pl struct {
		Label string `json:"label"`
	}
	_ = json.Unmarshal([]byte(payload), &pl)
	c.Label = pl.Label
	// Ergebnis (Teil- oder Endstand) liefern, sobald vorhanden – für Live-Polling.
	if result != "" {
		var rr struct {
			ExitCode int    `json:"exit_code"`
			Output   string `json:"output"`
		}
		if json.Unmarshal([]byte(result), &rr) == nil {
			c.ExitCode = rr.ExitCode
			c.Output = rr.Output
		}
	}
	if c.Status == "done" && sentAt.Valid {
		t := sentAt.Time
		c.RanAt = &t
	}
	return c, nil
}

// CommandsFor liefert die letzten Befehle eines Geräts (für die Anzeige).
func (s *Store) CommandsFor(ctx context.Context, deviceID string, limit int) ([]model.Command, error) {
	rows, err := s.db.QueryContext(ctx, s.rebind(`
		SELECT id, type, payload, status, created_at, sent_at, result
		FROM commands WHERE device_id=? ORDER BY created_at DESC LIMIT ?`), deviceID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Command
	for rows.Next() {
		var c model.Command
		var payload, result string
		var sentAt sql.NullTime
		if err := rows.Scan(&c.ID, &c.Type, &payload, &c.Status, &c.CreatedAt, &sentAt, &result); err != nil {
			return nil, err
		}
		c.DeviceID = deviceID
		var pl struct {
			Label string `json:"label"`
		}
		_ = json.Unmarshal([]byte(payload), &pl)
		c.Label = pl.Label
		if c.Status == "done" {
			var rr struct {
				ExitCode int    `json:"exit_code"`
				Output   string `json:"output"`
			}
			if json.Unmarshal([]byte(result), &rr) == nil {
				c.ExitCode = rr.ExitCode
				c.Output = rr.Output
			}
			if sentAt.Valid {
				t := sentAt.Time
				c.RanAt = &t
			}
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
