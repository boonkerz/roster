-- +goose Up
-- Active Response: Reaktionsregeln je Richtlinie (Trigger → Aktion) und ausgeführte IP-Sperren.
CREATE TABLE policy_responses (
    id        TEXT PRIMARY KEY,
    policy_id TEXT NOT NULL REFERENCES policies(id) ON DELETE CASCADE,
    trigger   TEXT NOT NULL,                -- brute_force | brute_force_success | new_source_ip | account_change | lockout | fim_change
    action    TEXT NOT NULL,                -- block_ip | lock_user | run_script
    minutes   INTEGER NOT NULL DEFAULT 60,  -- block_ip: Dauer, 0 = dauerhaft
    script_id TEXT,                         -- run_script
    platforms TEXT NOT NULL DEFAULT '',     -- JSON-Liste oder leer
    enabled   BOOLEAN NOT NULL DEFAULT TRUE
);
CREATE INDEX idx_policy_responses_policy ON policy_responses(policy_id);

CREATE TABLE ip_blocks (
    id          TEXT PRIMARY KEY,
    device_id   TEXT NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    ip          TEXT NOT NULL,
    reason      TEXT NOT NULL DEFAULT '',
    incident_id TEXT NOT NULL DEFAULT '',
    until_at    TIMESTAMP,                  -- NULL = dauerhaft
    status      TEXT NOT NULL DEFAULT 'pending', -- pending | active | released | failed
    detail      TEXT NOT NULL DEFAULT '',
    command_id  TEXT NOT NULL DEFAULT '',
    created_by  TEXT NOT NULL DEFAULT 'system',
    created_at  TIMESTAMP NOT NULL
);
CREATE INDEX idx_ip_blocks_device ON ip_blocks(device_id, created_at);
CREATE INDEX idx_ip_blocks_command ON ip_blocks(command_id);

-- +goose Down
DROP TABLE ip_blocks;
DROP TABLE policy_responses;
