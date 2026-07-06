-- +goose Up
-- Nicht verwaltete Geräte (ohne Agent, z. B. aus dem Netzwerk-Scan übernommen).
ALTER TABLE devices ADD COLUMN managed BOOLEAN NOT NULL DEFAULT 1;

-- +goose Down
ALTER TABLE devices DROP COLUMN managed;
