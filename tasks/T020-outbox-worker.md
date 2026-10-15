# T020 — Outbox et worker

- **Phase** : 2
- **Dépend de** : T010, T005
- **Taille estimée** : M
- **Exécution** : sonnet (`task-implementer`) — revue Opus : renforcée

## Objectif
Une file de messages fiable dans PostgreSQL (sans Redis) : un message enregistré dans la même transaction que le RDV est envoyé au moins une fois, avec retries.

## Contexte à lire
`AGENTS.md` (SQL, Transactions), `internal/store/db.go`, `cmd/doctolibre/main.go`.

## Spécification
Migration : 
```sql
outbox_message(
  id uuid pk, practice_id, kind text,            -- 'email' | 'sms'
  template text, payload jsonb, recipient text,
  appointment_id uuid null references appointment(id) on delete cascade,
  dedup_key text unique null,                    -- idempotence uniquement, ex: 'reminder-24h:<appointment_id>'
  run_at timestamptz not null default now(),
  status text check in ('pending','sending','sent','failed','cancelled'),
  attempts int default 0, last_error text, sent_at, provider_ref text,
  created_at, updated_at)
create index outbox_message_pending_run_at_idx on outbox_message (run_at) where status = 'pending';
create index outbox_message_appointment_pending_idx on outbox_message (appointment_id) where status = 'pending';
create index outbox_message_sending_idx on outbox_message (updated_at) where status = 'sending';  -- crash recovery
```
`dedup_key` sert **uniquement** à l'idempotence (unicité), jamais à retrouver les messages d'un RDV : on n'écrit pas de `LIKE 'prefix%'` dessus.
- `internal/outbox/outbox.go` : `Enqueue(ctx, tx pgx.Tx, Msg) error` (dans la transaction métier), `CancelForAppointment(ctx, tx, appointmentID)` : `UPDATE outbox_message SET status='cancelled' WHERE appointment_id = $1 AND status = 'pending'` (annuler les rappels d'un RDV annulé ou déplacé).
- `internal/outbox/worker.go` : boucle `SELECT … WHERE status='pending' AND run_at <= now() ORDER BY run_at LIMIT 20 FOR UPDATE SKIP LOCKED`, passage `sending`, envoi **hors transaction**, puis `sent` ou retry avec backoff exponentiel (1 min, 5 min, 30 min, 2 h, 6 h → `failed`). Réveil par `LISTEN/NOTIFY` + polling 30 s de secours.
  Messages restés `sending` > 10 min → repassés `pending` (crash recovery).
- `Dispatcher` interface : `Send(ctx, Msg) (providerRef string, err error)` ; erreurs `Permanent` (pas de retry) vs temporaires.
- Démarré par `serve` (goroutine) sauf si `DOCTOLIBRE_WORKER=off` ; sous-commande `doctolibre worker` pour le lancer seul.
- Page `/admin/messages` : 50 derniers messages, statut, erreur, bouton « renvoyer ».

## Critères d'acceptation
- [ ] Rollback de la transaction métier → aucun message.
- [ ] Deux workers concurrents n'envoient jamais deux fois le même message (test).
- [ ] Erreur temporaire → retry planifié ; permanente → `failed` immédiat.
- [ ] Arrêt gracieux : le worker termine l'envoi en cours.
- [ ] `EXPLAIN` de la requête de la boucle worker : utilise `outbox_message_pending_run_at_idx` (noter le plan dans la PR).

## Vérification
```bash
make check
```

## Hors scope
Implémentations email/SMS (T021, T022), contenus (T023).

## Suivi

> Rempli par l'agent pendant et à la fin de chaque session sur cette tâche. Le statut officiel est dans `tasks/README.md`.

- **Sessions** : —
- **Fait** : —
- **Reste à faire** : —
- **Notes de reprise** (pièges, état intermédiaire, commandes utiles) : —
- **Écarts avec la spécification** (et pourquoi) : —
