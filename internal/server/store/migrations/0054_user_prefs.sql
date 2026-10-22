-- +goose Up
-- Benutzereinstellungen der Web-UI (Spaltenauswahl, gespeicherte Filter, Export-
-- Spalten …) je Benutzer und Schlüssel als JSON – damit sie beim Anmelden an einem
-- anderen Browser wieder da sind, statt nur im localStorage zu liegen.
CREATE TABLE user_prefs (
    user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    key        TEXT NOT NULL,
    value      TEXT NOT NULL,
    updated_at TIMESTAMP NOT NULL,
    PRIMARY KEY (user_id, key)
);

-- +goose Down
DROP TABLE user_prefs;
