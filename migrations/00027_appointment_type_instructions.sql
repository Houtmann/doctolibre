-- +goose Up
-- Consignes pratiques par motif (T070) : texte brut, affiché au patient avant la réservation
-- et repris dans ses messages. Lues au moment de l'envoi, jamais copiées dans le rendez-vous.
ALTER TABLE appointment_type
    ADD COLUMN patient_instructions text NOT NULL DEFAULT '' CHECK (char_length(patient_instructions) <= 1000),
    ADD COLUMN instructions_ack_required boolean NOT NULL DEFAULT false;

-- +goose Down
ALTER TABLE appointment_type
    DROP COLUMN instructions_ack_required,
    DROP COLUMN patient_instructions;
