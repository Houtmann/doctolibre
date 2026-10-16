-- +goose Up
-- Liste d'attente (T031) : un patient décrit les créneaux qui lui conviendraient ;
-- quand un créneau se libère, les premiers inscrits compatibles reçoivent une
-- proposition (waitlist_offer) valable quelques heures. Le premier qui clique la
-- réserve : la contrainte EXCLUDE de appointment tranche.
CREATE TABLE waitlist_entry (
    id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    practice_id         uuid NOT NULL REFERENCES practice (id),
    patient_id          uuid NOT NULL REFERENCES patient (id),
    -- NULL = n'importe quel praticien du cabinet.
    practitioner_id     uuid REFERENCES practitioner (id),
    appointment_type_id uuid NOT NULL REFERENCES appointment_type (id),
    earliest            date NOT NULL,
    latest              date NOT NULL,
    -- 0 = lundi (comme availability_rule.weekday) ; vide = tous les jours.
    weekdays            smallint[] NOT NULL DEFAULT '{}'
        CONSTRAINT waitlist_entry_weekdays_chk CHECK (weekdays <@ ARRAY[0, 1, 2, 3, 4, 5, 6]::smallint[]),
    -- Plage de début de séance souhaitée (heure murale du cabinet) ; NULL = toute la journée.
    time_from           time,
    time_to             time,
    status              text NOT NULL DEFAULT 'active'
        CONSTRAINT waitlist_entry_status_chk CHECK (status IN ('active', 'fulfilled', 'expired', 'cancelled')),
    -- Canal dont le contact a été vérifié par code (inscription en ligne) ; NULL = saisie par le cabinet.
    verified_channel    text
        CONSTRAINT waitlist_entry_verified_channel_chk CHECK (verified_channel IN ('email', 'sms')),
    source              text NOT NULL DEFAULT 'online'
        CONSTRAINT waitlist_entry_source_chk CHECK (source IN ('online', 'staff')),
    created_at          timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT waitlist_entry_dates_chk CHECK (latest >= earliest),
    CONSTRAINT waitlist_entry_times_chk CHECK ((time_from IS NULL AND time_to IS NULL) OR (time_from IS NOT NULL AND time_to IS NOT NULL AND time_from < time_to))
);
-- Recherche des candidats à un créneau libéré (FIFO) et file d'expiration.
CREATE INDEX waitlist_entry_active_idx ON waitlist_entry (practice_id, created_at, id) WHERE status = 'active';
CREATE INDEX waitlist_entry_patient_idx ON waitlist_entry (patient_id);

-- Proposition d'un créneau précis à une inscription. Le jeton (secret du lien /w/{jeton})
-- n'est jamais stocké en clair.
CREATE TABLE waitlist_offer (
    id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    entry_id            uuid NOT NULL REFERENCES waitlist_entry (id) ON DELETE CASCADE,
    practice_id         uuid NOT NULL REFERENCES practice (id),
    practitioner_id     uuid NOT NULL REFERENCES practitioner (id),
    slot_start          timestamptz NOT NULL,
    token_hash          text NOT NULL UNIQUE,
    expires_at          timestamptz NOT NULL,
    claimed_at          timestamptz,
    created_at          timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT waitlist_offer_once UNIQUE (entry_id, practitioner_id, slot_start)
);

-- +goose Down
DROP TABLE waitlist_offer;
DROP TABLE waitlist_entry;
