-- +goose Up
-- Désinscription en ligne de la liste d'attente (T097) : lien /wl/{jeton}. Le jeton est
-- dérivé (HMAC de l'id de l'inscription, comme T050/T055) ; seul son sha256 est stocké.
-- Inscriptions antérieures : haché NULL, pas de lien de désinscription dans leurs messages.
ALTER TABLE waitlist_entry ADD COLUMN unsubscribe_hash text UNIQUE;

-- +goose Down
ALTER TABLE waitlist_entry DROP COLUMN unsubscribe_hash;
