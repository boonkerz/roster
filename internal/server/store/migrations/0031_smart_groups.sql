-- +goose Up
-- Smart Groups: dynamische Mitgliedschaft per Regel (JSON). Leer = statische Gruppe.
ALTER TABLE groups ADD COLUMN rule TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE groups DROP COLUMN rule;
