-- +goose Up
-- Katalog verteilbarer Software-Pakete (je Paketmanager eine Kennung).
CREATE TABLE software_packages (
    id     TEXT PRIMARY KEY,
    name   TEXT NOT NULL,
    winget TEXT NOT NULL DEFAULT '',
    choco  TEXT NOT NULL DEFAULT '',
    apt    TEXT NOT NULL DEFAULT '',
    dnf    TEXT NOT NULL DEFAULT '',
    brew   TEXT NOT NULL DEFAULT ''
);

-- +goose Down
DROP TABLE software_packages;
