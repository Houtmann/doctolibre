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
- [ ] Accès `/admin` sans session → redirection `/admin/login?next=…` (next validé : chemin relatif uniquement).
- [ ] Session expirée ou révoquée → déconnecté.
- [ ] Tests : hash/verify, login OK/KO, rate limit, open-redirect refusé.

## Vérification
```bash
make test && ./bin/doctolibre create-user --email kine@example.test --role admin --practice-name "Cabinet test"
```

## Hors scope
Mot de passe oublié (à ajouter après T021), 2FA, gestion multi-utilisateurs dans l'UI (T033).

## Suivi

> Rempli par l'agent pendant et à la fin de chaque session sur cette tâche. Le statut officiel est dans `tasks/README.md`.

- **Sessions** : —
- **Fait** : —
- **Reste à faire** : —
- **Notes de reprise** (pièges, état intermédiaire, commandes utiles) : —
- **Écarts avec la spécification** (et pourquoi) : —
