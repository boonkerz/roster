-- +goose Up
-- Sicherheits-Updates und Versionsangaben je ausstehendem Patch (PatchMon-Funktionen).
-- security: Paketmanager meldet das Update als Sicherheits-Update (apt -security-Suite,
-- dnf-Security-Advisory, WUA-Kategorie „Security Updates“/MSRC-Schweregrad, macOS „Security“).
ALTER TABLE os_updates ADD COLUMN version TEXT NOT NULL DEFAULT '';
ALTER TABLE os_updates ADD COLUMN current_version TEXT NOT NULL DEFAULT '';
ALTER TABLE os_updates ADD COLUMN source TEXT NOT NULL DEFAULT '';
ALTER TABLE os_updates ADD COLUMN security BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE os_updates ADD COLUMN kind TEXT NOT NULL DEFAULT '';

-- Denormalisierter Zähler für Liste/Dashboard/Smart Groups; NULL = noch nicht geprüft.
ALTER TABLE devices ADD COLUMN security_updates_count INTEGER;

-- +goose Down
ALTER TABLE devices DROP COLUMN security_updates_count;
ALTER TABLE os_updates DROP COLUMN kind;
ALTER TABLE os_updates DROP COLUMN security;
ALTER TABLE os_updates DROP COLUMN source;
ALTER TABLE os_updates DROP COLUMN current_version;
ALTER TABLE os_updates DROP COLUMN version;
