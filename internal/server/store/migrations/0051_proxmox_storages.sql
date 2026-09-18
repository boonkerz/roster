-- +goose Up
-- Backup-fähige Speicher eines PVE-Hosts, vom Agent bei jedem Checkin gemeldet. Bewusst
-- eine JSON-Spalte statt einer Tabelle wie proxmox_guests: es ist eine kurze Namensliste
-- ohne eigene Auswertung, sie füttert nur die Speicher-Auswahl im Backup-Formular.
-- Format: [{"name":"backup-pi_1","node":"pve","shared":false}, …], leer = unbekannt.
ALTER TABLE devices ADD COLUMN proxmox_storages TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE devices DROP COLUMN proxmox_storages;
