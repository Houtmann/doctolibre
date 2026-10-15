-- +goose Up
-- Jeton d'abonnement au flux ICS d'un utilisateur : seul le sha256 (hex) est stocké.
-- Une ligne par utilisateur ; régénérer remplace le haché (l'ancien lien meurt),
-- révoquer supprime la ligne. full_names : titre avec le nom complet du patient
-- (sinon initiales), à activer en connaissance de cause (le flux transite par Google/Apple).
CREATE TABLE calendar_feed_token (
    user_id    uuid PRIMARY KEY REFERENCES app_user (id) ON DELETE CASCADE,
    token_hash text NOT NULL UNIQUE,
    full_names boolean NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE calendar_feed_token;
