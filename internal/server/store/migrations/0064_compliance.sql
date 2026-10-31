-- +goose Up
-- Compliance-/Baseline-Prüfung: Ergebnis je Gerät und Regel plus denormalisierter Score.
CREATE TABLE compliance_results (
    device_id  TEXT NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    rule_id    TEXT NOT NULL,
    status     TEXT NOT NULL,            -- pass | fail | na | error
    evidence   TEXT NOT NULL DEFAULT '',
    checked_at TIMESTAMP NOT NULL,
    PRIMARY KEY (device_id, rule_id)
);
ALTER TABLE devices ADD COLUMN compliance_score INTEGER;
ALTER TABLE devices ADD COLUMN compliance_checked_at TIMESTAMP;
ALTER TABLE devices ADD COLUMN compliance_profile TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE devices DROP COLUMN compliance_profile;
ALTER TABLE devices DROP COLUMN compliance_checked_at;
ALTER TABLE devices DROP COLUMN compliance_score;
DROP TABLE compliance_results;
