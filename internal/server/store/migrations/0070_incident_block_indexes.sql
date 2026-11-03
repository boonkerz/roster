-- +goose Up
-- Jede Befehlsrückmeldung sucht den Vorfall über response_command_id (bisher ein voller
-- Tabellendurchlauf je Ergebnis); die Sicherheitsübersicht zählt Sperren im Zeitfenster
-- (bisher voller Durchlauf der – mit der Flottensperre großen – ip_blocks je Aufruf).
CREATE INDEX idx_security_incidents_response_cmd ON security_incidents(response_command_id);
CREATE INDEX idx_ip_blocks_created ON ip_blocks(created_at, ip, device_id);

-- +goose Down
DROP INDEX idx_ip_blocks_created;
DROP INDEX idx_security_incidents_response_cmd;
