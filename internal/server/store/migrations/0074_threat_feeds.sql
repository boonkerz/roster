-- +goose Up
-- Bedrohungsdaten: zwischengespeicherte Blocklisten (Rohtext), damit der Index nach einem
-- Neustart ohne Netzzugriff steht und ein ausgefallener Feed seinen letzten Stand behält.
CREATE TABLE threat_feeds (
    feed       TEXT PRIMARY KEY,
    data       TEXT NOT NULL,
    fetched_at TIMESTAMP NOT NULL,
    entries    INTEGER NOT NULL DEFAULT 0,
    error      TEXT NOT NULL DEFAULT ''
);

-- +goose Down
DROP TABLE threat_feeds;
