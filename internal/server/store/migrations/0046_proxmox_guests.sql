-- +goose Up
-- Proxmox-VE-Gäste je Gerät (vom Agent auf dem PVE-Host über pvesh gemeldet) samt
-- Backup-Status. Momentaufnahme, bei jedem Checkin vollständig ersetzt – analog
-- docker_containers. proxmox_version <> '' kennzeichnet ein Gerät als PVE-Host
-- (auch wenn es noch keine Gäste hat).
ALTER TABLE devices ADD COLUMN proxmox_version TEXT NOT NULL DEFAULT '';

CREATE TABLE proxmox_guests (
    device_id          TEXT    NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    node               TEXT    NOT NULL DEFAULT '',
    vmid               INTEGER NOT NULL,
    type               TEXT    NOT NULL DEFAULT '',
    name               TEXT    NOT NULL DEFAULT '',
    status             TEXT    NOT NULL DEFAULT '',
    template           BOOLEAN NOT NULL DEFAULT FALSE,
    cpus               REAL    NOT NULL DEFAULT 0,
    cpu                REAL    NOT NULL DEFAULT 0,
    mem                BIGINT  NOT NULL DEFAULT 0,
    maxmem             BIGINT  NOT NULL DEFAULT 0,
    maxdisk            BIGINT  NOT NULL DEFAULT 0,
    uptime             BIGINT  NOT NULL DEFAULT 0,
    backup_at          TIMESTAMP,
    backup_size        BIGINT  NOT NULL DEFAULT 0,
    backup_storage     TEXT    NOT NULL DEFAULT '',
    backup_count       INTEGER NOT NULL DEFAULT 0,
    backup_task_status TEXT    NOT NULL DEFAULT '',
    backup_task_at     TIMESTAMP,
    backup_task_msg    TEXT    NOT NULL DEFAULT '',
    backup_job         BOOLEAN,
    updated_at         TIMESTAMP NOT NULL
);
CREATE INDEX idx_proxmox_guests_device ON proxmox_guests(device_id);

-- +goose Down
DROP TABLE proxmox_guests;
ALTER TABLE devices DROP COLUMN proxmox_version;
