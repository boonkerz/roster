-- +goose Up
-- Bereich „Sicherheitsüberwachung“ je Richtlinie: Datei-Integrität (Regeln + Intervall),
-- Anmeldungs-Überwachung (Schalter) und Compliance-Prüfung (Schalter/Profil/Regeln).
CREATE TABLE policy_security (
    policy_id             TEXT PRIMARY KEY REFERENCES policies(id) ON DELETE CASCADE,
    fim_interval_min      INTEGER NOT NULL DEFAULT 60,
    auth_monitor          BOOLEAN NOT NULL DEFAULT FALSE,
    compliance_enabled    BOOLEAN NOT NULL DEFAULT FALSE,
    compliance_profile    TEXT NOT NULL DEFAULT 'server',
    compliance_disabled   TEXT NOT NULL DEFAULT '',
    compliance_interval_h INTEGER NOT NULL DEFAULT 24
);
CREATE TABLE policy_fim_rules (
    id        TEXT PRIMARY KEY,
    policy_id TEXT NOT NULL REFERENCES policies(id) ON DELETE CASCADE,
    path      TEXT NOT NULL,
    recursive BOOLEAN NOT NULL DEFAULT TRUE,
    exclude   TEXT NOT NULL DEFAULT '',   -- ein Glob-Muster je Zeile
    platforms TEXT NOT NULL DEFAULT '',   -- JSON-Liste oder leer
    alert     BOOLEAN NOT NULL DEFAULT TRUE
);
CREATE INDEX idx_policy_fim_rules_policy ON policy_fim_rules(policy_id);

-- Erkannte Datei-Änderungen je Gerät.
CREATE TABLE fim_events (
    id           TEXT PRIMARY KEY,
    device_id    TEXT NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    ts           TIMESTAMP NOT NULL,
    rule_id      TEXT NOT NULL DEFAULT '',
    path         TEXT NOT NULL DEFAULT '',
    change       TEXT NOT NULL,            -- created | modified | deleted | attrs | baseline | limit
    old_hash     TEXT NOT NULL DEFAULT '',
    new_hash     TEXT NOT NULL DEFAULT '',
    old_mode     TEXT NOT NULL DEFAULT '',
    new_mode     TEXT NOT NULL DEFAULT '',
    old_owner    TEXT NOT NULL DEFAULT '',
    new_owner    TEXT NOT NULL DEFAULT '',
    size         BIGINT NOT NULL DEFAULT 0,
    alert        BOOLEAN NOT NULL DEFAULT TRUE,
    acknowledged BOOLEAN NOT NULL DEFAULT FALSE
);
CREATE INDEX idx_fim_events_device_ts ON fim_events(device_id, ts);
CREATE INDEX idx_fim_events_ts ON fim_events(ts);

-- +goose Down
DROP TABLE fim_events;
DROP TABLE policy_fim_rules;
DROP TABLE policy_security;
