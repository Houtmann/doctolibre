-- +goose Up
-- Remplaçant (T059) : accès à l'agenda d'un praticien titulaire pour une période
-- donnée. `valid` est une plage de jours dans le fuseau du cabinet, bornes
-- incluses à la saisie (stockée [début, fin+1) par PostgreSQL). Aucune tâche
-- planifiée : l'expiration est un filtre sur la date locale du jour.
CREATE TABLE practitioner_delegation (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    practice_id     uuid NOT NULL REFERENCES practice (id),
    user_id         uuid NOT NULL REFERENCES app_user (id) ON DELETE CASCADE,
    practitioner_id uuid NOT NULL REFERENCES practitioner (id),
    valid           daterange NOT NULL CHECK (NOT isempty(valid) AND lower_inc(valid) AND NOT upper_inf(valid) AND NOT lower_inf(valid)),
    created_by      uuid REFERENCES app_user (id) ON DELETE SET NULL,
    created_at      timestamptz NOT NULL DEFAULT now(),
    EXCLUDE USING gist (user_id WITH =, practitioner_id WITH =, valid WITH &&)
);
CREATE INDEX practitioner_delegation_user_idx ON practitioner_delegation (user_id);
CREATE INDEX practitioner_delegation_practice_idx ON practitioner_delegation (practice_id, created_at DESC);

-- +goose Down
DROP TABLE practitioner_delegation;
