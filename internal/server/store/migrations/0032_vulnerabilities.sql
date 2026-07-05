-- +goose Up
-- Bekannte Schwachstellen (CVE/OSV) je Gerät, ermittelt durch Abgleich der
-- installierten Software gegen OSV.dev.
CREATE TABLE vulnerabilities (
    device_id   TEXT NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    package     TEXT NOT NULL,
    version     TEXT NOT NULL DEFAULT '',
    vuln_id     TEXT NOT NULL,
    severity    TEXT NOT NULL DEFAULT '',
    summary     TEXT NOT NULL DEFAULT '',
    fixed       TEXT NOT NULL DEFAULT '',
    url         TEXT NOT NULL DEFAULT '',
    detected_at TIMESTAMP NOT NULL,
    PRIMARY KEY (device_id, package, vuln_id)
);
CREATE INDEX idx_vuln_device ON vulnerabilities(device_id);
CREATE INDEX idx_vuln_severity ON vulnerabilities(severity);

-- +goose Down
DROP TABLE vulnerabilities;
