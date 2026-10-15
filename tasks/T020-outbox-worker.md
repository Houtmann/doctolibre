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
- [x] Rollback de la transaction métier → aucun message.
- [x] Deux workers concurrents n'envoient jamais deux fois le même message (test).
- [x] Erreur temporaire → retry planifié ; permanente → `failed` immédiat.
- [x] Arrêt gracieux : le worker termine l'envoi en cours.
- [x] `EXPLAIN` de la requête de la boucle worker : utilise `outbox_message_pending_run_at_idx` (noter le plan dans la PR).

## Vérification
```bash
make check
```

## Hors scope
Implémentations email/SMS (T021, T022), contenus (T023).

## Suivi

> Rempli par l'agent pendant et à la fin de chaque session sur cette tâche. Le statut officiel est dans `tasks/README.md`.

- **Sessions** : 2026-10-15 (sonnet)
- **Fait** : migration `00004_outbox.sql` (table + 3 index spec + index `(practice_id, created_at DESC, id)` pour la page admin) ; `internal/store/queries/outbox.sql` + sqlc ; `internal/outbox/outbox.go` (`Msg`, `Enqueue(ctx, tx, Msg)` avec `pg_notify` dans la tx et dedup via `ON CONFLICT DO NOTHING`, `CancelForAppointment(ctx, tx, clock, id)`, `Queue{Pool, Clock}` : `OutboxStats` = `metrics.OutboxSource`, `ListRecent`, `Resend`) ; `internal/outbox/worker.go` (`Worker.Run`/`RunOnce`, `Dispatcher`, `DispatcherFunc`, `Permanent`/`IsPermanent`, LISTEN sur connexion hijackée + polling de secours, claim `FOR UPDATE SKIP LOCKED` en tx courte, envoi hors tx, récupération des `sending` > 10 min, backoff 1m/5m/30m/2h/6h puis `failed` (6 tentatives), arrêt gracieux : envoi en cours terminé, messages réservés non envoyés rendus sans tentative comptée) ; tests `internal/outbox/outbox_test.go` ; `TestMigrateUpDownUp` adapté (la dernière migration est maintenant outbox_message, plus appointment).
- **Reste à faire** (hors périmètre de cette session, demandé explicitement) : câblage `serve.go` (goroutine worker sauf `DOCTOLIBRE_WORKER=off`, `metrics.RegisterOutbox(outbox.Queue{...})`), sous-commande `doctolibre worker`, page `/admin/messages` (50 derniers + bouton renvoyer ; le store est prêt : `Queue.ListRecent` / `Queue.Resend`).
- **EXPLAIN** (200 000 lignes dont 2 000 pending, après ANALYZE) de la requête de claim : `Index Scan using outbox_message_pending_run_at_idx` (`Index Cond: run_at <= now()`), `LockRows` -> `Limit 20`, puis jointure par `outbox_message_pkey` ; 0,46 ms. La récupération des bloqués utilise `outbox_message_sending_idx`.
- **Notes de reprise** : `Msg.RunAt` zéro = `now()` côté base (le claim compare à `Clock.Now()` : en test, avancer l'horloge au-delà de now() réel). `last_error` (tronqué à 500 octets) peut contenir un destinataire : il n'est jamais loggé. Les mises à jour de statut sont gardées par `status='sending' AND attempts=<claim>` (un worker lent ne peut pas écraser un message récupéré/re-réservé). `OutboxCountByStatus` fait un GROUP BY sans index : à surveiller si la table grossit (purge = T043).
- **Correctif revue (double envoi)** : juste avant chaque envoi, `TouchOutboxMessage` (garde `status='sending' AND attempts`) ; 0 ligne => message récupéré ailleurs, pas d'envoi, ID loggé. `RunOnce` refuse `StuckAfter < 2 x SendTimeout`. Tests `TestStaleClaimNotSentTwice` (limite connue : le message déjà en cours d'envoi chez A quand B le récupère peut être envoyé deux fois, seuls les messages en attente dans le lot sont protégés ; d'où la contrainte StuckAfter >= 2 x SendTimeout) et `TestInvalidTimeoutsRejected`.
- **Écarts avec la spécification** : `CancelForAppointment` prend un `domain.Clock` en plus (temps injecté pour `updated_at`). Serve/commande worker/page admin non faits (consigne de l'orchestrateur : ne pas toucher `internal/web` ni brancher `serve.go`).
