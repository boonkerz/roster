-- +goose Up
-- Benachrichtigungen je Richtlinie: welche Sicherheitsvorfälle gemeldet werden ('*' = alle,
-- sonst kommagetrennte Arten, leer = keine) und ob automatisch gesperrte Vorfälle still bleiben.
ALTER TABLE policies ADD COLUMN security_alert_kinds TEXT NOT NULL DEFAULT '*';
ALTER TABLE policies ADD COLUMN security_alert_skip_blocked BOOLEAN NOT NULL DEFAULT FALSE;

-- +goose Down
ALTER TABLE policies DROP COLUMN security_alert_skip_blocked;
ALTER TABLE policies DROP COLUMN security_alert_kinds;
