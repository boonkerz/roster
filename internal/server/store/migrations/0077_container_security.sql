-- +goose Up
ALTER TABLE docker_images ADD COLUMN created_at TEXT NOT NULL DEFAULT '';

-- Container-Sicherheit: Paketlisten der Container-Images (vom Agent per docker exec gelesen),
-- deren OSV-Treffer und der Datenbank-Bestand in DB-Containern (Abweichungs-Erkennung).
CREATE TABLE docker_image_scans (
    device_id       TEXT NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    image_id        TEXT NOT NULL,
    image           TEXT NOT NULL DEFAULT '',
    os              TEXT NOT NULL DEFAULT '',
    os_version      TEXT NOT NULL DEFAULT '',
    os_name         TEXT NOT NULL DEFAULT '',
    pkg             TEXT NOT NULL DEFAULT '',
    packages        TEXT NOT NULL DEFAULT '',   -- JSON [{name,version}] für den OSV-Abgleich
    package_count   INTEGER NOT NULL DEFAULT 0,
    services        TEXT NOT NULL DEFAULT '',   -- erkannte Dienstpakete, kommagetrennt (sshd, ftpd, …)
    error           TEXT NOT NULL DEFAULT '',
    scanned_at      TIMESTAMP NOT NULL,
    vuln_scanned_at TIMESTAMP,                  -- NULL = OSV-Abgleich steht aus
    PRIMARY KEY (device_id, image_id)
);

CREATE TABLE container_vulnerabilities (
    device_id   TEXT NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    image_id    TEXT NOT NULL,
    image       TEXT NOT NULL DEFAULT '',
    package     TEXT NOT NULL,
    version     TEXT NOT NULL DEFAULT '',
    vuln_id     TEXT NOT NULL,
    severity    TEXT NOT NULL DEFAULT '',
    summary     TEXT NOT NULL DEFAULT '',
    fixed       TEXT NOT NULL DEFAULT '',
    url         TEXT NOT NULL DEFAULT '',
    detected_at TIMESTAMP NOT NULL,
    PRIMARY KEY (device_id, image_id, package, vuln_id)
);
CREATE INDEX idx_cvuln_device ON container_vulnerabilities(device_id);

CREATE TABLE docker_dbs (
    device_id    TEXT NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    container_id TEXT NOT NULL DEFAULT '',
    container    TEXT NOT NULL DEFAULT '',
    engine       TEXT NOT NULL DEFAULT '',
    name         TEXT NOT NULL DEFAULT '',
    collections  INTEGER NOT NULL DEFAULT 0,
    error        TEXT NOT NULL DEFAULT '',
    updated_at   TIMESTAMP NOT NULL
);
CREATE INDEX idx_docker_dbs_device ON docker_dbs(device_id);

-- +goose Down
ALTER TABLE docker_images DROP COLUMN created_at;
DROP TABLE docker_dbs;
DROP TABLE container_vulnerabilities;
DROP TABLE docker_image_scans;
