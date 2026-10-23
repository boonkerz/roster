-- +goose Up
-- Stündliche Statistik-Snapshots für die Dashboard-Diagramme: je Standort eine Zeile
-- pro Kennzahl (kind=summary), Check-Status (kind=check, label=passing/warning/failing)
-- und Feldwert (kind=field, label=Wert) mit der Anzahl Geräte als value.
CREATE TABLE stat_samples (
    ts      BIGINT NOT NULL,              -- Unix-Millisekunden, auf volle Stunde gerundet
    site_id TEXT   NOT NULL DEFAULT '',   -- '' = Gerät ohne Standort
    kind    TEXT   NOT NULL,              -- summary | check | field
    key     TEXT   NOT NULL,              -- Kennzahl, Check-ID oder Feld-ID
    label   TEXT   NOT NULL DEFAULT '',   -- Status bzw. Feldwert ('' = einzelne Reihe)
    value   REAL   NOT NULL
);
CREATE INDEX idx_stat_samples_lookup ON stat_samples(kind, key, ts);
CREATE INDEX idx_stat_samples_ts ON stat_samples(ts);

-- +goose Down
DROP TABLE stat_samples;
