-- +goose Up
-- Recherche de patients insensible à la casse et aux accents (T017).
CREATE EXTENSION IF NOT EXISTS unaccent WITH SCHEMA public;
CREATE EXTENSION IF NOT EXISTS pg_trgm WITH SCHEMA public;

-- unaccent() n'est pas IMMUTABLE (dépend du dictionnaire) : wrapper immutable
-- indispensable pour l'indexer. Le dictionnaire est fixé explicitement.
-- +goose StatementBegin
CREATE FUNCTION f_unaccent(text) RETURNS text
    LANGUAGE sql IMMUTABLE PARALLEL SAFE STRICT
    AS $$ SELECT public.unaccent('public.unaccent'::regdictionary, $1) $$;
-- +goose StatementEnd

-- (unaccent est qualifié en dur : la construction d'un index n'utilise pas le
-- search_path de la session.)
-- Texte de recherche : prénom/nom dans les deux ordres, téléphone, email ;
-- minuscules, sans accents.
-- +goose StatementBegin
CREATE FUNCTION patient_search_text(first_name text, last_name text, phone text, email citext) RETURNS text
    LANGUAGE sql IMMUTABLE PARALLEL SAFE
    AS $$ SELECT public.unaccent('public.unaccent'::regdictionary, lower(first_name || ' ' || last_name || ' ' || last_name || ' ' || first_name
                                  || ' ' || coalesce(phone, '') || ' ' || coalesce(email::text, ''))) $$;
-- +goose StatementEnd

CREATE INDEX patient_search_trgm_idx ON patient
    USING gin (patient_search_text(first_name, last_name, phone, email) public.gin_trgm_ops)
    WHERE deleted_at IS NULL;

-- Fusion de doublons : la fiche absorbée est soft-deleted et pointe vers la fiche conservée.
ALTER TABLE patient ADD COLUMN merged_into uuid REFERENCES patient (id);

-- +goose Down
ALTER TABLE patient DROP COLUMN merged_into;
DROP INDEX patient_search_trgm_idx;
DROP FUNCTION patient_search_text(text, text, text, citext);
DROP FUNCTION f_unaccent(text);
