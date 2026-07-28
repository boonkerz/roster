-- +goose Up
-- Healthcheck-Status je Container (healthy|unhealthy|starting|"" wenn kein Healthcheck).
ALTER TABLE docker_containers ADD COLUMN health TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE docker_containers DROP COLUMN health;
