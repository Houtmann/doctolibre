# T010 — Schéma de base v1

- **Phase** : 1
- **Dépend de** : T002
- **Taille estimée** : M
- **Exécution** : sonnet (`task-implementer`) — revue Opus : renforcée

## Objectif
Toutes les tables du cœur métier existent, avec les contraintes qui garantissent les invariants (en particulier : pas de double réservation).

## Contexte à lire
`PLAN.md` §3, `AGENTS.md` (section SQL), `migrations/00001_init.sql`, `sqlc.yaml`.

## Spécification
Migration `00002_core.sql`. Conventions : PK `uuid` (`gen_random_uuid()`), `created_at`/`updated_at timestamptz not null default now()`, noms au singulier en snake_case.

- `practice(id, name, slug unique, address, phone, timezone text not null default 'Europe/Paris', min_booking_notice interval default '2 hours', min_cancel_notice interval default '24 hours', booking_horizon interval default '60 days')`
- `practitioner(id, practice_id fk, first_name, last_name, slug, title, bio, color, active bool, unique(practice_id, slug))`
- `app_user(id, practice_id, practitioner_id null fk, email citext unique, password_hash, role text check in ('admin','practitioner','secretary'), last_login_at)`
- `session(id text pk /* hash du token */, user_id fk on delete cascade, expires_at, created_at, user_agent, ip)`
- `appointment_type(id, practice_id, name, duration interval not null, buffer_before interval default '0', buffer_after interval default '0', online_bookable bool, at_home bool default false, color, position int, active bool)`
- `practitioner_appointment_type(practitioner_id, appointment_type_id, pk(both))`
- `availability_rule(id, practitioner_id, weekday smallint check 0..6 /* 0 = lundi */, start_time time, end_time time check (end_time > start_time), valid_from date, valid_until date null, appointment_type_ids uuid[] null /* null = tous */)`
- `availability_exception(id, practitioner_id, during tstzrange not null, kind text check in ('closed','open'), reason text)`
- `patient(id, practice_id, first_name, last_name, birth_date null, phone text null, email citext null, sms_opt_in bool default true, email_opt_in bool default true, notes_admin text /* PAS de données médicales */, deleted_at null, check (phone is not null or email is not null))`
- `appointment(id, practice_id, practitioner_id, patient_id, appointment_type_id, during tstzrange not null, buffer_before interval not null default '0', buffer_after interval not null default '0', during_blocked tstzrange not null, status text check in ('booked','cancelled','no_show','done'), source text check in ('online','staff'), care_plan_id null, cancelled_at, cancelled_by text, cancel_reason, manage_token_hash text unique)`
  - `during` = durée réelle du soin (affichage, ICS, SMS). `during_blocked` = `[lower(during) - buffer_before, upper(during) + buffer_after)` = temps réellement bloqué dans l'agenda.
  - Les buffers sont **copiés depuis le motif à la création** : modifier un motif ne change pas les RDV existants.
  - `during_blocked` est calculé côté Go par **une seule fonction** `domain.BlockedRange(start, end, before, after)` utilisée par le store. Pas de colonne générée : `timestamptz - interval` n'est pas `IMMUTABLE` dans Postgres. Garde-fou : `CHECK (during_blocked @> during)`.
  - **Contrainte** : `EXCLUDE USING gist (practitioner_id WITH =, during_blocked WITH &&) WHERE (status <> 'cancelled')`. Elle porte sur la zone bloquée (sinon deux réservations concurrentes peuvent écraser un buffer) et couvre aussi `done` / `no_show` (un RDV honoré occupe toujours son créneau).
  - Index : `(practitioner_id, lower(during_blocked))`, `(patient_id)`.
- `audit_log(id bigserial, at, user_id null, actor text, action text, entity text, entity_id uuid, meta jsonb)`

Requêtes sqlc de base dans `internal/store/queries/*.sql` (un fichier par table) : Create/Get/List/Update nécessaires aux tâches T011–T017 — rester minimal, les tâches suivantes en ajouteront.

`internal/testutil/fixtures.go` : `NewPractice(t, db)`, `NewPractitioner(t, db, practiceID)`, `NewPatient(...)`, `NewAppointmentType(...)` avec valeurs par défaut sensées.

## Critères d'acceptation
- [x] Migration up/down/up idempotente.
- [x] Test : insérer deux RDV `booked` qui se chevauchent pour un même praticien échoue avec l'erreur d'exclusion (code `23P01`) ; OK si l'un est `cancelled`, OK pour deux praticiens différents ; OK pour deux RDV adjacents sans buffer (`[)`).
- [x] Test buffers : A = 10:00–10:30 avec `buffer_after` 15 min, B = 10:30–11:00 → **rejeté** ; B = 10:45–11:15 → accepté.
- [x] Test statuts : un RDV `done` ou `no_show` bloque toujours son créneau.
- [x] `internal/store/errors.go` : `IsExclusionViolation(err) bool`, `IsUniqueViolation(err) bool`.

## Vérification
```bash
make migrate && make sqlc && make test
```

## Hors scope
Séries (T030), liste d'attente (T031), outbox (T020) : leurs tables viennent avec leur tâche.

## Suivi

> Rempli par l'agent pendant et à la fin de chaque session sur cette tâche. Le statut officiel est dans `tasks/README.md`.

- **Sessions** : 2026-10-15 (sonnet)
- **Fait** : migration `migrations/00002_core.sql` (toutes les tables, EXCLUDE `appointment_no_overlap`, CHECK `during_blocked @> during` et `NOT isempty(during)`, index `(practitioner_id, lower(during_blocked))`, `(patient_id)`, `(practice_id, lower(during), id)`) ; requêtes sqlc (1 fichier par table dans `internal/store/queries`) ; `internal/domain/errors.go` (ErrSlotTaken, ErrNotFound, ErrForbidden, ErrValidation) ; `internal/store/{errors,tx,appointment}.go` (IsExclusionViolation, IsUniqueViolation, WithTx, CreateAppointment qui calcule during_blocked via domain.BlockedRange et traduit 23P01 en ErrSlotTaken) ; fixtures testutil.New* ; `internal/store/schema_test.go` (up/down/up, chevauchements, adjacence, praticiens distincts, cancelled, done/no_show, buffers, CHECK, course à 8 goroutines = 1 succès, rollback WithTx). `make check` vert (Postgres.app 17).
- **Reste à faire** : rien.
- **EXPLAIN** (1 praticien, 8761 RDV) : agenda `during_blocked && range` + `status <> cancelled` -> Bitmap Index Scan sur `appointment_no_overlap` (gist, praticien + range) puis tri ; par token -> Index Scan `appointment_manage_token_hash_key` ; liste patient -> Index Scan Backward `appointment_practice_start_idx` + filtre patient (jeu de test dégénéré à 1 patient ; avec beaucoup de patients le planner prendra `appointment_patient_idx`).
- **Notes de reprise** : tests DB avec `DOCTOLIBRE_TEST_DATABASE_URL=postgres://hadrien@localhost:5432/doctolibre_test?sslmode=disable`. Ne pas utiliser `time.Now()` : les timestamps passent en paramètres (ex. `GetSession` prend `now`).
- **Écarts avec la spécification** : `availability_rule.valid_from` NOT NULL sans défaut ; `valid_until` inclusif (commentaire SQL) ; `care_plan_id` sans FK (table créée en T030) ; chaînes vides par défaut plutôt que NULL pour les textes optionnels (sauf patient.phone/email, cancelled_by, cancel_reason, manage_token_hash) ; `CHECK` supplémentaires (durée > 0, buffers >= 0, valid_until >= valid_from, during d exception non vide) ; index supplémentaires (session, app_user, availability_exception gist, patient nom, audit_log) ; les erreurs domain n existaient pas : créées.
