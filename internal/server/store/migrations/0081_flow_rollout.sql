-- +goose Up
-- Gestufter Rollout von Abläufen: höchstens N Geräte gleichzeitig (0 = alle sofort), bei
-- Fehler anhalten; resumed_at = Zeitpunkt von „Trotzdem fortsetzen“ (ältere Fehler zählen
-- dann nicht mehr als Halt). Geräte warten in flow_run_devices mit status 'pending'.
ALTER TABLE flow_runs ADD COLUMN parallel INTEGER NOT NULL DEFAULT 0;
ALTER TABLE flow_runs ADD COLUMN stop_on_fail BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE flow_runs ADD COLUMN resumed_at TIMESTAMP;

-- +goose Down
ALTER TABLE flow_runs DROP COLUMN resumed_at;
ALTER TABLE flow_runs DROP COLUMN stop_on_fail;
ALTER TABLE flow_runs DROP COLUMN parallel;
