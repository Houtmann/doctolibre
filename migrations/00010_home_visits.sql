-- +goose Up
-- Visites à domicile (T032). Adresse du RDV et du patient (saisie libre, donnée
-- personnelle : jamais journalisée), temps de trajet par motif, zones acceptées
-- et activation de la réservation en ligne à domicile par cabinet.
ALTER TABLE appointment ADD COLUMN address text;
ALTER TABLE patient ADD COLUMN address text;
ALTER TABLE appointment_type
    ADD COLUMN travel_buffer interval NOT NULL DEFAULT '15 minutes'
    CONSTRAINT appointment_type_travel_buffer_nonneg CHECK (travel_buffer >= interval '0');
ALTER TABLE practice
    ADD COLUMN home_visit_online boolean NOT NULL DEFAULT false,
    ADD COLUMN home_visit_zones text[] NOT NULL DEFAULT '{}';

-- +goose Down
ALTER TABLE practice DROP COLUMN home_visit_zones, DROP COLUMN home_visit_online;
ALTER TABLE appointment_type DROP COLUMN travel_buffer;
ALTER TABLE patient DROP COLUMN address;
ALTER TABLE appointment DROP COLUMN address;
