-- +goose Up
-- Sperren werden je Adresse über alle Geräte nachgeschlagen (Flottensperre, „schon
-- gesperrt?“); der bisherige Index (device_id, created_at) half dabei nicht.
CREATE INDEX idx_ip_blocks_ip_status ON ip_blocks(ip, status, device_id);

-- +goose Down
DROP INDEX idx_ip_blocks_ip_status;
