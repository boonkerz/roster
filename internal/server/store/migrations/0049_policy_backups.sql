-- +goose Up
-- Backup-Bereich der Richtlinien: Einträge (was, wann, danach) und ihre Läufe.
-- Anders als policy_tasks werden Backups SERVERSEITIG geplant – nur der Server kennt
-- andere Geräte (Startbedingung, Folgeaktion) und behält Läufe über Neustarts.
CREATE TABLE policy_backups (
    id               TEXT    PRIMARY KEY,
    policy_id        TEXT    NOT NULL REFERENCES policies(id) ON DELETE CASCADE,
    name             TEXT    NOT NULL DEFAULT '',
    type             TEXT    NOT NULL DEFAULT 'proxmox', -- proxmox | script
    enabled          BOOLEAN NOT NULL DEFAULT TRUE,
    config           TEXT    NOT NULL DEFAULT '{}',      -- JSON, typabhängig
    script_id        TEXT REFERENCES scripts(id) ON DELETE SET NULL,
    -- Zeitplan (Serverzeit): Wochentage wie time.Weekday (0=Sonntag), leer = täglich.
    weekdays         TEXT    NOT NULL DEFAULT '',
    at_time          TEXT    NOT NULL DEFAULT '',        -- "18:00"
    catch_up_minutes INTEGER NOT NULL DEFAULT 360,       -- so lange darf ein Lauf nachgeholt werden
    -- Startbedingung: erst loslegen, wenn dieses Gerät online ist.
    wait_device_id   TEXT REFERENCES devices(id) ON DELETE SET NULL,
    wait_minutes     INTEGER NOT NULL DEFAULT 0,
    timeout_minutes  INTEGER NOT NULL DEFAULT 720,
    -- Folgeaktion auf einem anderen Gerät.
    after_device_id  TEXT REFERENCES devices(id) ON DELETE SET NULL,
    after_script_id  TEXT REFERENCES scripts(id) ON DELETE SET NULL,
    after_when       TEXT    NOT NULL DEFAULT 'always',  -- always | success | failure
    last_run_at      TIMESTAMP,
    created_at       TIMESTAMP NOT NULL
);
CREATE INDEX idx_policy_backups_policy ON policy_backups(policy_id);

CREATE TABLE backup_runs (
    id                TEXT    PRIMARY KEY,
    backup_id         TEXT    NOT NULL REFERENCES policy_backups(id) ON DELETE CASCADE,
    device_id         TEXT    NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    trigger_type      TEXT    NOT NULL DEFAULT 'schedule', -- schedule | manual
    status            TEXT    NOT NULL DEFAULT 'waiting',  -- waiting|running|ok|failed|timeout|skipped
    scheduled_at      TIMESTAMP NOT NULL,
    wait_until        TIMESTAMP,
    started_at        TIMESTAMP,
    finished_at       TIMESTAMP,
    deadline_at       TIMESTAMP,
    exit_code         INTEGER NOT NULL DEFAULT 0,
    summary           TEXT    NOT NULL DEFAULT '',
    output            TEXT    NOT NULL DEFAULT '',
    command_id        TEXT,
    follow_command_id TEXT,
    created_at        TIMESTAMP NOT NULL,
    -- Verhindert doppelte Läufe, falls zwei Serverinstanzen gleichzeitig planen.
    UNIQUE (backup_id, device_id, scheduled_at)
);
CREATE INDEX idx_backup_runs_backup ON backup_runs(backup_id, created_at);
CREATE INDEX idx_backup_runs_device ON backup_runs(device_id, created_at);
CREATE INDEX idx_backup_runs_cmd ON backup_runs(command_id);
CREATE INDEX idx_backup_runs_status ON backup_runs(status);

-- +goose Down
DROP TABLE backup_runs;
DROP TABLE policy_backups;
