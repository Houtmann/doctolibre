-- +goose Up
-- RGPD (T043) : durées de conservation par cabinet, anonymisation des patients,
-- minimisation des messages envoyés, journal d'audit rattaché au cabinet.
ALTER TABLE practice
    ADD COLUMN retention_outbox_days int NOT NULL DEFAULT 90
        CONSTRAINT practice_retention_outbox_days_chk CHECK (retention_outbox_days BETWEEN 1 AND 3650),
    ADD COLUMN retention_patient_years int NOT NULL DEFAULT 3
        CONSTRAINT practice_retention_patient_years_chk CHECK (retention_patient_years BETWEEN 1 AND 30),
    ADD COLUMN retention_audit_days int NOT NULL DEFAULT 365
        CONSTRAINT practice_retention_audit_days_chk CHECK (retention_audit_days BETWEEN 30 AND 3650),
    -- Purge automatique des patients inactifs : désactivée tant que le praticien n'a pas relu la prévisualisation.
    ADD COLUMN patient_purge_enabled boolean NOT NULL DEFAULT false,
    ADD COLUMN privacy_contact text NOT NULL DEFAULT ''
        CONSTRAINT practice_privacy_contact_len_chk CHECK (char_length(privacy_contact) <= 200);

-- Patient anonymisé : plus aucune coordonnée, donc le CHECK « téléphone ou email » ne s'applique plus.
ALTER TABLE patient ADD COLUMN anonymized_at timestamptz;
ALTER TABLE patient DROP CONSTRAINT patient_check;
ALTER TABLE patient ADD CONSTRAINT patient_contact_chk
    CHECK (anonymized_at IS NOT NULL OR phone IS NOT NULL OR email IS NOT NULL);

-- Message dont le contenu (payload, destinataire) a été effacé.
ALTER TABLE outbox_message ADD COLUMN scrubbed_at timestamptz;
CREATE INDEX outbox_message_scrub_idx ON outbox_message (practice_id, (COALESCE(sent_at, updated_at)))
    WHERE scrubbed_at IS NULL AND status IN ('sent', 'failed', 'cancelled');

-- Journal d'audit : cabinet (écran /admin/audit, purge) ; les lignes existantes
-- sont rattachées via l'utilisateur.
ALTER TABLE audit_log ADD COLUMN practice_id uuid REFERENCES practice (id) ON DELETE CASCADE;
UPDATE audit_log l SET practice_id = u.practice_id FROM app_user u WHERE u.id = l.user_id;
CREATE INDEX audit_log_practice_id_idx ON audit_log (practice_id, id DESC);
CREATE INDEX audit_log_practice_at_idx ON audit_log (practice_id, at);

-- +goose Down
DROP INDEX audit_log_practice_at_idx;
DROP INDEX audit_log_practice_id_idx;
ALTER TABLE audit_log DROP COLUMN practice_id;
DROP INDEX outbox_message_scrub_idx;
ALTER TABLE outbox_message DROP COLUMN scrubbed_at;
ALTER TABLE patient DROP CONSTRAINT patient_contact_chk;
ALTER TABLE patient ADD CONSTRAINT patient_check CHECK (phone IS NOT NULL OR email IS NOT NULL);
ALTER TABLE patient DROP COLUMN anonymized_at;
ALTER TABLE practice
    DROP COLUMN privacy_contact, DROP COLUMN patient_purge_enabled, DROP COLUMN retention_audit_days,
    DROP COLUMN retention_patient_years, DROP COLUMN retention_outbox_days;
