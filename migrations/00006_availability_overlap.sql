-- +goose Up
-- Deux plages d'un même praticien ne se chevauchent pas le même jour de semaine
-- tant que leurs périodes de validité (dates inclusives) se recoupent.
-- La plage horaire est projetée sur un jour fictif pour utiliser un tsrange ;
-- date + time est IMMUTABLE, donc utilisable dans une contrainte.
ALTER TABLE availability_rule ADD CONSTRAINT availability_rule_no_overlap EXCLUDE USING gist (
    practitioner_id WITH =,
    weekday WITH =,
    tsrange(date '2000-01-01' + start_time, date '2000-01-01' + end_time, '[)') WITH &&,
    daterange(valid_from, valid_until, '[]') WITH &&
);

-- +goose Down
ALTER TABLE availability_rule DROP CONSTRAINT availability_rule_no_overlap;
