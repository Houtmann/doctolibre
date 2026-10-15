-- +goose Up
-- Jetons de réinitialisation du mot de passe (personnel). token_hash = sha256
-- hexadécimal du jeton envoyé par email, jamais le jeton lui-même.
CREATE TABLE password_reset (
    token_hash text PRIMARY KEY,
    user_id    uuid NOT NULL REFERENCES app_user (id) ON DELETE CASCADE,
    expires_at timestamptz NOT NULL,
    used_at    timestamptz,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX password_reset_user_idx ON password_reset (user_id);
CREATE INDEX password_reset_expires_idx ON password_reset (expires_at);

-- +goose Down
DROP TABLE password_reset;
