# T011 — Auth praticien

- **Phase** : 1
- **Dépend de** : T004, T005, T010
- **Taille estimée** : M
- **Exécution** : sonnet (`task-implementer`) — revue Opus : renforcée

## Objectif
Le personnel du cabinet se connecte par email + mot de passe ; l'espace `/admin` est protégé.

## Contexte à lire
`internal/httpx/middleware.go`, `internal/web/routes.go`, `internal/web/render.go`, `internal/store/queries/app_user.sql`, migration `00002_core.sql` (tables `app_user`, `session`).

## Spécification
- `internal/auth/password.go` : argon2id (`golang.org/x/crypto/argon2`), format PHC encodé, `Hash`, `Verify` en temps constant, paramètres raisonnables (m=64MiB, t=1, p=2).
- `internal/auth/session.go` : token 32 octets aléatoires ; en base on stocke `sha256(token)` ; cookie `__Host-session` (`Secure` en prod, `HttpOnly`, `SameSite=Lax`), durée 12 h glissante, révocation au logout.
- Middleware `RequireUser(roles ...string)` qui charge l'utilisateur dans le contexte ; `auth.UserFrom(ctx)`.
- Routes : `GET/POST /admin/login`, `POST /admin/logout`, `GET /admin` (tableau de bord vide).
- Rate limit login : 5 essais / 15 min par IP **et** par email. Message d'erreur générique.
- Sous-commande `doctolibre create-user --email --role --practice-name` : crée le cabinet si absent, demande le mot de passe sur stdin (sans écho, `golang.org/x/term`).
- Audit : `login_success`, `login_failure`, `logout`.

## Critères d'acceptation
- [x] Accès `/admin` sans session → redirection `/admin/login?next=…` (next validé : chemin relatif uniquement).
- [x] Session expirée ou révoquée → déconnecté.
- [x] Tests : hash/verify, login OK/KO, rate limit, open-redirect refusé.

## Vérification
```bash
make test && ./bin/doctolibre create-user --email kine@example.test --role admin --practice-name "Cabinet test"
```

## Hors scope
Mot de passe oublié (à ajouter après T021), 2FA, gestion multi-utilisateurs dans l'UI (T033).

## Suivi

> Rempli par l'agent pendant et à la fin de chaque session sur cette tâche. Le statut officiel est dans `tasks/README.md`.

- **Sessions** : 2026-10-15 (sonnet).
- **Fait** : internal/auth (password argon2id PHC m=64MiB t=1 p=2, Service Login/Authenticate/Logout, cookie, RequireUser, SafeNext, CreateUser, Slugify) ; routes GET/POST /admin/login, POST /admin/logout, GET /admin (pages login et admin_dashboard, bouton déconnexion dans layout admin) ; rate limit 5/15 min par IP et par email ; audit login_success/login_failure/logout ; sous-commande create-user (x/term, repli sur 1re ligne de stdin si non-TTY) ; serve ouvre le pool et active /admin si DOCTOLIBRE_DATABASE_URL défini ; requêtes sqlc ExtendSession, DeleteSessionsByUser, GetPracticeByName ; CreateSession prend created_at ; tests auth + web.
- **Reste à faire** : rien (chemin TTY interactif de create-user non testé automatiquement).
- **Notes de reprise** : aucune migration ajoutée. Tests DB : DOCTOLIBRE_TEST_DATABASE_URL.
- **Ecarts avec la spécification** : cookie `session` en dev (HTTP) et `__Host-session` en prod, car le préfixe __Host- exige Secure ; durée absolue de 7 jours en plus de l expiration glissante 12 h ; mot de passe >= 12 caractères ; GetPracticeByName par nom exact ; ClientIP = RemoteAddr (derrière Caddy toutes les IP se confondent, voir rapport).
