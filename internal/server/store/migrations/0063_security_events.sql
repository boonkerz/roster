-- +goose Up
-- Anmeldungs-Überwachung: Rohereignisse, bekannte (Benutzer, Quell-IP)-Paare und
-- abgeleitete Sicherheitsvorfälle (Brute-Force, Erfolg nach Brute-Force, neue IP,
-- Kontoänderungen, Sperrungen). record_id macht den Import idempotent.
CREATE TABLE auth_events (
    id        TEXT PRIMARY KEY,
    device_id TEXT NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    ts        TIMESTAMP NOT NULL,
    kind      TEXT NOT NULL,
    username  TEXT NOT NULL DEFAULT '',
    source_ip TEXT NOT NULL DEFAULT '',
    service   TEXT NOT NULL DEFAULT '',
    detail    TEXT NOT NULL DEFAULT '',
    record_id TEXT NOT NULL DEFAULT '',
    UNIQUE (device_id, record_id, ts, kind, username, source_ip)
);
CREATE INDEX idx_auth_events_device_ts ON auth_events(device_id, ts);
CREATE INDEX idx_auth_events_ts ON auth_events(ts);
CREATE INDEX idx_auth_events_ip ON auth_events(source_ip, ts);

CREATE TABLE device_known_ips (
    device_id  TEXT NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    username   TEXT NOT NULL DEFAULT '',
    ip         TEXT NOT NULL,
    first_seen TIMESTAMP NOT NULL,
    last_seen  TIMESTAMP NOT NULL,
    PRIMARY KEY (device_id, username, ip)
);

CREATE TABLE security_incidents (
    id                  TEXT PRIMARY KEY,
    device_id           TEXT NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    ts                  TIMESTAMP NOT NULL,
    kind                TEXT NOT NULL,                 -- brute_force | brute_force_success | new_source_ip | account_change | lockout | fim_change
    severity            TEXT NOT NULL DEFAULT 'warning', -- info | warning | critical
    source_ip           TEXT NOT NULL DEFAULT '',
    username            TEXT NOT NULL DEFAULT '',
    title               TEXT NOT NULL DEFAULT '',
    detail              TEXT NOT NULL DEFAULT '',
    status              TEXT NOT NULL DEFAULT 'open',  -- open | acknowledged | resolved
    response            TEXT NOT NULL DEFAULT '',      -- Active Response (Phase 8): block_ip 1.2.3.4 …
    response_status     TEXT NOT NULL DEFAULT '',
    response_command_id TEXT NOT NULL DEFAULT '',
    acknowledged_by     TEXT NOT NULL DEFAULT '',
    acknowledged_at     TIMESTAMP
);
CREATE INDEX idx_security_incidents_device_ts ON security_incidents(device_id, ts);
CREATE INDEX idx_security_incidents_status_ts ON security_incidents(status, ts);

-- Beginn der Anmeldungs-Überwachung je Gerät (Lernphase für „neue IP“).
ALTER TABLE devices ADD COLUMN auth_monitor_since TIMESTAMP;

-- +goose Down
ALTER TABLE devices DROP COLUMN auth_monitor_since;
DROP TABLE security_incidents;
DROP TABLE device_known_ips;
DROP TABLE auth_events;
