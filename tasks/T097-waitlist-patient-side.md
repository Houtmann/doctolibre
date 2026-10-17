# T097 — Liste d'attente côté patient

- **Phase** : 5
- **Dépend de** : T093, T091
- **Taille estimée** : M (< 500)
- **Exécution** : sonnet (`task-implementer`) — revue Opus : renforcée (nouveau jeton, migration)

## Objectif
L'inscription en liste d'attente donne au patient un OTP au texte juste, un récapitulatif et un moyen de se désinscrire en ligne.

Origine : audit UX des parcours patients du 2026-10-17.

## Contexte à lire
- `internal/service/otp.go` (`OutboxOTPSender` ~l. 71, partagé réservation / liste d'attente), `internal/notify` (`otp.txt.tmpl`, SMS)
- `internal/service/waitlist.go` (`Join` ~l. 292-343, n'enfile rien), `internal/service/waitlist_offers.go` (message d'offre), `internal/service/manage_token.go` (convention de jeton T050)
- `internal/web/public_waitlist.go`, `internal/web/routes.go` (route existante `/w/{token}`), `internal/web/limits.go` (`httpx.Limiter`, T087)
- `internal/store/queries/waitlist.sql`, `migrations/` (dernier : 00024 ; prochain attendu 00025, à vérifier)

## Spécification
1. **Texte de l'OTP** — `otp.txt.tmpl` (et SMS) dit « confirmer votre rendez-vous » aussi pour la liste d'attente. Passer un objet (`purpose` : booking / waitlist) dans le payload de `OutboxOTPSender` et adapter le texte.
2. **Récapitulatif d'inscription** — `Waitlist.Join` enfile un message (outbox, dans la transaction) : motif, praticien, fenêtre souhaitée, lien de désinscription.
3. **Désinscription en ligne** — jeton de gestion de l'inscription (suivre la convention existante : `crypto/rand` stocké hashé, ou dérivation HMAC comme T050), route publique `GET/POST /wl/{token}` (nom aligné sur `/w/{token}`) avec confirmation avant POST ; lien aussi dans les messages d'offre. Migration goose nouvelle (vérifier le numéro dans `migrations/`), `make sqlc`. Rate limiting via `httpx.Limiter` (T087). Un seul agent à la fois sur sqlc / migrations.

## Critères d'acceptation
- [ ] OTP de liste d'attente : texte sans « rendez-vous confirmé » ; OTP de réservation inchangé (golden files).
- [ ] `Join` enfile un récapitulatif dans la transaction, une seule fois ; rien enfilé si la transaction échoue (test service DB).
- [ ] `GET /wl/{token}` affiche la confirmation sans modifier l'état ; `POST` désinscrit ; jeton inconnu ou rejoué → page neutre ; limite de débit appliquée (tests handler).
- [ ] Jeton stocké hashé, comparaison en temps constant ; migration réversible (`Down`).
- [ ] `make check` vert (dont `css-check` si un template change).

## Vérification
```bash
make sqlc && make css && make check && go test ./internal/service/ ./internal/web/ ./internal/notify/
```

## Hors scope
- Modifier les règles d'offre ou de réclamation (T031, T088).

## Suivi

> Rempli par l'agent pendant et à la fin de chaque session sur cette tâche. Le statut officiel est dans `tasks/README.md`.

- **Sessions** :
- **Fait** :
- **Reste à faire** :
- **Notes de reprise** :
- **Écarts avec la spécification** :
