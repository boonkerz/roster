-- +goose Up
-- Ablauf-Index für die Sperren-Pflege: abgelaufene wirksame Sperren schnell finden (die
-- Flottensperre legt je Adresse einen Eintrag pro Gerät an – Millionen Zeilen).
CREATE INDEX IF NOT EXISTS idx_ip_blocks_until ON ip_blocks(until_at) WHERE status = 'active';

-- +goose Down
DROP INDEX IF EXISTS idx_ip_blocks_until;
