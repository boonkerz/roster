-- +goose Up
-- Verfallsprüfung eingereihter Sperren je Gerät beim Checkin (status='pending' ist selten,
-- der Geräte-Index müsste sonst alle Sperren des Geräts lesen).
CREATE INDEX idx_ip_blocks_status_device ON ip_blocks(status, device_id);

-- +goose Down
DROP INDEX idx_ip_blocks_status_device;
