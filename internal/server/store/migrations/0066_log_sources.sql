-- +goose Up
-- Zusätzliche Protokolle je Richtlinie (nginx, SFTP-/FTP-Container, eigene Muster) für die
-- Angriffserkennung des Agents.
CREATE TABLE policy_log_sources (
    id        TEXT PRIMARY KEY,
    policy_id TEXT NOT NULL REFERENCES policies(id) ON DELETE CASCADE,
    name      TEXT NOT NULL,                 -- Dienstname in den Ereignissen
    type      TEXT NOT NULL,                 -- file | docker
    target    TEXT NOT NULL,                 -- Pfad (Glob) bzw. Container-Name/-Muster
    format    TEXT NOT NULL,                 -- nginx_access | nginx_error | sshd | ftp | regex
    pattern   TEXT NOT NULL DEFAULT '',      -- regex-Format: Muster mit (?P<ip>…)
    kind      TEXT NOT NULL DEFAULT '',      -- regex-Format: login_failed | probe
    enabled   BOOLEAN NOT NULL DEFAULT TRUE
);
CREATE INDEX idx_policy_log_sources_policy ON policy_log_sources(policy_id);

-- +goose Down
DROP TABLE policy_log_sources;
