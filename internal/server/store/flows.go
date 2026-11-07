package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/boonkerz/roster/internal/server/model"
	"github.com/boonkerz/roster/internal/shared"
)

// Abläufe (Flow-Designer): Definitionen, Läufe und Ergebnisse je Gerät.

func scanFlow(row scanner) (model.Flow, error) {
	var f model.Flow
	var graph, params string
	if err := row.Scan(&f.ID, &f.Name, &f.Description, &graph, &params, &f.CreatedBy, &f.CreatedAt, &f.UpdatedAt); err != nil {
		return f, err
	}
	_ = json.Unmarshal([]byte(graph), &f.Graph)
	_ = json.Unmarshal([]byte(params), &f.Params)
	if f.Graph.Nodes == nil {
		f.Graph.Nodes = []shared.FlowNode{}
	}
	if f.Graph.Edges == nil {
		f.Graph.Edges = []shared.FlowEdge{}
	}
	if f.Params == nil {
		f.Params = []shared.FlowParam{}
	}
	return f, nil
}

const flowCols = `id, name, description, graph, params, created_by, created_at, updated_at`

// ListFlows liefert alle Abläufe (nach Name).
func (s *Store) ListFlows(ctx context.Context) ([]model.Flow, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+flowCols+` FROM flows ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.Flow{}
	for rows.Next() {
		f, err := scanFlow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// GetFlow liefert einen Ablauf.
func (s *Store) GetFlow(ctx context.Context, id string) (*model.Flow, error) {
	f, err := scanFlow(s.db.QueryRowContext(ctx, s.rebind(`SELECT `+flowCols+` FROM flows WHERE id=?`), id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &f, nil
}

// SaveFlow legt einen Ablauf an (ID leer) oder aktualisiert ihn.
func (s *Store) SaveFlow(ctx context.Context, f *model.Flow) error {
	g, _ := json.Marshal(f.Graph)
	p, _ := json.Marshal(f.Params)
	now := time.Now().UTC()
	if f.ID == "" {
		f.ID = NewID()
		f.CreatedAt, f.UpdatedAt = now, now
		_, err := s.db.ExecContext(ctx, s.rebind(`INSERT INTO flows (`+flowCols+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`),
			f.ID, f.Name, f.Description, string(g), string(p), f.CreatedBy, now, now)
		return err
	}
	f.UpdatedAt = now
	res, err := s.db.ExecContext(ctx, s.rebind(`UPDATE flows SET name=?, description=?, graph=?, params=?, updated_at=? WHERE id=?`),
		f.Name, f.Description, string(g), string(p), now, f.ID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteFlow löscht einen Ablauf (Läufe bleiben als Verlauf erhalten).
func (s *Store) DeleteFlow(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, s.rebind(`DELETE FROM flows WHERE id=?`), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// CreateFlowRun legt einen Lauf mit Schnappschuss des Graphen an.
func (s *Store) CreateFlowRun(ctx context.Context, run *model.FlowRun) error {
	run.ID = NewID()
	run.CreatedAt = time.Now().UTC()
	g, _ := json.Marshal(run.Graph)
	p, _ := json.Marshal(run.Params)
	_, err := s.db.ExecContext(ctx, s.rebind(`INSERT INTO flow_runs (id, flow_id, flow_name, graph, params, test_device_id, started_by, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`), run.ID, run.FlowID, run.FlowName, string(g), string(p), run.TestDeviceID, run.StartedBy, run.CreatedAt)
	return err
}

// AddFlowRunDevice vermerkt den Befehl eines Geräts für eine Stufe (ersetzt einen alten Eintrag).
func (s *Store) AddFlowRunDevice(ctx context.Context, runID, deviceID, stage, commandID string) error {
	now := time.Now().UTC()
	if _, err := s.db.ExecContext(ctx, s.rebind(`DELETE FROM flow_run_devices WHERE run_id=? AND device_id=? AND stage=?`), runID, deviceID, stage); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, s.rebind(`INSERT INTO flow_run_devices (run_id, device_id, stage, command_id, status, result, updated_at)
		VALUES (?, ?, ?, ?, 'queued', '', ?)`), runID, deviceID, stage, commandID, now)
	return err
}

// CompleteFlowCommand übernimmt das Agent-Ergebnis eines run_flow-Befehls. Liefert false,
// wenn der Befehl zu keinem Lauf gehört.
func (s *Store) CompleteFlowCommand(ctx context.Context, commandID, status, result string) (bool, error) {
	res, err := s.db.ExecContext(ctx, s.rebind(`UPDATE flow_run_devices SET status=?, result=?, updated_at=? WHERE command_id=?`),
		status, result, time.Now().UTC(), commandID)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// GetFlowRun liefert einen Lauf mit allen Geräte-Ergebnissen.
func (s *Store) GetFlowRun(ctx context.Context, id string) (*model.FlowRun, error) {
	var run model.FlowRun
	var g, p string
	err := s.db.QueryRowContext(ctx, s.rebind(`SELECT id, flow_id, flow_name, graph, params, test_device_id, started_by, created_at FROM flow_runs WHERE id=?`), id).
		Scan(&run.ID, &run.FlowID, &run.FlowName, &g, &p, &run.TestDeviceID, &run.StartedBy, &run.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal([]byte(g), &run.Graph)
	_ = json.Unmarshal([]byte(p), &run.Params)
	rows, err := s.db.QueryContext(ctx, s.rebind(`SELECT x.device_id, COALESCE(d.hostname, ''), x.stage, x.command_id, x.status, x.result, x.updated_at
		FROM flow_run_devices x LEFT JOIN devices d ON d.id=x.device_id WHERE x.run_id=?
		ORDER BY CASE x.stage WHEN 'preview' THEN 0 WHEN 'test' THEN 1 ELSE 2 END, d.hostname`), id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	run.Devices = []model.FlowRunDevice{}
	for rows.Next() {
		var d model.FlowRunDevice
		var result string
		if err := rows.Scan(&d.DeviceID, &d.Hostname, &d.Stage, &d.CommandID, &d.Status, &result, &d.UpdatedAt); err != nil {
			return nil, err
		}
		if result != "" {
			var fr shared.FlowResult
			if json.Unmarshal([]byte(result), &fr) == nil {
				d.Result = &fr
			}
		}
		run.Devices = append(run.Devices, d)
	}
	return &run, rows.Err()
}

// ListFlowRuns liefert die letzten Läufe eines Ablaufs (ohne Ergebnisse, mit Zählern).
func (s *Store) ListFlowRuns(ctx context.Context, flowID string, limit int) ([]model.FlowRunSummary, error) {
	rows, err := s.db.QueryContext(ctx, s.rebind(`SELECT r.id, r.flow_name, r.started_by, r.created_at,
		(SELECT COUNT(*) FROM flow_run_devices x WHERE x.run_id=r.id AND x.stage<>'preview'),
		(SELECT COUNT(*) FROM flow_run_devices x WHERE x.run_id=r.id AND x.stage<>'preview' AND x.status='ok'),
		(SELECT COUNT(*) FROM flow_run_devices x WHERE x.run_id=r.id AND x.stage<>'preview' AND x.status IN ('fail','rolled_back')),
		(SELECT COUNT(*) FROM flow_run_devices x WHERE x.run_id=r.id AND x.stage<>'preview' AND x.status='queued')
		FROM flow_runs r WHERE r.flow_id=? ORDER BY r.created_at DESC LIMIT ?`), flowID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.FlowRunSummary{}
	for rows.Next() {
		var r model.FlowRunSummary
		if err := rows.Scan(&r.ID, &r.FlowName, &r.StartedBy, &r.CreatedAt, &r.Devices, &r.OK, &r.Failed, &r.Pending); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
