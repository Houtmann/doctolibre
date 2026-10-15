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
- [x] Chaque driver testé avec `httptest.Server` (requête construite correctement).
- [x] Tests segments : 160 car. GSM-7 = 1 segment, 161 = 2 ; un emoji force UCS-2.
- [x] Changer de fournisseur = changer l'env, rien d'autre.

## Vérification
```bash
make check
```

## Hors scope
Réception de SMS / réponses (« répondez OUI ») — éventuellement plus tard.

## Suivi

> Rempli par l'agent pendant et à la fin de chaque session sur cette tâche. Le statut officiel est dans `tasks/README.md`.

- **Sessions** : 2026-10-15 (sonnet)
- **Fait** : package leaf `internal/notify/sms` (SMS, Sender, StatusError, PermanentError, IsPermanent, Segments GSM-7/UCS-2, Transliterate, MaskPhone) ; drivers `logsms`, `httpgeneric`, `androidgateway`, `ovh` (chacun avec tests httptest) ; `internal/notify/sms.go` : alias SMS/SMSSender, `NewSMSSender(cfg, clock, logger)`, `SMSDispatch(sender, from, transliterate) outbox.DispatcherFunc`, `SMSRunAt(now, loc, isOTP)` (tests DST) ; `internal/config/sms.go` (bloc `Config.SMS`, validation par driver) ; `docs/sms.md` section Configuration + exemples Twilio/Brevo/Plivo + règles d'enfilement.
- **Reste à faire** : câbler dans `serve.go` (T024) : `NewSMSSender` puis `SMSDispatch` pour `kind=sms` ; enfilement (T023) avec `SMSRunAt` et payload `{"body","sms_opt_in","otp"}`. `.env.example` : T040 (variables listées dans docs/sms.md).
- **Notes de reprise** : les types vivent dans `notify/sms` (et non `notify`) pour éviter un cycle d'import avec les drivers ; `notify` expose des alias. L'opt-out est contrôlé à l'envoi via `sms_opt_in` du payload (copie faite à l'enfilement), sans requête DB ; opt-out => échec définitif (statut failed). `// Package notify` est déclaré dans `sms.go` : dédoublonner avec T021. Prix non modifiés (non vérifiables hors ligne). Exemples Twilio/Brevo/Plivo non testés contre les vraies API.
- **Écarts avec la spécification** : sous-packages sous `internal/notify/sms/` (pas `sms/` à la racine de notify) ; `.env.example` non touché (consigne orchestrateur) ; URL du driver http non templatée ; OVH horodate avec l'horloge injectée (pas /auth/time).
