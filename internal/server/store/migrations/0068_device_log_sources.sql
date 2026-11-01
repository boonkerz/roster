-- +goose Up
-- Lesestatus der Log-Quellen je Gerät (vom Agent mit dem Checkin gemeldet): gefundene
-- Dateien/Container, Ereignisse seit Agent-Start, letzter Treffer, Fehler.
CREATE TABLE device_log_sources (
    device_id     TEXT NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    source_id     TEXT NOT NULL,
    name          TEXT NOT NULL DEFAULT '',
    type          TEXT NOT NULL DEFAULT '',
    target        TEXT NOT NULL DEFAULT '',
    targets       INTEGER NOT NULL DEFAULT 0,
    events        INTEGER NOT NULL DEFAULT 0,
    last_event_at TIMESTAMP NULL,
    last_run_at   TIMESTAMP NULL,
    error         TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (device_id, source_id)
);

-- +goose Down
DROP TABLE device_log_sources;
