# Décisions

Format : `AAAA-MM-JJ — décision — pourquoi — alternative écartée`.

- 2026-10-15 — Go stdlib net/http + pgx/sqlc + goose — SQL explicite, peu de dépendances, lisible par un LLM — ORM (GORM/ent).
- 2026-10-15 — Front html/template + htmx vendored, sans build JS — binaire unique, maintenance minimale — SPA (React/Svelte).
- 2026-10-15 — Pas de Redis en v1, file de jobs en Postgres (SKIP LOCKED) — un service de moins à héberger — Redis/asynq.
- 2026-10-15 — Anti double-réservation par contrainte EXCLUDE (btree_gist) — garantie en base, même en concurrence — verrou applicatif.
- 2026-10-15 — Patient sans compte : OTP + lien signé — moins de friction et de données — comptes patients.
- 2026-10-15 — Email via SMTP générique, SMS via interface multi-drivers (webhook HTTP générique, passerelle Android, OVH) — coût minimal, pas de lock-in — SDK propriétaire unique.
- 2026-10-15 — Licence AGPL-3.0 — un hébergeur qui modifie doit republier — MIT.
- 2026-10-15 — Par défaut : confirmation par email, rappel J-1 seul par SMS ; OVH recommandé comme fournisseur payant, passerelle Android pour le budget minimal (voir docs/sms.md) — ~25 € HT/mois au lieu de ~58 € pour un kiné solo — tout par SMS / Twilio par défaut.
- 2026-10-15 — Contrainte EXCLUDE sur `during_blocked` (soin + buffers) et `status <> 'cancelled'` — sinon deux réservations concurrentes peuvent manger un buffer, et un RDV `done` libérerait son créneau (review) — EXCLUDE sur `during` / `status = 'booked'`.
- 2026-10-15 — Buffers copiés sur le RDV, `during_blocked` calculé en Go (`domain.BlockedRange`) — `timestamptz - interval` n'est pas IMMUTABLE, colonne générée impossible — colonne générée.
- 2026-10-15 — Outbox : `appointment_id` FK pour annuler les rappels ; `dedup_key` réservé à l'idempotence — requête indexée et intégrité référentielle — `LIKE 'prefix%'` sur `dedup_key`.
- 2026-10-15 — Tests DB isolés par schéma (`search_path` via un pool par test) — simple, compatible `t.Parallel()` — `CREATE DATABASE … TEMPLATE`.
- 2026-10-15 — Suivi multi-session dans le dépôt : statut dans `tasks/README.md` (seule source de vérité), section « Suivi » par tâche, `docs/progress/STATUS.md` (réécrit) + `journal.md` (append-only) — les sessions LLM n'ont pas de mémoire partagée — outil externe (issues GitHub, Notion).
- 2026-10-15 — Opus orchestre et contrôle, Sonnet implémente (sous-agent `task-implementer`, commande `/run-task`) ; l'orchestrateur est le seul écrivain des fichiers de suivi ; revue renforcée pour T010, T011, T013, T014, T020, T043 — coût réduit sur l'exécution, contrôle par le modèle le plus fort, pas de conflit d'écriture en parallèle — tout en Opus / chaque agent met à jour le suivi.
