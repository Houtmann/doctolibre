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
- [x] OTP de liste d'attente : texte sans « rendez-vous confirmé » ; OTP de réservation inchangé (golden files).
- [x] `Join` enfile un récapitulatif dans la transaction, une seule fois ; rien enfilé si la transaction échoue (test service DB).
- [x] `GET /wl/{token}` affiche la confirmation sans modifier l'état ; `POST` désinscrit ; jeton inconnu ou rejoué → page neutre ; limite de débit appliquée (tests handler).
- [x] Jeton stocké hashé, comparaison en temps constant ; migration réversible (`Down`).
- [x] `make check` vert (dont `css-check` si un template change).

## Vérification
```bash
make sqlc && make css && make check && go test ./internal/service/ ./internal/web/ ./internal/notify/
```

## Hors scope
- Modifier les règles d'offre ou de réclamation (T031, T088).

## Suivi

> Rempli par l'agent pendant et à la fin de chaque session sur cette tâche. Le statut officiel est dans `tasks/README.md`.

- **Sessions** : 2026-10-25 (sonnet)
- **Fait** : migration 00026 (`waitlist_entry.unsubscribe_hash text UNIQUE`, Down réversible) ; jeton de désinscription dérivé HMAC (`ManageKey.WaitlistToken`, label `waitlist-token/v1`, comme T050/T055), haché sha256 stocké, comparaison en temps constant ; `insertWaitlistEntry` tire l'id et pose le haché (inscriptions en ligne et saisies cabinet) ; récapitulatif `waitlist_joined` (email + SMS) enfilé dans la transaction de `Waitlist.Join` (canal vérifié, réglage « confirmations » du cabinet) ; lien `/wl/{jeton}` dans l'email d'offre (pas dans le SMS d'offre : 1 segment GSM-7 à tenir) ; `OTPSender.SendOTP` reçoit `purpose` (booking | waitlist), template email `otp_waitlist` et SMS dédiés, OTP de réservation inchangé ; routes `GET/POST /wl/{token}` (limiteur « offer » par IP), page `public_waitlist_leave` (confirm / done / gone), GET en lecture seule, jeton inconnu / rejoué / inscription close = même page 404 ; tests service (DB), handler, templates ; goldens régénérés (seuls `waitlist_avec_lien.email.{html,txt}` changent + nouveaux `waitlist_joined_*`).
- **Reste à faire** : passage navigateur de `/wl/{jeton}` (non vu) ; envoi réel du récapitulatif vers Mailpit.
- **Notes de reprise** : requête nouvelle `GetWaitlistEntryByUnsubscribeHash` = lookup par index UNIQUE (`unsubscribe_hash`), pas de scan : EXPLAIN non nécessaire (table `waitlist_entry` modeste). Les inscriptions antérieures à 00026 ont un haché NULL : pas de lien de désinscription dans leurs messages d'offre.
- **Écarts avec la spécification** : (1) le « payload avec purpose » est réalisé par un paramètre `purpose` de `SendOTP` qui choisit le template `otp_waitlist` (au lieu d'un champ dans le payload de `otp`), pour ne pas casser les OTP déjà en file (templates en `missingkey=error`) ni les goldens de réservation ; (2) jeton dérivé HMAC plutôt que `crypto/rand`, pour que les messages d'offre retrouvent le lien sans le stocker ; (3) pas de lien de désinscription dans le SMS d'offre.
