-- +goose Up
-- Codes de vérification (OTP) de la réservation en ligne. Le code et les
-- coordonnées du patient ne sont jamais stockés en clair : seuls des hachés
-- sha256 le sont. recipient_hash sert au rate limit « 3 codes / heure / numéro »,
-- binding_hash lie le code à un créneau et à des coordonnées précises.
CREATE TABLE otp_challenge (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    practice_id    uuid NOT NULL REFERENCES practice (id),
    channel        text NOT NULL CHECK (channel IN ('sms', 'email')),
    recipient_hash text NOT NULL,
    binding_hash   text NOT NULL,
    code_hash      text NOT NULL,
    attempts       smallint NOT NULL DEFAULT 0,
    expires_at     timestamptz NOT NULL,
    consumed_at    timestamptz,
    created_at     timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX otp_challenge_recipient_idx ON otp_challenge (recipient_hash, created_at);
CREATE INDEX otp_challenge_expires_idx ON otp_challenge (expires_at);

-- Recherche du patient à la réservation en ligne (téléphone ou email + nom).
CREATE INDEX patient_practice_phone_idx ON patient (practice_id, phone) WHERE phone IS NOT NULL AND deleted_at IS NULL;
CREATE INDEX patient_practice_email_idx ON patient (practice_id, email) WHERE email IS NOT NULL AND deleted_at IS NULL;

-- +goose Down
DROP INDEX patient_practice_email_idx;
DROP INDEX patient_practice_phone_idx;
DROP TABLE otp_challenge;
