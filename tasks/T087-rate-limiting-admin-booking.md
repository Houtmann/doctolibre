# T087 — Rate limiting du back-office et de la prise de RDV

- **Phase** : 5
- **Dépend de** : T014, T033, T046
- **Taille estimée** : M (< 500)
- **Exécution** : sonnet (`task-implementer`) — revue Opus : renforcée (sécurité, concurrence)

## Objectif
Toutes les routes `/admin/*` et toutes les routes publiques de réservation et de liste d'attente passent par un limiteur ; les limites sont partagées par parcours (pas un seau par route), la mémoire du limiteur est bornée, et chaque refus est visible (429 propre, métrique).

## État actuel (constaté le 2026-10-17)
- `httpx.RateLimit` (`internal/httpx/middleware.go`) : seau `x/time/rate` par clé, en mémoire, éviction des clés inactives > 10 min ; `Retry-After: 1` fixe ; map **non bornée** ; clé = IP complète (une IPv6 tournante dans son /64 = clés infinies).
- Limité aujourd'hui : login (IP + email), mot de passe oublié, invitations (`team.go`), parcours public (`public.go`, `public_waitlist.go`) par IP, OTP par destinataire en base (`ErrOTPRateLimited`, `OTPMaxAttempts`).
- Trous :
  - **Aucune limite sur les routes authentifiées** `/admin/*` (import CSV, exports, JSON de l'agenda, recherche patients, renvoi de messages…) ni sur les requêtes portant un cookie de session invalide (chacune = une requête DB dans `RequireUser`).
  - Public : chaque appel à `limit()` crée un seau distinct → `POST …/details` et `POST …/confirm` ont chacun leur quota (le quota effectif est la somme) ; `GET /p/{practice}` et `GET /p/{practice}/{practitioner}` ne sont pas limités.
  - Rien ne borne le volume de réservations en ligne **par praticien** : un attaquant réparti sur beaucoup d'IP (et d'adresses email jetables) peut vider un agenda.

## Contexte à lire
- `internal/httpx/middleware.go` (`ClientIP`, `RateLimit`, `rateLimit`, `Error`) et ses tests
- `internal/web/public.go` (`publicRoutes`, constantes `publicBurst`, `publicInterval`), `internal/web/public_waitlist.go`
- `internal/web/admin.go` (`adminRoutes`), `internal/web/routes.go` (ou équivalent : où `require` est construit et passé aux `*Routes`)
- `internal/auth/session.go` (`RequireUser`, `UserFrom`)
- `internal/metrics/metrics.go`

## Spécification

### 1. `httpx` : limiteur durci
Remplacer `RateLimit(key, r, burst)` par un limiteur nommé et réutilisable :

```go
type Limiter struct { /* nom, r, burst, map bornée, now */ }
func NewLimiter(name string, key func(*http.Request) string, r rate.Limit, burst int) *Limiter
func (l *Limiter) Middleware() Middleware          // un même *Limiter = un même seau, partagé entre routes
func (l *Limiter) Allow(r *http.Request) (ok bool, retryAfter time.Duration)
```
- **Partage** : un `*Limiter` appliqué à plusieurs routes partage ses seaux (c'est le but : un quota par parcours).
- **Clé IP** : nouvelle fonction `httpx.IPKey(r)` = `ClientIP` pour IPv4, **préfixe /64** pour IPv6 (IPv4 mappée → IPv4). Les limiteurs par IP utilisent `IPKey`.
- **Mémoire bornée** : au plus `maxVisitors` clés (constante, 50 000). Map pleine après balayage → la requête utilise un seau de débordement unique `"overflow"` (même `r`/`burst`) et un `Warn` est loggé au plus une fois par minute (nom du limiteur, sans IP).
- **Retry-After** : calculé depuis le limiteur (`Reserve`/`Tokens` → délai avant le prochain jeton, arrondi à la seconde supérieure, min 1), plus `1` fixe.
- **Réponse 429** : via `httpx.Error` ; vérifier qu'une requête `HX-Request` reçoit un fragment (alerte daisyUI « Trop de requêtes, réessayez dans N s. ») et non une page complète — le corriger dans `Error` si ce n'est pas le cas.
- **Métrique** : compteur `doctolibre_rate_limited_total{limiter="<nom>"}` dans `internal/metrics` ; le limiteur reçoit un hook optionnel (`func(name string)`) pour ne pas faire importer `metrics` par `httpx`. Ni IP ni clé en label.
- Migrer les appelants existants (login, mot de passe, invitations, public, liste d'attente) sans changer leurs quotas.

### 2. Back-office (`/admin/*`)
Deux limiteurs, câblés là où `require` est construit, pour que **toutes** les routes `require(...)` / `admin(...)` en héritent sans les lister une à une :
- `admin-ip` (par `IPKey`, avant `RequireUser`) : 20 req/s, rafale 200 — coupe le sondage de jetons de session et le flood non authentifié sans gêner un cabinet derrière une seule IP.
- `admin-user` (par `auth.UserFrom(ctx).ID`, après `RequireUser`) : 5 req/s, rafale 100.
- `admin-heavy` (par utilisateur) en plus, sur les routes coûteuses : import CSV (aperçu + import), exports (RGPD, CSV), renvoi de message, endpoint JSON de l'agenda : 1 req / 2 s, rafale 10. Lister dans le Suivi les routes retenues (grep `mux.Handle` dans `internal/web`).
- Les routes de connexion gardent leurs limiteurs actuels.

### 3. Prise de RDV et liste d'attente (public)
- `public-browse` (par IP) : `GET /p/{practice}`, `GET /p/{practice}/{practitioner}` — 2 req/s, rafale 30.
- `public-search` (par IP) : grille, premier créneau, formulaire de détails, grille de déplacement — quota actuel (`publicInterval`, `publicBurst`), **un seul seau** pour toutes ces routes.
- `public-book` (par IP) : `POST …/details`, `POST …/confirm`, `POST` de la liste d'attente — **un seul seau partagé**, quota actuel.
- `public-manage` (par IP) : routes `/r/{token}/…` — inchangé, seau partagé (déjà le cas).
- `public-book-practitioner` (clé = `{practice}/{practitioner}` des path values) sur `POST …/confirm` et sur la confirmation de déplacement : **30 réservations / heure, rafale 30** par praticien, constante `maxOnlineBookingsPerHour`. Au-delà : 429 avec un message invitant à appeler le cabinet ; `Warn` loggé (IDs/slug du praticien, pas de données patient) une fois par heure et par praticien.
- Ne pas consommer de jeton `public-book-practitioner` quand la confirmation échoue en validation (OTP faux) : appliquer ce limiteur **dans le handler** via `Allow` juste avant l'appel au service de réservation, pas en middleware.

### 4. Tests
- `httpx` : seau partagé entre deux routes ; `IPKey` (IPv4, IPv6 → /64, IPv4 mappée) ; borne `maxVisitors` (avec une petite valeur injectée en test) et seau de débordement ; `Retry-After` > 1 quand le délai le justifie (horloge injectée) ; fragment htmx sur 429 ; hook métrique appelé.
- `web` (handlers complets `web.Routes(deps)`) : un utilisateur connecté qui dépasse `admin-user` reçoit 429, un autre utilisateur non ; `POST details` puis `POST confirm` décomptent le même seau ; 31ᵉ confirmation en une heure sur un praticien → 429, l'autre praticien non affecté ; OTP faux ne consomme pas le quota praticien.

## Critères d'acceptation
- [x] Aucune route `/admin/*` ni publique de réservation / liste d'attente n'échappe à un limiteur (liste des routes et de leur limiteur dans le Suivi).
- [x] Les POST de réservation partagent un seau par IP ; les GET de recherche aussi.
- [x] Quota de réservations en ligne par praticien et par heure, sans consommer sur échec de validation.
- [x] Clé IPv6 au /64, map bornée avec seau de débordement.
- [x] `Retry-After` calculé ; 429 en fragment pour htmx.
- [x] `doctolibre_rate_limited_total{limiter}` exposé sur `/metrics`.
- [x] Quotas existants (login, mot de passe, invitations, OTP) inchangés.
- [x] Tests ci-dessus ; `make check` vert.

## Vérification
```bash
make check
go test ./internal/httpx/ ./internal/web/ -run 'RateLimit|Limiter|IPKey' -count=1
```

## Hors scope
- Limiteur partagé entre instances (Redis, table Postgres) : v1 = une instance, en mémoire.
- Plafond de RDV futurs par contact patient (téléphone/email) : règle métier à trancher (incompatible avec l'auto-planification de série T071 si trop bas) → tâche séparée si voulu.
- Quotas configurables par variable d'environnement : constantes en v1 (T086 modifie `internal/config` en ce moment ; ne pas y toucher).
- Captcha, blocage d'IP persistant, liste noire.

## Suivi

> Rempli par l'agent pendant et à la fin de chaque session sur cette tâche. Le statut officiel est dans `tasks/README.md`.

- **Sessions** : 2026-10-17 (sonnet)
- **Fait** : tout le périmètre.
  - `internal/httpx/limiter.go` : `Limiter` (`NewLimiter`, `Middleware`, `Allow`, `AllowKey`, `Reserve`/`Cancel`, `Deny`, `OnLimit`, `Reject`, `Clock`, `Logger`), `IPKey` (/64 IPv6, IPv4 mappée), map bornée à 50 000 clés avec seau de débordement séparé (jamais une clé « overflow » réelle), balayage périodique + balayage forcé (max 1/s) quand la map est pleine, évince aussi les seaux de nouveau pleins (sans perte), `Retry-After` calculé, `RetrySeconds`. Ancien `RateLimit` supprimé.
  - `httpx.Error` : 429 + HX-Request = fragment `alert alert-warning` ; `Renderer.Error` délègue pour ce cas ; `app.js` : `htmx:beforeSwap` laisse insérer un 429 (htmx n'insère pas les 4xx par défaut). `make css` fait.
  - Métrique `doctolibre_rate_limited_total{limiter}` (`Metrics.RateLimited`).
  - `auth.Service.WithLimits(pre, post)` (copie du service) : `RequireUser` applique `pre` (IP) avant l'authentification et `post` (utilisateur) juste après. `adminRoutes` fait `d = d.withAdminLimits(...)` avant tout `*Routes` (Deps est passé par valeur) : aucune route `/admin/*` protégée n'est listée, aucun fichier de T056 touché.
  - `internal/web/limits.go` : constantes de quotas, `heavyAdminRoutes` (clé = `r.Pattern`, une seule liste), `publicLimits`, `reservePractitioner`, `emailKey`.
  - Migration des limiteurs existants sans changer les quotas : `login-ip`, `login-email`, `password-ip`, `password-email`, `invite-ip`.
- **Routes et limiteurs** :
  - `/admin/*` protégées (tout `RequireUser`) : `admin-ip` (20/s, rafale 200, IPKey, avant auth) puis `admin-user` (5/s, rafale 100, par user ID).
  - `admin-heavy` (1 req/2 s, rafale 10, par user, en plus ; seau partagé entre les routes) : `POST /admin/patients/import` (aperçu + import), `GET /admin/patients/{id}/export.json`, `GET /admin/patients/{id}/export.csv`, `POST /admin/patients/{id}/delete`, `GET /admin/agenda/events`, `POST /admin/messages/{id}/resend`.
  - Login/mot de passe/invitation (inchangés) : `POST /admin/login` (login-ip + login-email), `POST /admin/password/forgot` (password-ip + password-email), `POST` reset (password-ip), `GET`/`POST` acceptation d'invitation (invite-ip). `GET` du formulaire de login / forgot / reset : statiques, non limités (comme avant).
  - `public-browse` (2/s, rafale 30) : `GET /p/{practice}`, `GET /p/{practice}/{practitioner}`.
  - `public-search` (6 s/jeton, rafale 10) : `GET …/first`, `GET …/book`, `GET …/book/details`, `GET /p/…/waitlist`, `GET /r/{token}/reschedule`, `GET /r/{token}/reschedule/confirm`.
  - `public-book` (idem) : `POST …/book/details`, `POST …/book/confirm`, `POST …/waitlist`, `POST …/waitlist/confirm`.
  - `public-manage` (idem) : `GET /r/{token}`, `/ics`, `/cancel` (GET/POST), `POST /r/{token}/reschedule`.
  - `public-offer` (idem) : `GET`/`POST /w/{token}`.
  - `public-book-practitioner` (30/h, rafale 30, clé = ID du praticien, dans le handler) : `POST …/book/confirm` et `POST /r/{token}/reschedule`.
- **Reste à faire** : rien pour la tâche. Non vu au navigateur : l'affichage du 429 htmx (fragment inséré via `htmx:beforeSwap`).
- **Notes de reprise** : tests déterministes grâce à `Deps.LimiterNow` (horloge figée dans `newAdmin`, `newPublicEnv`, `limits_test.go`) ; sans cela le régime établi se régénère pendant des requêtes lentes. Les POST de réservation d'une même IP partagent un seau de 10 : `TestPublicBookingForgedSlotRefused` utilise désormais une IP par créneau forgé.
- **Écarts avec la spécification** (et pourquoi) :
  1. La clé du quota praticien est l'ID du praticien (après résolution du slug, donc 404 pour un slug inconnu) et non les path values `{practice}/{practitioner}` : même seau pour la réservation et le déplacement, et aucune clé forgée par l'attaquant.
  2. `Cancel` du jeton praticien sur toute erreur du service (OTP faux, validation, créneau pris), pas seulement sur échec de validation : seules les réservations abouties comptent. Le jeton est pris avant `Book` (l'OTP est vérifié dans le service), puis rendu.
  3. Les GET de la grille de déplacement (`/r/{token}/reschedule`, `…/confirm`) vont dans `public-search` (spec : « grille de déplacement »), le reste de `/r/…` dans `public-manage`. Les liens `/w/{token}` ont leur propre seau `public-offer` (quota inchangé, pas de mélange avec `/r/`). Le formulaire GET de la liste d'attente est dans `public-search`, ses POST dans `public-book`.
  4. `admin-heavy` : liste de routes dans `heavyAdminRoutes` (par `r.Pattern`) au lieu d'envelopper chaque handler ; ajouté `POST …/delete` (effacement RGPD).
  5. Hors périmètre non limités : `GET /ics/{file}` (flux ICS à jeton), `GET /p/{practice}/privacy`, `GET /healthz`, `GET /metrics` (jeton).
  6. Le 429 du limiteur public est rendu par la page d'erreur stylée (non-htmx) ; le quota praticien rend le formulaire avec un message (déplacement : message flash + redirection).
