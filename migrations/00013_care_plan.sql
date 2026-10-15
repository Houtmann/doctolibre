-- +goose Up
-- Séries de séances kiné (T030). Aucune donnée médicale : ni pathologie, ni
-- ordonnance ; `label` est un libellé administratif libre (100 caractères).
-- preferred_weekdays : 0 = lundi ... 6 = dimanche (comme availability_rule).
-- preferred_time_* : fenêtre horaire souhaitée en heure murale du cabinet.
CREATE TABLE care_plan (
    id                   uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    practice_id          uuid NOT NULL REFERENCES practice (id),
    patient_id           uuid NOT NULL REFERENCES patient (id),
    practitioner_id      uuid NOT NULL REFERENCES practitioner (id),
    appointment_type_id  uuid NOT NULL REFERENCES appointment_type (id),
    label                text NOT NULL DEFAULT '' CHECK (char_length(label) <= 100),
    sessions_prescribed  int NOT NULL CHECK (sessions_prescribed BETWEEN 1 AND 100),
    frequency_per_week   smallint NOT NULL CHECK (frequency_per_week BETWEEN 1 AND 7),
    preferred_weekdays   smallint[] NOT NULL DEFAULT '{}'
        CHECK (preferred_weekdays <@ ARRAY[0, 1, 2, 3, 4, 5, 6]::smallint[]),
    preferred_time_start time,
    preferred_time_end   time,
    start_date           date NOT NULL,
    status               text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'completed', 'cancelled')),
    created_at           timestamptz NOT NULL DEFAULT now(),
    updated_at           timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT care_plan_time_window CHECK (
        (preferred_time_start IS NULL AND preferred_time_end IS NULL)
        OR (preferred_time_start IS NOT NULL AND preferred_time_end IS NOT NULL AND preferred_time_start < preferred_time_end)
    )
);
CREATE INDEX care_plan_patient_idx ON care_plan (practice_id, patient_id, created_at DESC);

ALTER TABLE appointment
    ADD CONSTRAINT appointment_care_plan_fk FOREIGN KEY (care_plan_id) REFERENCES care_plan (id);
CREATE INDEX appointment_care_plan_idx ON appointment (care_plan_id, lower(during)) WHERE care_plan_id IS NOT NULL;

-- +goose Down
DROP INDEX appointment_care_plan_idx;
ALTER TABLE appointment DROP CONSTRAINT appointment_care_plan_fk;
DROP TABLE care_plan;
