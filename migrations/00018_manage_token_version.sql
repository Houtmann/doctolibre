-- +goose Up
-- Le jeton du lien de gestion /r/{jeton} est dérivé (HMAC) de l'id du RDV et de cette
-- version : monter la version (RevokeManageToken) invalide l'ancien lien.
ALTER TABLE appointment ADD COLUMN manage_token_version int NOT NULL DEFAULT 1;

-- +goose Down
ALTER TABLE appointment DROP COLUMN manage_token_version;
