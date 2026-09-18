-- +goose Up
-- Allgemeiner Schlüssel-Wert-Topf für globale Einstellungen. Bisher hatte jede
-- Einstellung eine eigene Ein-Zeilen-Tabelle (alert_config …); für einzelne Werte wie
-- die Zeitzone ist das zu viel Zeremonie. Werte sind immer Text – die Auswertung macht
-- der Aufrufer.
--
-- Bekannte Schlüssel:
--   timezone  IANA-Name ("Europe/Berlin"). Leer/fehlend = Systemzeit des jeweiligen
--             Geräts (Server für Backups, Agent für Tasks/Checks) – also wie bisher.
CREATE TABLE app_settings (
    key        TEXT PRIMARY KEY,
    value      TEXT NOT NULL DEFAULT '',
    updated_at TIMESTAMP NOT NULL
);

-- +goose Down
DROP TABLE app_settings;
