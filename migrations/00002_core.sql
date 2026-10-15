-- +goose Up
CREATE TABLE practice (
    id                 uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name               text NOT NULL,
    slug               text NOT NULL UNIQUE,
    address            text NOT NULL DEFAULT '',
    phone              text NOT NULL DEFAULT '',
    timezone           text NOT NULL DEFAULT 'Europe/Paris',
    min_booking_notice interval NOT NULL DEFAULT '2 hours',
    min_cancel_notice  interval NOT NULL DEFAULT '24 hours',
    booking_horizon    interval NOT NULL DEFAULT '60 days',
    created_at         timestamptz NOT NULL DEFAULT now(),
    updated_at         timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE practitioner (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    practice_id uuid NOT NULL REFERENCES practice (id),
    first_name  text NOT NULL,
    last_name   text NOT NULL,
    slug        text NOT NULL,
    title       text NOT NULL DEFAULT '',
    bio         text NOT NULL DEFAULT '',
    color       text NOT NULL DEFAULT '#2563eb',
    active      boolean NOT NULL DEFAULT true,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),
    UNIQUE (practice_id, slug)
);

CREATE TABLE app_user (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    practice_id     uuid NOT NULL REFERENCES practice (id),
    practitioner_id uuid REFERENCES practitioner (id),
    email           citext NOT NULL UNIQUE,
    password_hash   text NOT NULL,
    role            text NOT NULL CHECK (role IN ('admin', 'practitioner', 'secretary')),
    last_login_at   timestamptz,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX app_user_practice_idx ON app_user (practice_id);

-- id = sha256 (hex) du token de session, jamais le token lui-même.
CREATE TABLE session (
    id         text PRIMARY KEY,
    user_id    uuid NOT NULL REFERENCES app_user (id) ON DELETE CASCADE,
    expires_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    user_agent text NOT NULL DEFAULT '',
    ip         text NOT NULL DEFAULT ''
);
CREATE INDEX session_user_idx ON session (user_id);
CREATE INDEX session_expires_idx ON session (expires_at);

CREATE TABLE appointment_type (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    practice_id     uuid NOT NULL REFERENCES practice (id),
    name            text NOT NULL,
    duration        interval NOT NULL CHECK (duration > interval '0'),
    buffer_before   interval NOT NULL DEFAULT '0' CHECK (buffer_before >= interval '0'),
    buffer_after    interval NOT NULL DEFAULT '0' CHECK (buffer_after >= interval '0'),
    online_bookable boolean NOT NULL DEFAULT true,
    at_home         boolean NOT NULL DEFAULT false,
    color           text NOT NULL DEFAULT '#2563eb',
    position        integer NOT NULL DEFAULT 0,
    active          boolean NOT NULL DEFAULT true,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX appointment_type_practice_idx ON appointment_type (practice_id, position);

CREATE TABLE practitioner_appointment_type (
    practitioner_id     uuid NOT NULL REFERENCES practitioner (id) ON DELETE CASCADE,
    appointment_type_id uuid NOT NULL REFERENCES appointment_type (id) ON DELETE CASCADE,
    PRIMARY KEY (practitioner_id, appointment_type_id)
);

-- weekday : 0 = lundi ... 6 = dimanche.
-- valid_from et valid_until sont des dates INCLUSIVES (le moteur de créneaux
-- traite valid_until comme dernier jour d'application) ; valid_until NULL = sans fin.
-- appointment_type_ids NULL = tous les motifs.
CREATE TABLE availability_rule (
    id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    practitioner_id     uuid NOT NULL REFERENCES practitioner (id) ON DELETE CASCADE,
    weekday             smallint NOT NULL CHECK (weekday BETWEEN 0 AND 6),
    start_time          time NOT NULL,
    end_time            time NOT NULL,
    valid_from          date NOT NULL,
    valid_until         date,
    appointment_type_ids uuid[],
    created_at          timestamptz NOT NULL DEFAULT now(),
    updated_at          timestamptz NOT NULL DEFAULT now(),
    CHECK (end_time > start_time),
    CHECK (valid_until IS NULL OR valid_until >= valid_from)
);
CREATE INDEX availability_rule_practitioner_idx ON availability_rule (practitioner_id, weekday);

CREATE TABLE availability_exception (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    practitioner_id uuid NOT NULL REFERENCES practitioner (id) ON DELETE CASCADE,
    during          tstzrange NOT NULL CHECK (NOT isempty(during)),
    kind            text NOT NULL CHECK (kind IN ('closed', 'open')),
    reason          text NOT NULL DEFAULT '',
    created_at      timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX availability_exception_during_idx ON availability_exception USING gist (practitioner_id, during);

-- Aucune donnée médicale : notes_admin est une note administrative.
CREATE TABLE patient (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    practice_id  uuid NOT NULL REFERENCES practice (id),
    first_name   text NOT NULL,
    last_name    text NOT NULL,
    birth_date   date,
    phone        text,
    email        citext,
    sms_opt_in   boolean NOT NULL DEFAULT true,
    email_opt_in boolean NOT NULL DEFAULT true,
    notes_admin  text NOT NULL DEFAULT '',
    deleted_at   timestamptz,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),
    CHECK (phone IS NOT NULL OR email IS NOT NULL)
);
CREATE INDEX patient_practice_name_idx ON patient (practice_id, last_name, first_name, id);

-- during : durée réelle du soin. during_blocked : soin + buffers (calculé en Go
-- par domain.BlockedRange, pas de colonne générée : timestamptz - interval n'est
-- pas IMMUTABLE). Buffers copiés du motif à la création.
-- care_plan_id : la FK sera ajoutée avec la table care_plan (T030).
CREATE TABLE appointment (
    id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    practice_id         uuid NOT NULL REFERENCES practice (id),
    practitioner_id     uuid NOT NULL REFERENCES practitioner (id),
    patient_id          uuid NOT NULL REFERENCES patient (id),
    appointment_type_id uuid NOT NULL REFERENCES appointment_type (id),
    during              tstzrange NOT NULL,
    buffer_before       interval NOT NULL DEFAULT '0',
    buffer_after        interval NOT NULL DEFAULT '0',
    during_blocked      tstzrange NOT NULL,
    status              text NOT NULL DEFAULT 'booked' CHECK (status IN ('booked', 'cancelled', 'no_show', 'done')),
    source              text NOT NULL CHECK (source IN ('online', 'staff')),
    care_plan_id        uuid,
    cancelled_at        timestamptz,
    cancelled_by        text,
    cancel_reason       text,
    manage_token_hash   text UNIQUE,
    created_at          timestamptz NOT NULL DEFAULT now(),
    updated_at          timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT appointment_during_not_empty CHECK (NOT isempty(during)),
    CONSTRAINT appointment_blocked_contains_during CHECK (during_blocked @> during),
    CONSTRAINT appointment_no_overlap EXCLUDE USING gist (
        practitioner_id WITH =,
        during_blocked WITH &&
    ) WHERE (status <> 'cancelled')
);
CREATE INDEX appointment_practitioner_blocked_idx ON appointment (practitioner_id, lower(during_blocked));
CREATE INDEX appointment_patient_idx ON appointment (patient_id);
CREATE INDEX appointment_practice_start_idx ON appointment (practice_id, lower(during), id);

CREATE TABLE audit_log (
    id        bigserial PRIMARY KEY,
    at        timestamptz NOT NULL DEFAULT now(),
    user_id   uuid REFERENCES app_user (id) ON DELETE SET NULL,
    actor     text NOT NULL,
    action    text NOT NULL,
    entity    text NOT NULL,
    entity_id uuid,
    meta      jsonb NOT NULL DEFAULT '{}'
);
CREATE INDEX audit_log_entity_idx ON audit_log (entity, entity_id, at);

-- +goose Down
DROP TABLE audit_log;
DROP TABLE appointment;
DROP TABLE patient;
DROP TABLE availability_exception;
DROP TABLE availability_rule;
DROP TABLE practitioner_appointment_type;
DROP TABLE appointment_type;
DROP TABLE session;
DROP TABLE app_user;
DROP TABLE practitioner;
DROP TABLE practice;
