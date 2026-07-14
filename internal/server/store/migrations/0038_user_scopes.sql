-- +goose Up
-- Daten-Scope pro Benutzer: begrenzt die Sichtbarkeit auf bestimmte Kunden/Standorte.
-- Hat ein Benutzer KEINE Einträge, sieht er alles (unverändertes Verhalten). Admins
-- sind immer unbeschränkt.
CREATE TABLE user_scopes (
    user_id     TEXT NOT NULL,
    target_type TEXT NOT NULL,          -- 'client' | 'site'
    target_id   TEXT NOT NULL,
    PRIMARY KEY (user_id, target_type, target_id)
);
CREATE INDEX idx_user_scopes_user ON user_scopes(user_id);

-- +goose Down
DROP TABLE user_scopes;
