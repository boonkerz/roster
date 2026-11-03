-- +goose Up
-- Die Geräteliste (Dashboard/Geräteseite alle 15 s) zählt je Gerät das letzte Ergebnis je
-- Task (MAX(ran_at) je device_id+task_id – ohne diesen Index quadratisch über die
-- Task-Ergebnisse des Geräts) und offene Vorfälle (bisher Zeilen lesen statt Index).
CREATE INDEX idx_task_results_device_task ON task_results(device_id, task_id, ran_at);
CREATE INDEX idx_security_incidents_device_status ON security_incidents(device_id, status);

-- +goose Down
DROP INDEX idx_security_incidents_device_status;
DROP INDEX idx_task_results_device_task;
