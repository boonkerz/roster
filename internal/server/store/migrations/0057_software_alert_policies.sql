-- +goose Up
-- Software-Änderungs-Benachrichtigungen über Richtlinien steuern (Bereich „Benachrichtigungen“).
-- software_alerts: '' = Standard (globaler Schalter alert_config.alert_software) | on | off.
-- Bei mehreren zugewiesenen Richtlinien gilt Gerät > Standort > Client (siehe ResolveSoftwareAlerts).
ALTER TABLE policies ADD COLUMN software_alerts TEXT NOT NULL DEFAULT '';
ALTER TABLE policies ADD COLUMN software_alert_kinds TEXT NOT NULL DEFAULT 'added,removed,updated';
ALTER TABLE policies ADD COLUMN software_ignore TEXT NOT NULL DEFAULT '';

-- Der Geräte-Schalter wird dreistufig ('' = wie Richtlinie | on | off); bisher stumm → off.
ALTER TABLE devices ADD COLUMN software_alerts TEXT NOT NULL DEFAULT '';
UPDATE devices SET software_alerts='off' WHERE mute_software_alerts;
ALTER TABLE devices DROP COLUMN mute_software_alerts;

-- +goose Down
ALTER TABLE devices ADD COLUMN mute_software_alerts BOOLEAN NOT NULL DEFAULT FALSE;
UPDATE devices SET mute_software_alerts=TRUE WHERE software_alerts='off';
ALTER TABLE devices DROP COLUMN software_alerts;
ALTER TABLE policies DROP COLUMN software_ignore;
ALTER TABLE policies DROP COLUMN software_alert_kinds;
ALTER TABLE policies DROP COLUMN software_alerts;
