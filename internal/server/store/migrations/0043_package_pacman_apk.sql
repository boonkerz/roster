-- +goose Up
-- Weitere Paketmanager-Kennungen: pacman (Arch) und apk (Alpine).
ALTER TABLE software_packages ADD COLUMN pacman TEXT NOT NULL DEFAULT '';
ALTER TABLE software_packages ADD COLUMN apk TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE software_packages DROP COLUMN apk;
ALTER TABLE software_packages DROP COLUMN pacman;
