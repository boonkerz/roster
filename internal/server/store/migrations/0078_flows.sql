-- +goose Up
-- Abläufe (Flow-Designer): gespeicherte Graphen, Läufe (Vorschau → Test → Rollout) und das
-- Ergebnis je Gerät (JSON des Agents).
CREATE TABLE flows (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    graph       TEXT NOT NULL DEFAULT '{}',
    params      TEXT NOT NULL DEFAULT '[]',
    created_by  TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMP NOT NULL,
    updated_at  TIMESTAMP NOT NULL
);

CREATE TABLE flow_runs (
    id             TEXT PRIMARY KEY,
    flow_id        TEXT NOT NULL,
    flow_name      TEXT NOT NULL DEFAULT '',
    graph          TEXT NOT NULL DEFAULT '{}',  -- Schnappschuss beim Start
    params         TEXT NOT NULL DEFAULT '{}',
    test_device_id TEXT NOT NULL DEFAULT '',
    started_by     TEXT NOT NULL DEFAULT '',
    created_at     TIMESTAMP NOT NULL
);
CREATE INDEX idx_flow_runs_flow ON flow_runs(flow_id, created_at);

CREATE TABLE flow_run_devices (
    run_id     TEXT NOT NULL REFERENCES flow_runs(id) ON DELETE CASCADE,
    device_id  TEXT NOT NULL,
    stage      TEXT NOT NULL,                  -- preview | test | rollout
    command_id TEXT NOT NULL DEFAULT '',
    status     TEXT NOT NULL DEFAULT 'queued', -- queued | ok | fail | rolled_back | dry | nothing
    result     TEXT NOT NULL DEFAULT '',
    updated_at TIMESTAMP NOT NULL,
    PRIMARY KEY (run_id, device_id, stage)
);
CREATE INDEX idx_flow_run_devices_cmd ON flow_run_devices(command_id);

-- +goose Down
DROP TABLE flow_run_devices;
DROP TABLE flow_runs;
DROP TABLE flows;
