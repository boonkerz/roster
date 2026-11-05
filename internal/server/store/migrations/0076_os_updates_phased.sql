-- +goose Up
-- Ubuntu Phased Updates: apt stellt Pakete bis zur Ausrollquote zurück (auch bei dist-upgrade);
-- der Agent markiert sie, damit die Oberfläche zeigt, warum ein Update liegen bleibt.
ALTER TABLE os_updates ADD COLUMN phased BOOLEAN NOT NULL DEFAULT FALSE;

-- +goose Down
ALTER TABLE os_updates DROP COLUMN phased;
