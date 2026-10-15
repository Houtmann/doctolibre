# T022 — SMS : interface et fournisseurs

- **Phase** : 2
- **Dépend de** : T020
- **Taille estimée** : M
- **Exécution** : sonnet (`task-implementer`) — revue Opus : standard

## Objectif
Envoyer des SMS via un backend **interchangeable par configuration**, en privilégiant les options les moins chères pour un praticien.

## Contexte à lire
`internal/outbox/` (interface `Dispatcher`), `internal/notify/email.go` (pour la symétrie), `internal/domain/phone.go`, `docs/sms.md`.

## Spécification
- `internal/notify/sms.go` : `type SMSSender interface { Send(ctx, SMS) (ref string, err error) }`, `SMS{To /* E.164 */, Body, Sender}`.
  Helpers : comptage de segments GSM-7 / UCS-2, translittération optionnelle des caractères hors GSM-7 (ex. `œ`→`oe`) pour rester en GSM-7 et payer 1 segment.
- Implémentations (un sous-package chacune, < 150 lignes) :
  1. `sms/logsms` : dev.
  2. `sms/httpgeneric` : **webhook configurable** — méthode, URL, headers, body en `text/template` (`{{.To}}`, `{{.Body}}`), auth basic/bearer. Couvre la plupart des API (Brevo, Twilio, SMSFactor, Free Mobile…) sans code dédié.
  3. `sms/androidgateway` : app open-source « SMS Gateway for Android » (capcom6) — mode local (IP du téléphone sur le LAN) ou cloud. Coût = forfait mobile.
  4. `sms/ovh` : API OVH SMS (signature OVH), fournisseur français courant.
- Config : `DOCTOLIBRE_SMS_DRIVER=log|http|android|ovh` + variables propres à chaque driver ; documenter dans `.env.example`.
- Branchement `Dispatcher` pour `kind='sms'`. Respect de `patient.sms_opt_in`. Pas d'envoi entre 20 h et 8 h (heure du cabinet) sauf OTP → `run_at` décalé.
- `docs/sms.md` **existe déjà** (comparatif des coûts, réglages par défaut, pièges de facturation) : le compléter avec une section « Configuration » par driver et des exemples de config `httpgeneric` pour Twilio, Brevo et Plivo. Mettre à jour les prix si l'écart dépasse 10 %.

## Critères d'acceptation
- [ ] Chaque driver testé avec `httptest.Server` (requête construite correctement).
- [ ] Tests segments : 160 car. GSM-7 = 1 segment, 161 = 2 ; un emoji force UCS-2.
- [ ] Changer de fournisseur = changer l'env, rien d'autre.

## Vérification
```bash
make check
```

## Hors scope
Réception de SMS / réponses (« répondez OUI ») — éventuellement plus tard.

## Suivi

> Rempli par l'agent pendant et à la fin de chaque session sur cette tâche. Le statut officiel est dans `tasks/README.md`.

- **Sessions** : —
- **Fait** : —
- **Reste à faire** : —
- **Notes de reprise** (pièges, état intermédiaire, commandes utiles) : —
- **Écarts avec la spécification** (et pourquoi) : —
