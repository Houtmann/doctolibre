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
- [ ] Test d'intégration avec un faux serveur SMTP (Mailpit dans `docker-compose.dev.yml`, UI sur :8025).
- [ ] Accents corrects dans sujet et corps (test).
- [ ] Mot de passe oublié fonctionnel de bout en bout en dev.

## Vérification
```bash
make db-up && make check   # puis ouvrir http://localhost:8025
```

## Hors scope
Contenus des confirmations/rappels (T023), API propriétaires (Sendgrid…) — SMTP suffit.

## Suivi

> Rempli par l'agent pendant et à la fin de chaque session sur cette tâche. Le statut officiel est dans `tasks/README.md`.

- **Sessions** : —
- **Fait** : —
- **Reste à faire** : —
- **Notes de reprise** (pièges, état intermédiaire, commandes utiles) : —
- **Écarts avec la spécification** (et pourquoi) : —
