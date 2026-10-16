-- +goose Up
-- Désactivation d'un compte (T033) : l'utilisateur ne peut plus se connecter,
-- ses sessions sont révoquées, mais ses rendez-vous et son historique restent.
ALTER TABLE app_user ADD COLUMN active boolean NOT NULL DEFAULT true;

-- Invitations de membres de l'équipe. token_hash = sha256 hexadécimal du jeton
-- envoyé par email, jamais le jeton lui-même ; usage unique (used_at).
CREATE TABLE user_invitation (
    token_hash  text PRIMARY KEY,
    practice_id uuid NOT NULL REFERENCES practice (id),
    invited_by  uuid REFERENCES app_user (id) ON DELETE SET NULL,
    email       citext NOT NULL,
    role        text NOT NULL CHECK (role IN ('admin', 'practitioner', 'secretary')),
    expires_at  timestamptz NOT NULL,
    used_at     timestamptz,
    created_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX user_invitation_practice_idx ON user_invitation (practice_id, created_at DESC);
CREATE INDEX user_invitation_expires_idx ON user_invitation (expires_at);

-- +goose Down
DROP TABLE user_invitation;
ALTER TABLE app_user DROP COLUMN active;
