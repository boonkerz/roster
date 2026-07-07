-- +goose Up
-- Ergebnis des Außen-Checks: Der (gehostete) Server testet die öffentliche IP des
-- Geräts auf den lauschenden TCP-Ports zurück -> was NAT/Firewall wirklich durchlässt.
-- Wird je Gerät bei jedem Außen-Check ersetzt.
CREATE TABLE external_ports (
    device_id  TEXT    NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    port       INTEGER NOT NULL,
    reachable  BOOLEAN NOT NULL,
    checked_at TIMESTAMP NOT NULL
);
CREATE INDEX idx_external_ports_device ON external_ports(device_id);

-- +goose Down
DROP TABLE external_ports;
