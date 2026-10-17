-- +goose Up
-- Ressources partagées (T060) : box, appareils. Capacité 1, au plus une ressource par motif.
CREATE TABLE resource (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    practice_id uuid        NOT NULL REFERENCES practice (id),
    name        text        NOT NULL CHECK (name <> '' AND char_length(name) <= 60),
    active      boolean     NOT NULL DEFAULT true,
    created_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX resource_practice_idx ON resource (practice_id, name, id);

-- PK sur le motif : une seule ressource par motif en v1.
CREATE TABLE appointment_type_resource (
    appointment_type_id uuid PRIMARY KEY REFERENCES appointment_type (id) ON DELETE CASCADE,
    resource_id         uuid NOT NULL REFERENCES resource (id)
);
CREATE INDEX appointment_type_resource_resource_idx ON appointment_type_resource (resource_id);

-- during_blocked = celui du RDV (buffers inclus). active = false quand le RDV est annulé.
-- L'index GiST de l'EXCLUDE sert aussi la requête d'occupation (resource_id, during_blocked).
CREATE TABLE appointment_resource (
    appointment_id uuid        NOT NULL PRIMARY KEY REFERENCES appointment (id) ON DELETE CASCADE,
    resource_id    uuid        NOT NULL REFERENCES resource (id),
    during_blocked tstzrange   NOT NULL,
    active         boolean     NOT NULL DEFAULT true,
    CONSTRAINT appointment_resource_no_overlap EXCLUDE USING gist (
        resource_id WITH =,
        during_blocked WITH &&
    ) WHERE (active)
);

-- Une seule synchronisation pour tous les chemins d'écriture (réservation en ligne,
-- staff, report, séries, liste d'attente, annulation) : la ressource du motif (si active)
-- est réservée à l'insertion du RDV, suit son déplacement et est libérée à l'annulation.
-- La violation de l'EXCLUDE remonte comme celle d'appointment_no_overlap (SQLSTATE 23P01).
-- +goose StatementBegin
CREATE FUNCTION appointment_sync_resource() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'INSERT' THEN
        INSERT INTO appointment_resource (appointment_id, resource_id, during_blocked, active)
        SELECT NEW.id, r.id, NEW.during_blocked, NEW.status <> 'cancelled'
        FROM appointment_type_resource atr
        JOIN resource r ON r.id = atr.resource_id
        WHERE atr.appointment_type_id = NEW.appointment_type_id
          AND r.active AND r.practice_id = NEW.practice_id;
    ELSE
        UPDATE appointment_resource
        SET during_blocked = NEW.during_blocked, active = (NEW.status <> 'cancelled')
        WHERE appointment_id = NEW.id;
    END IF;
    RETURN NULL;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER appointment_resource_insert AFTER INSERT ON appointment
    FOR EACH ROW EXECUTE FUNCTION appointment_sync_resource();
CREATE TRIGGER appointment_resource_update AFTER UPDATE ON appointment
    FOR EACH ROW
    WHEN (OLD.during_blocked IS DISTINCT FROM NEW.during_blocked OR OLD.status IS DISTINCT FROM NEW.status)
    EXECUTE FUNCTION appointment_sync_resource();

-- +goose Down
DROP TRIGGER appointment_resource_update ON appointment;
DROP TRIGGER appointment_resource_insert ON appointment;
DROP FUNCTION appointment_sync_resource();
DROP TABLE appointment_resource;
DROP TABLE appointment_type_resource;
DROP TABLE resource;
