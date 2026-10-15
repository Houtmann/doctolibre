-- +goose Up
-- Réglages de notification par cabinet (T023). Canaux : email | sms | both | none.
-- Défauts (objectif coût, cf. docs/sms.md) : confirmation par email, rappel J-1 par SMS,
-- annulations/déplacements par email, rappel H-2 désactivé.
ALTER TABLE practice
    ADD COLUMN notify_confirmation_channel text NOT NULL DEFAULT 'email'
        CONSTRAINT practice_notify_confirmation_channel_chk CHECK (notify_confirmation_channel IN ('email', 'sms', 'both', 'none')),
    ADD COLUMN notify_reminder_channel text NOT NULL DEFAULT 'sms'
        CONSTRAINT practice_notify_reminder_channel_chk CHECK (notify_reminder_channel IN ('email', 'sms', 'both', 'none')),
    ADD COLUMN notify_change_channel text NOT NULL DEFAULT 'email'
        CONSTRAINT practice_notify_change_channel_chk CHECK (notify_change_channel IN ('email', 'sms', 'both', 'none')),
    ADD COLUMN reminder_day_before boolean NOT NULL DEFAULT true,
    ADD COLUMN reminder_day_before_hour smallint NOT NULL DEFAULT 18
        CONSTRAINT practice_reminder_day_before_hour_chk CHECK (reminder_day_before_hour BETWEEN 0 AND 23),
    ADD COLUMN reminder_two_hours boolean NOT NULL DEFAULT false,
    ADD COLUMN reminder_series boolean NOT NULL DEFAULT true,
    ADD COLUMN notify_signature text NOT NULL DEFAULT ''
        CONSTRAINT practice_notify_signature_len_chk CHECK (char_length(notify_signature) <= 200);

-- Canal dont le contact a été vérifié par code (réservation en ligne) ; NULL = RDV
-- saisi par le cabinet, tous les contacts du patient sont utilisables.
ALTER TABLE appointment
    ADD COLUMN verified_channel text
        CONSTRAINT appointment_verified_channel_chk CHECK (verified_channel IN ('email', 'sms'));

-- Rappels : « ce RDV a-t-il déjà son message ? » (la FK ON DELETE CASCADE en profite aussi).
CREATE INDEX outbox_message_appointment_idx ON outbox_message (appointment_id);

-- +goose Down
DROP INDEX outbox_message_appointment_idx;
ALTER TABLE appointment DROP COLUMN verified_channel;
ALTER TABLE practice
    DROP COLUMN notify_signature, DROP COLUMN reminder_series, DROP COLUMN reminder_two_hours,
    DROP COLUMN reminder_day_before_hour, DROP COLUMN reminder_day_before,
    DROP COLUMN notify_change_channel, DROP COLUMN notify_reminder_channel, DROP COLUMN notify_confirmation_channel;
