-- +goose Up
-- Dashboard/Sicherheitsübersicht lesen Fehlversuche und Angriffsquellen über Zeitfenster
-- (24 h / 7 Tage) – abdeckender Index, damit keine Tabellenzeilen angefasst werden.
CREATE INDEX idx_auth_events_ts_kind ON auth_events(ts, kind, source_ip, device_id);

-- +goose Down
DROP INDEX idx_auth_events_ts_kind;
