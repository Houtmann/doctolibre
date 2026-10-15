# T021 — Email SMTP générique

- **Phase** : 2
- **Dépend de** : T020
- **Taille estimée** : M
- **Exécution** : sonnet (`task-implementer`) — revue Opus : standard

## Objectif
Envoyer des emails transactionnels via **n'importe quel serveur SMTP** (OVH, Gandi, Brevo, Gmail, Postfix local…), avec des templates texte + HTML.

## Contexte à lire
`internal/outbox/` (interface `Dispatcher`, type `Msg`), `internal/config/config.go`.

## Spécification
- `internal/notify/email.go` : `type EmailSender interface { Send(ctx, Email) (ref string, err error) }`, `Email{To, Subject, Text, HTML, ReplyTo, Headers}`.
- Implémentations :
  - `notify/smtp` : stdlib `net/smtp` ou `github.com/wneessen/go-mail` (choisir **une**, justifier dans `docs/decisions.md`). STARTTLS/TLS implicite, AUTH PLAIN/LOGIN, timeouts. Construction MIME multipart/alternative correcte, encodage des en-têtes UTF-8, `Message-ID`, `Date`, `List-Unsubscribe` facultatif.
  - `notify/logmail` : écrit l'email dans les logs + dans `./tmp/mails/*.eml` en dev.
- Config : `DOCTOLIBRE_EMAIL_DRIVER=smtp|log`, `SMTP_HOST`, `SMTP_PORT`, `SMTP_USER`, `SMTP_PASSWORD`, `SMTP_FROM`, `SMTP_TLS=starttls|tls|none`.
- Templates : `internal/notify/templates/email/<name>.{subject,txt,html}.tmpl` (`text/template` pour sujet/texte, `html/template` pour HTML), layout HTML commun minimal (tables, inline styles, pas d'images distantes).
- Branchement dans le `Dispatcher` de l'outbox pour `kind='email'`. Erreurs SMTP 5xx → `Permanent`, 4xx/réseau → temporaire.
- Bonus de la tâche : mot de passe oublié pour le personnel (`/admin/password/forgot` + lien signé 1 h).
- `docs/email.md` : configuration SPF/DKIM/DMARC expliquée simplement, exemples de config OVH / Brevo / Gmail.

## Critères d'acceptation
- [x] Test d'intégration avec un faux serveur SMTP (faux serveur en mémoire dans les tests ; Mailpit ajouté à `docker-compose.dev.yml`, UI sur :8025, non exécuté faute de Docker).
- [x] Accents corrects dans sujet et corps (test).
- [x] Mot de passe oublié fonctionnel de bout en bout (test handler : demande, message outbox, lien, nouveau mot de passe, login ; pas via Mailpit).

## Vérification
```bash
make db-up && make check   # puis ouvrir http://localhost:8025
```

## Hors scope
Contenus des confirmations/rappels (T023), API propriétaires (Sendgrid…) — SMTP suffit.

## Suivi

> Rempli par l'agent pendant et à la fin de chaque session sur cette tâche. Le statut officiel est dans `tasks/README.md`.

- **Sessions** : 2026-10-15 (sonnet).
- **Fait** : `internal/notify` (Email/EmailSender, MIME multipart/alternative QP UTF-8, SMTP net/smtp avec STARTTLS/TLS/none, AUTH PLAIN/LOGIN, timeouts, driver `log` en .eml, templates embarqués + layout HTML, `NewEmailDispatcher` pour l'outbox, `NewEmailSender(cfg, logger)`) ; config `internal/config/email.go` (bloc `Config.Email`) ; mot de passe oublié (migration 00021 `password_reset`, requêtes sqlc, `auth.RequestPasswordReset/ResetTokenValid/ResetPassword`, handlers `internal/web/password.go`, pages, lien sur le login) ; `docs/email.md` ; service `mailpit` dans `deploy/docker-compose.dev.yml` ; tests (faux SMTP, config, handlers).
- **Reste à faire** : câblage dans `cmd/doctolibre/serve.go` (orchestrateur / T024) : `notify.NewEmailSender(cfg.Email, logger)` puis `notify.NewEmailDispatcher(sender)` pour `kind='email'`. Purge périodique de `password_reset` (requête `DeleteExpiredPasswordResets` prête, non appelée).
- **Notes de reprise** : tests DB avec `DOCTOLIBRE_TEST_DATABASE_URL=postgres://hadrien@localhost:5432/doctolibre_test?sslmode=disable`. Migration numérotée 00021 pour éviter les collisions avec les migrations parallèles.
- **Écarts avec la spécification** : pas de sous-packages `notify/smtp` et `notify/logmail` (un seul package `notify`, évite un cycle d'imports sur le type `Email`) ; faux serveur SMTP en mémoire à la place de Mailpit pour les tests ; stdlib `net/smtp` choisie (aucune dépendance ajoutée) ; `TestMigrateUpDownUp` rendu indépendant de la dernière migration (comptage des tables) ; `render_test.go` : donnée de test pour la page `password_reset`.
