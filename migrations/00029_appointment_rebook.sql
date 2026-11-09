-- +goose Up
-- Reprise de rendez-vous après annulation (T098) : le RDV annulé pointe vers le RDV qui l'a
-- remplacé. Posé par un UPDATE conditionnel (rebooked_appointment_id IS NULL) dans la
-- transaction de création : un seul rebook par RDV annulé, même en concurrence. L'UNIQUE
-- empêche en outre qu'un même nouveau RDV serve deux fois.
ALTER TABLE appointment
    ADD COLUMN rebooked_appointment_id uuid REFERENCES appointment (id) ON DELETE SET NULL;
CREATE UNIQUE INDEX appointment_rebooked_appointment_uniq
    ON appointment (rebooked_appointment_id) WHERE rebooked_appointment_id IS NOT NULL;

-- +goose Down
DROP INDEX appointment_rebooked_appointment_uniq;
ALTER TABLE appointment DROP COLUMN rebooked_appointment_id;
