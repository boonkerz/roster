-- +goose Up
-- Temperatursensoren je Gerät (Momentaufnahme, bei jedem Checkin ersetzt) und die
-- Leittemperatur im Auslastungsverlauf. temp bleibt NULL für Geräte ohne Sensoren
-- und für alle Samples von vor dieser Version.
CREATE TABLE temperatures (
    device_id  TEXT    NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    pos        INTEGER NOT NULL DEFAULT 0, -- Reihenfolge wie vom Agent gemeldet (CPU zuerst)
    sensor     TEXT    NOT NULL DEFAULT '',
    label      TEXT    NOT NULL DEFAULT '',
    class      TEXT    NOT NULL DEFAULT '',
    celsius    REAL    NOT NULL DEFAULT 0,
    high       REAL    NOT NULL DEFAULT 0,
    critical   REAL    NOT NULL DEFAULT 0,
    updated_at TIMESTAMP NOT NULL
);
CREATE INDEX idx_temperatures_device ON temperatures(device_id);

ALTER TABLE metrics_samples ADD COLUMN temp REAL;

-- +goose Down
ALTER TABLE metrics_samples DROP COLUMN temp;
DROP TABLE temperatures;
