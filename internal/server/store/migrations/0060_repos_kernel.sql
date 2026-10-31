-- +goose Up
-- Paketquellen je Gerät (Replace-on-Checkin wie listen_ports) sowie Auto-Update-Status,
-- Kernel (laufend/installiert) und OS-Details (Build/Codename/Edition) am Gerät.
CREATE TABLE package_repos (
    device_id   TEXT NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    repo_id     TEXT NOT NULL DEFAULT '',
    name        TEXT NOT NULL DEFAULT '',
    url         TEXT NOT NULL DEFAULT '',
    type        TEXT NOT NULL DEFAULT '',   -- apt | dnf | zypper | pacman | apk | winget | brew
    enabled     BOOLEAN NOT NULL DEFAULT TRUE,
    gpg_check   BOOLEAN NOT NULL DEFAULT TRUE,
    suite       TEXT NOT NULL DEFAULT '',
    third_party BOOLEAN NOT NULL DEFAULT FALSE
);
CREATE INDEX idx_package_repos_device ON package_repos(device_id);

ALTER TABLE devices ADD COLUMN auto_updates TEXT NOT NULL DEFAULT '';
ALTER TABLE devices ADD COLUMN kernel_running TEXT NOT NULL DEFAULT '';
ALTER TABLE devices ADD COLUMN kernel_installed TEXT NOT NULL DEFAULT '';
ALTER TABLE devices ADD COLUMN os_build TEXT NOT NULL DEFAULT '';
ALTER TABLE devices ADD COLUMN os_codename TEXT NOT NULL DEFAULT '';
ALTER TABLE devices ADD COLUMN os_edition TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE devices DROP COLUMN os_edition;
ALTER TABLE devices DROP COLUMN os_codename;
ALTER TABLE devices DROP COLUMN os_build;
ALTER TABLE devices DROP COLUMN kernel_installed;
ALTER TABLE devices DROP COLUMN kernel_running;
ALTER TABLE devices DROP COLUMN auto_updates;
DROP TABLE package_repos;
