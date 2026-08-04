-- +goose Up
-- Langlebige User-API-Tokens (Bearer) für native Clients / die Mobile-App. Werden nur
-- gehasht gespeichert und im Klartext ausschließlich einmalig bei der Erzeugung ausgegeben.
CREATE TABLE user_api_tokens (
    id           TEXT PRIMARY KEY,
    user_id      TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    label        TEXT NOT NULL DEFAULT '',
    token_hash   TEXT NOT NULL UNIQUE,
    created_at   TIMESTAMP NOT NULL,
    last_used_at TIMESTAMP,
    expires_at   TIMESTAMP,
    revoked      BOOLEAN NOT NULL DEFAULT FALSE
);
CREATE INDEX idx_user_api_tokens_user ON user_api_tokens(user_id);

-- +goose Down
DROP TABLE user_api_tokens;
