-- +goose Up
-- Verlauf der Check-Ergebnisse: je Checkin eine Zeile pro Check (Status + Messwert),
-- für Verlaufsdiagramme am Gerät. Wird nach der Check-Historie-Aufbewahrung
-- (Standard 10 Tage) wieder gelöscht.
CREATE TABLE check_samples (
    device_id TEXT   NOT NULL,
    check_id  TEXT   NOT NULL,
    ts        BIGINT NOT NULL, -- Unix-Millisekunden
    status    TEXT   NOT NULL,
    value     REAL
);
CREATE INDEX idx_check_samples_key ON check_samples(device_id, check_id, ts);
CREATE INDEX idx_check_samples_ts ON check_samples(ts);

-- +goose Down
DROP TABLE check_samples;
