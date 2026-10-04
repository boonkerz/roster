-- +goose Up
-- Schalter: Check-Alarme bei Auto-Remediation aufschieben (TRUE) oder wie früher sofort
-- melden, auch wenn gerade eine Remediation läuft (FALSE).
ALTER TABLE alert_config ADD COLUMN alert_after_remediation BOOLEAN NOT NULL DEFAULT TRUE;

-- +goose Down
ALTER TABLE alert_config DROP COLUMN alert_after_remediation;
