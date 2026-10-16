# AGENTS.md — conventions pour humains et LLM

À lire avant toute modification. Court exprès : si une règle manque, l'ajouter ici.

## Rôles : Opus orchestre, Sonnet exécute

| Rôle | Modèle | Fait | Ne fait pas |
|---|---|---|---|
| **Orchestrateur** | Opus (session principale, commande `/run-task`) | Choisit la tâche, précise la spec si besoin, délègue, **contrôle** (relance `make check`, lit le diff à risque, vérifie les critères), renvoie les corrections, tient le suivi (`tasks/README.md`, `STATUS.md`, `journal.md`, `decisions.md`), propose le commit | N'écrit pas le code applicatif |
| **Exécutant** | Sonnet (sous-agent [`task-implementer`](.claude/agents/task-implementer.md)) | Implémente **une** tâche, écrit les tests, lance les vérifications, remplit la section **Suivi** de sa tâche, rend un rapport | Ne touche pas aux fichiers de suivi global ni à `PLAN.md` / `AGENTS.md`, ne committe pas |

- Lancer une tâche : `/run-task` (prochaine tâche prête) ou `/run-task T013`.
- Tâches indépendantes en parallèle : un `task-implementer` par tâche, chacun dans son worktree ; l'orchestrateur est le **seul écrivain** des fichiers de suivi.
- Le champ **Exécution** de chaque tâche indique le modèle et le niveau de revue (`standard` ou `renforcée`).
- Corrections après revue : même sous-agent (via `SendMessage`), 2 allers-retours max, puis `blocked` et question à l'humain.
- Session sans orchestrateur (un humain ou un seul agent) : suivre le workflow ci-dessous en jouant les deux rôles.

## Workflow de session

Le projet avance sur **plusieurs sessions**, souvent sans mémoire entre elles : tout ce qui doit survivre est écrit dans le dépôt.
Index de la doc : [`docs/README.md`](docs/README.md).

### Début de session
1. Lire [`docs/progress/STATUS.md`](docs/progress/STATUS.md) (où on en est, prochaine action, blocages).
2. S'il y a une tâche `in_progress` : la reprendre en lisant d'abord sa section **Suivi**. Sinon, prendre la première tâche `todo` de [`tasks/README.md`](tasks/README.md) dont toutes les dépendances sont `done`.
3. Passer son statut à `in_progress` dans `tasks/README.md`.
4. Lire **uniquement** : ce fichier, la tâche, les fichiers listés dans « Contexte à lire ».

### Pendant
- Rester dans le périmètre de la tâche. Pas de refactoring opportuniste, pas de nouvelle dépendance non prévue.
- Toute décision non triviale non prévue → une ligne datée dans [`docs/decisions.md`](docs/decisions.md) (quoi / pourquoi / alternative écartée).
- Si la session risque de s'arrêter (contexte long, blocage), mettre à jour la section **Suivi** de la tâche **avant**, pas après.

### Fin de session (obligatoire, même si la tâche n'est pas finie)

En mode orchestré, l'exécutant fait l'étape 1, l'orchestrateur les étapes 2 à 5.

1. **Section Suivi de la tâche** : ce qui est fait, ce qui reste, notes de reprise (état intermédiaire, pièges, commandes), écarts avec la spec.
2. **Statut** dans `tasks/README.md` :
   | Statut | Signification |
   |---|---|
   | `todo` | Pas commencée |
   | `in_progress` | Commencée, la section Suivi dit où reprendre |
   | `blocked` | Bloquée ; la raison est dans Suivi **et** dans STATUS.md (« Blocages ») |
   | `review` | Implémentée, `make check` vert, en attente de relecture humaine |
   | `done` | Relue / mergée, critères d'acceptation cochés |
   | `superseded` | Remplacée par une autre tâche, nommée dans Suivi ; ne pas l'implémenter |
3. **[`docs/progress/STATUS.md`](docs/progress/STATUS.md)** : réécrire (date, en cours, prochaine action, blocages, compteurs par phase).
4. **[`docs/progress/journal.md`](docs/progress/journal.md)** : ajouter une entrée **en haut** (modèle dans le fichier).
5. `make check` vert avant `review`. Un commit par tâche (ou par session si la tâche est longue : `T014 (wip): …`).

## Commandes

```bash
make db-up      # Postgres (+ Mailpit) de dev
make migrate    # applique les migrations
make sqlc       # régénère internal/store/sqlcgen après modif de queries/*.sql ou migrations
make run        # lance le serveur en dev (:8080)
make test       # tous les tests (tests DB skippés si Postgres absent)
make tools      # télécharge Tailwind (sha256 épinglé) dans bin/
make css        # régénère internal/web/static/ui.css (à commiter)
make check      # fmt + vet + lint + test + css-check — doit être vert
```

## Architecture et dépendances entre packages

```
cmd/doctolibre → web → service → store, booking, notify, outbox, auth
                                  booking : pur (aucun import interne sauf domain)
                                  domain  : types et erreurs, n'importe rien d'interne
                                  auth → service autorisé (seed à la création du cabinet) ; service n'importe jamais auth
```
- `web` (handlers) : parse la requête, appelle **un** service, rend un template. Aucun SQL, aucune logique métier.
- `service` : règles métier, transactions, émission d'événements. Reçoit ses dépendances par struct (pas de globals).
- `store` : SQL via sqlc uniquement. Pas de SQL construit par concaténation.
- `booking` : calculs purs, testables sans DB.

## Go

- Go ≥ 1.24, stdlib d'abord. Dépendances autorisées : `pgx/v5`, `goose`, `sqlc` (tool), `google/uuid`, `golang.org/x/{crypto,time,term}`, `prometheus/client_golang`. Toute autre → justifier dans `docs/decisions.md`.
- Erreurs : `fmt.Errorf("context: %w", err)` ; erreurs métier sentinelles dans `domain` (`ErrSlotTaken`, `ErrNotFound`, `ErrForbidden`, `ErrValidation{Field, Msg}`). Les handlers mappent les erreurs → statut HTTP en un seul endroit.
- `context.Context` en premier argument partout où il y a de l'I/O.
- Pas de `init()`, pas de variable globale mutable, pas de `panic` hors `main`.
- Temps : jamais `time.Now()` dans le métier → injecter un `domain.Clock`.
- Logs : `slog` avec attributs ; **jamais** de nom, téléphone, email, token ou contenu de message patient dans les logs. Logger les IDs.

## SQL / PostgreSQL

- Migrations goose en SQL pur, `-- +goose Up` / `-- +goose Down`, jamais modifiées une fois mergées : on en ajoute une nouvelle.
- `uuid` en PK, `timestamptz` partout (jamais `timestamp`), plages horaires en `tstzrange` `[)`.
- Les invariants vivent en base quand c'est possible (contraintes `CHECK`, `UNIQUE`, `EXCLUDE`) ; l'app traduit les erreurs.
- Toute requête de liste filtre par `practice_id` et est paginée (keyset de préférence).
- Nouvelle requête sur une table potentiellement grosse (`appointment`, `outbox_message`, `audit_log`) → vérifier l'index avec `EXPLAIN` et le mentionner dans la PR.
- Transactions : `store.WithTx(ctx, pool, func(q *sqlcgen.Queries, tx pgx.Tx) error)`. Les messages outbox sont insérés **dans** la transaction métier ; les appels réseau (SMTP, SMS) **jamais** dans une transaction.

## Temps et fuseaux

- Stockage en UTC (`timestamptz`), calculs de créneaux dans le fuseau du cabinet (`practice.timezone`).
- Construire une heure murale avec `time.Date(y, m, d, h, min, 0, 0, loc)` ; ne jamais ajouter `24*time.Hour` pour passer au jour suivant (DST) → `AddDate(0, 0, 1)`.
- Tout code touchant aux créneaux a des tests sur les dates de changement d'heure.

## Front

- `html/template` + htmx (vendored dans `internal/web/static`) + **Tailwind CSS v4 (exécutable autonome, sans Node) + daisyUI 5**. Pas de Node, pas de bundler, pas de framework JS.
- Style : classes utilitaires Tailwind + composants daisyUI (`btn`, `input`, `alert`, `table`, `badge`, `menu`, `modal`, `drawer`…). **Les couleurs ne vivent que dans les blocs de thème d'`internal/web/input.css`** (`doctolibre` clair, `doctolibre-dark`) ; ailleurs, on utilise les jetons daisyUI (`bg-base-100`, `text-primary`…) ou `color-mix` sur des variables, jamais d'hex.
- `internal/web/static/ui.css` est **généré et commité** : lancer **`make css` après toute modification d'un template, de `static/app.js` ou d'`input.css`** (`make tools` une fois pour obtenir `bin/tailwindcss`). `make check` et la CI vérifient qu'il est à jour (`make css-check`) ; `go build` n'a pas besoin du binaire.
- **Aucune classe construite dynamiquement** : pas de `"btn-" + x`, pas de `class="alert-{{.Kind}}"`. Écrire les noms complets (`{{if eq .Kind "error"}}alert-error{{else}}…`), sinon Tailwind ne les voit pas et `ui.css` ne les contient pas.
- Chaque formulaire **fonctionne sans JS** (POST classique + redirect) ; htmx est une amélioration (`hx-post`, `hx-target`, `hx-swap`). Le handler détecte `HX-Request` pour renvoyer un partial au lieu de la page.
- Réutiliser les partials et modèles de [`docs/design-system.md`](docs/design-system.md) ; page de contrôle `/dev/styleguide` en dev.
- Pas de script ni de style inline (CSP inchangée : `default-src 'self'`, donc pas d'image `data:` non plus). Pas de ressource externe (CDN, polices Google…) : tout est embarqué. `static/app.js` est chargé dans `<head>` sans `defer` (il pose `data-theme` avant le rendu).
- Templates : `pages/` (une par route), `partials/` (fragments réutilisables et réponses htmx), `layout/`. Noms de fichiers en snake_case.
- Mobile-first, accessible : `<label>` sur chaque champ, `<button>` pour les actions, contrastes AA, `lang="fr"`.
- Textes en français, centralisés dans les templates (pas d'i18n en v1, mais pas de texte en dur dans le Go hors messages d'erreur métier).

## Sécurité

- CSRF sur toutes les routes non-GET. Cookies `HttpOnly`, `Secure` (prod), `SameSite=Lax`.
- Tokens (session, lien patient, OTP, ICS) : générés avec `crypto/rand`, **stockés hashés** (sha256), comparés en temps constant.
- Rate limiting sur login, OTP, réservation.
- Pas de données médicales dans le modèle. Champ libre côté cabinet = « note administrative », avertissement dans l'UI.

## Tests

- Table-driven, `t.Parallel()` quand possible.
- Tests DB : `db := testutil.DB(t)` (base isolée, skip si Postgres absent) + fixtures `testutil.New*`.
- Handlers : `httptest` sur le handler complet (`web.Routes(deps)`), en vérifiant statut + fragments HTML clés, pas le HTML entier.
- Golden files dans `testdata/` pour ICS et emails (`-update` pour régénérer).
- Un bug corrigé = un test de non-régression.

## Commits / PR

- Un commit par tâche si possible : `T014: réservation en ligne patient`.
- Description de PR : lien vers la tâche, critères cochés, sortie de `make check`, `EXPLAIN` si requête nouvelle, captures pour l'UI.
