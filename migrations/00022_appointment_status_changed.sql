-- +goose Up
-- Pointage « Ma journée » (T072) : qui a changé le statut d'un RDV, et quand.
-- Renseignés par Calendar.SetStatus ; servent aux statistiques (T073).
ALTER TABLE appointment
    ADD COLUMN status_changed_by uuid REFERENCES app_user (id),
    ADD COLUMN status_changed_at timestamptz;

-- +goose Down
ALTER TABLE appointment
    DROP COLUMN status_changed_at,
    DROP COLUMN status_changed_by;
