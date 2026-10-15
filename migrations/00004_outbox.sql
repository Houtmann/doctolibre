-- +goose Up
-- File de messages (email/SMS) écrite dans la transaction métier.
-- dedup_key : idempotence uniquement (unicité), jamais utilisée pour retrouver
-- les messages d'un RDV (on passe par appointment_id).
CREATE TABLE outbox_message (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    practice_id    uuid NOT NULL REFERENCES practice (id),
    kind           text NOT NULL CHECK (kind IN ('email', 'sms')),
    template       text NOT NULL,
    payload        jsonb NOT NULL DEFAULT '{}',
    recipient      text NOT NULL,
    appointment_id uuid REFERENCES appointment (id) ON DELETE CASCADE,
    dedup_key      text UNIQUE,
    run_at         timestamptz NOT NULL DEFAULT now(),
    status         text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'sending', 'sent', 'failed', 'cancelled')),
    attempts       integer NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    last_error     text,
    sent_at        timestamptz,
    provider_ref   text,
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX outbox_message_pending_run_at_idx ON outbox_message (run_at) WHERE status = 'pending';
CREATE INDEX outbox_message_appointment_pending_idx ON outbox_message (appointment_id) WHERE status = 'pending';
-- Récupération des messages restés 'sending' après un crash.
CREATE INDEX outbox_message_sending_idx ON outbox_message (updated_at) WHERE status = 'sending';
-- Page admin « derniers messages » (pagination par cabinet).
CREATE INDEX outbox_message_practice_created_idx ON outbox_message (practice_id, created_at DESC, id);

-- +goose Down
DROP TABLE outbox_message;
