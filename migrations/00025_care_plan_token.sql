-- +goose Up
-- Lien de série /s/{jeton} (T055) : jeton dérivé (HMAC) de l'id de la série et de cette
-- version ; seul le sha256 du jeton est stocké. Monter la version et vider le haché
-- (fusion de fiches, effacement) invalide le lien. Séries existantes : haché NULL,
-- renseigné à la première génération du lien.
ALTER TABLE care_plan
    ADD COLUMN token_version int NOT NULL DEFAULT 1,
    ADD COLUMN token_hash text UNIQUE;

-- +goose Down
ALTER TABLE care_plan DROP COLUMN token_hash, DROP COLUMN token_version;
