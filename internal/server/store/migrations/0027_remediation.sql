-- +goose Up
-- Self-Healing: optionales Remediation-Skript je Check + Cooldown gegen Flapping.
ALTER TABLE policy_checks ADD COLUMN remediation_script_id TEXT;

CREATE TABLE remediation_runs (
    device_id  TEXT NOT NULL,
    check_id   TEXT NOT NULL,
    last_run   TIMESTAMP NOT NULL,
    PRIMARY KEY (device_id, check_id)
);

-- +goose Down
DROP TABLE remediation_runs;
ALTER TABLE policy_checks DROP COLUMN remediation_script_id;
