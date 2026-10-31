-- +goose Up
-- Patch-Verlauf je Gerät (aus apt/dnf/yum/pacman-Logs, Windows-Update-Historie,
-- softwareupdate). Der Agent schickt die letzten 90 Tage erneut; die UNIQUE-Bedingung
-- macht das Einfügen idempotent (ON CONFLICT DO NOTHING).
CREATE TABLE patch_history (
    id          TEXT PRIMARY KEY,
    device_id   TEXT NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    ts          TIMESTAMP NOT NULL,
    action      TEXT NOT NULL,              -- install | upgrade | remove
    name        TEXT NOT NULL,
    old_version TEXT NOT NULL DEFAULT '',
    new_version TEXT NOT NULL DEFAULT '',
    source      TEXT NOT NULL DEFAULT '',   -- apt | dnf | yum | pacman | wua | softwareupdate
    detail      TEXT NOT NULL DEFAULT '',
    UNIQUE (device_id, ts, name, action, new_version)
);
CREATE INDEX idx_patch_history_device_ts ON patch_history(device_id, ts);
CREATE INDEX idx_patch_history_ts ON patch_history(ts);

-- +goose Down
DROP TABLE patch_history;
