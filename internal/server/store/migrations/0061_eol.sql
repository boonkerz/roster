-- +goose Up
-- Support-Ende (EOL) je Gerät: Zyklen-Cache von endoflife.date je Produkt und die
-- Zuordnung/Bewertung je Gerät (kind os = Betriebssystem, proxmox = PVE-Hauptversion).
CREATE TABLE eol_cache (
    product    TEXT PRIMARY KEY,
    data       TEXT NOT NULL,
    fetched_at TIMESTAMP NOT NULL
);
CREATE TABLE device_eol (
    device_id      TEXT NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    kind           TEXT NOT NULL DEFAULT 'os',
    product        TEXT NOT NULL,
    cycle          TEXT NOT NULL,
    label          TEXT NOT NULL DEFAULT '',
    eol_date       TIMESTAMP,
    support_date   TIMESTAMP,
    status         TEXT NOT NULL DEFAULT 'unknown',  -- ok | soon | eol | unknown
    checked_at     TIMESTAMP NOT NULL,
    alerted_status TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (device_id, kind)
);

-- +goose Down
DROP TABLE device_eol;
DROP TABLE eol_cache;
