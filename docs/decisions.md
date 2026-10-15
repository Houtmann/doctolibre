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
- 2026-10-15 — `go.mod` en `go 1.26.0`, sqlc et goose déclarés en directive `tool` (`go tool sqlc`) — imposé par les versions récentes des outils, rien à installer globalement (T002) — épingler d'anciennes versions pour rester en 1.24.
- 2026-10-15 — CSRF en double-submit cookie `csrf_token` (HttpOnly, SameSite=Lax), jeton par header `X-CSRF-Token` (htmx via `hx-headers`) ou champ `csrf_token` (T003/T004) — indépendant des sessions — jeton stocké en session.
- 2026-10-15 — htmx 2.0.4 et Pico CSS 2.0.6 vendored (sha256 dans T004) ; `htmx-config` sans `allowEval` ni styles d'indicateur injectés — compatible CSP `default-src 'self'` — CDN.
- 2026-10-15 — Rendu : `View{CSRF, Flash, Data}` pour les pages, donnée brute pour les partials, layout choisi par commentaire `{{/* layout: admin */}}` en tête de page (T004) — convention simple et explicite — un template par layout.
- 2026-10-15 — `/metrics` désactivé par défaut, activé par `DOCTOLIBRE_METRICS_TOKEN` (Bearer) ; label de route = `r.Pattern` (middleware autour du mux) (T045) — cardinalité bornée, pas d'exposition publique — liste d'IP autorisées.
- 2026-10-15 — `domain.BlockedRange` est l'unique calcul de `during_blocked` ; `store.CreateAppointment` est le point unique d'insertion des RDV et traduit 23P01 (`appointment_no_overlap`) en `ErrSlotTaken` ; `availability_rule.valid_until` inclusif (T010/T013) — moteur et contrainte appliquent la même règle — calcul dupliqué en SQL.
- 2026-10-15 — Moteur de créneaux : les buffers peuvent déborder de la plage ouverte mais jamais empiéter sur un Busy ou une exception Closed ; une exception Open vaut pour tous les motifs (T013) — règle identique à la contrainte EXCLUDE — buffers contenus dans la plage.
- 2026-10-15 — Outbox : tentatives comptées au claim, backoff 1m/5m/30m/2h/6h puis `failed` ; mises à jour gardées par `(status='sending', attempts)` ; « touch » gardé juste avant chaque envoi et `StuckAfter >= 2 × SendTimeout` (T020) — évite le double envoi quand un lot est récupéré par un autre worker (bug trouvé en revue) — `updated_at` posé seulement au claim.
- 2026-10-15 — Auth : argon2id (m=64 MiB, t=1, p=2), sessions 12 h glissantes plafonnées à 7 jours, cookie `__Host-session` en prod (`session` en dev), mot de passe ≥ 12 caractères, rate limit login 5/15 min par IP et par email (T011) — recommandations OWASP, adapté à un petit cabinet — bcrypt / sessions sans plafond.
- 2026-10-15 — Travail parallèle : un worktree par tâche, merge `--no-ff` dans `main` par l'orchestrateur après contrôle ; conflits `go.mod`/`go.sum` résolus par union + `go mod tidy` — tâches indépendantes en parallèle sans se marcher dessus — tout en séquentiel.
