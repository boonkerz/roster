-- +goose Up
-- Aufgeschobene Check-Alarme: wechselt ein Check auf „failing" und löst eine
-- Auto-Remediation aus, wird erst benachrichtigt, wenn der Check NACH der Remediation
-- immer noch fehlschlägt. Heilt sich das Gerät selbst, bleibt alles still – auch die
-- „behoben"-Meldung, denn der Fehler wurde nie gemeldet. Eine Zeile je Gerät/Check.
CREATE TABLE deferred_check_alerts (
    id          TEXT PRIMARY KEY,
    device_id   TEXT NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    check_id    TEXT NOT NULL,
    check_name  TEXT NOT NULL DEFAULT '',
    event_id    TEXT NOT NULL,
    command_id  TEXT NOT NULL DEFAULT '',   -- Remediation-Skript (leer: nur Proxmox-Reboot)
    not_before  TIMESTAMP NOT NULL,         -- frühestens dann melden
    created_at  TIMESTAMP NOT NULL,
    UNIQUE(device_id, check_id)
);

-- +goose Down
DROP TABLE deferred_check_alerts;
