-- +goose Up
-- Pro Gerät Software-Änderungs-Benachrichtigungen unterdrücken (z. B. bei Geräten,
-- die dauernd „flappende" Software melden).
ALTER TABLE devices ADD COLUMN mute_software_alerts BOOLEAN NOT NULL DEFAULT FALSE;

-- +goose Down
ALTER TABLE devices DROP COLUMN mute_software_alerts;
