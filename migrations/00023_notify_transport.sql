-- +goose Up
-- Fournisseurs d'email et de SMS par cabinet (T086). Absence de ligne = « server » :
-- le fournisseur configuré par les variables d'environnement du serveur.
-- settings : paramètres non secrets ; secrets : identifiants chiffrés (AES-256-GCM, internal/secretbox).
CREATE TABLE practice_notify_transport (
    practice_id uuid        NOT NULL REFERENCES practice (id),
    kind        text        NOT NULL CHECK (kind IN ('email', 'sms')),
    mode        text        NOT NULL CHECK (mode IN ('server', 'custom', 'disabled')),
    driver      text,
    settings    jsonb       NOT NULL DEFAULT '{}',
    secrets     bytea,
    updated_at  timestamptz NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (practice_id, kind),
    CHECK ((mode = 'custom') = (driver IS NOT NULL)),
    CHECK (driver IS NULL OR (kind = 'email' AND driver = 'smtp') OR (kind = 'sms' AND driver IN ('http', 'android', 'ovh')))
);

-- +goose Down
DROP TABLE practice_notify_transport;
