-- +goose Up
-- Auto-planification de la série par le patient (T071). Réglage du cabinet (désactivé par
-- défaut) et choix par série : le patient ne peut planifier que si les deux sont actifs.
ALTER TABLE practice
    ADD COLUMN care_plan_self_scheduling boolean NOT NULL DEFAULT false;
ALTER TABLE care_plan
    ADD COLUMN patient_self_schedule boolean NOT NULL DEFAULT false;

-- +goose Down
ALTER TABLE care_plan DROP COLUMN patient_self_schedule;
ALTER TABLE practice DROP COLUMN care_plan_self_scheduling;
