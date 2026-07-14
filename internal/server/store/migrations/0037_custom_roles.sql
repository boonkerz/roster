-- +goose Up
-- Custom-Rollen: wiederverwendbare Rechte-Sets (Seiten/Funktionen). Ein Benutzer mit
-- gesetzter custom_role_id erhält genau diese Rechte; sonst gelten die Default-Rechte
-- seiner eingebauten Rolle (admin/technician/viewer).
CREATE TABLE custom_roles (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL UNIQUE,
    permissions TEXT NOT NULL DEFAULT '[]', -- JSON-Array von Permission-Keys
    created_at  TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

ALTER TABLE users ADD COLUMN custom_role_id TEXT REFERENCES custom_roles(id) ON DELETE SET NULL;

-- +goose Down
ALTER TABLE users DROP COLUMN custom_role_id;
DROP TABLE custom_roles;
