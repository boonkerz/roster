-- +goose Up
-- Lüfter je Gerät (Momentaufnahme, bei jedem Checkin ersetzt). percent bleibt NULL,
-- wenn der Chip keine PWM-Ansteuerung verrät.
CREATE TABLE fans (
    device_id  TEXT    NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    pos        INTEGER NOT NULL DEFAULT 0,
    sensor     TEXT    NOT NULL DEFAULT '',
    label      TEXT    NOT NULL DEFAULT '',
    rpm        INTEGER NOT NULL DEFAULT 0,
    min_rpm    INTEGER NOT NULL DEFAULT 0,
    max_rpm    INTEGER NOT NULL DEFAULT 0,
    percent    INTEGER,
    alarm      BOOLEAN NOT NULL DEFAULT FALSE,
    updated_at TIMESTAMP NOT NULL
);
CREATE INDEX idx_fans_device ON fans(device_id);

-- +goose Down
DROP TABLE fans;
