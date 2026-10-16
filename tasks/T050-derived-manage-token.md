# T050 — Jeton de gestion dérivé (HMAC)

- **Phase** : 5
- **Dépend de** : T023, T030
- **Taille estimée** : M
- **Exécution** : sonnet (`task-implementer`) — revue Opus : renforcée

## Objectif
Chaque RDV a un lien de gestion `/r/{token}` recalculable côté serveur. Les rappels J-1 et les annulations par le cabinet contiennent ce lien, y compris pour les séances de série.

## Contexte à lire
`internal/service/booking.go` (`newManageToken`, `ManageTokenHash`, création ~l. 443-506), `internal/service/calendar.go` (l. ~591 et ~642, création staff), `internal/service/manage.go` (`lockByToken`, `RescheduleByPatient`), `internal/service/reminders.go` (l. ~213, `n.data(v, "")`), `internal/service/notifications.go` (`data`, `cancelled` l. ~279, `rescheduled`), `internal/service/care_plans.go` (création des séances), `internal/service/waitlist.go` (l. ~542) et `waitlist_offers.go` (l. ~135), `internal/service/privacy.go` + `internal/store/queries/privacy.sql` (l. ~77), `internal/service/patients.go` (`Merge`), `internal/config/config.go` (`SecretKey`, `minSecretLen`, `Validate`), `internal/web/templates/pages/public_confirmed.html`, `migrations/00002_core.sql` (l. 148), `docs/install.md`, `deploy/.env.example`.

## Spécification
- **Dérivation** : `jeton = base64url(HMAC-SHA256(k, appointment_id || ":" || token_version))`, avec `k` = clé dérivée de `Config.SecretKey` et du label `"manage-token/v1"` (HMAC de ce label avec `SecretKey`). Fonction pure et testable (ex. `service.DeriveManageToken(key []byte, id uuid.UUID, version int) string`).
- **Migration 00018** : `appointment.manage_token_version int NOT NULL DEFAULT 1`. `manage_token_hash` reste = sha256 hex du jeton (`ManageTokenHash`) : les requêtes `GetAppointmentByManageToken` / `LockAppointmentByManageToken` ne changent pas. Comparaison en temps constant partout où l'on compare un jeton dérivé à un haché stocké.
- **Nouveaux RDV** (en ligne, staff, séances de série, RDV créés depuis une offre de liste d'attente) : jeton dérivé dès la création, haché stocké à l'insertion. Les séances de série (`care_plans.go`) n'ont aujourd'hui aucun jeton : elles en obtiennent un.
- **RDV existants** : le hash n'est pas touché (les liens déjà envoyés restent valides). Pour eux, le rappel inclut le lien seulement si `sha256(dérivé(id, version)) == manage_token_hash`, sinon pas de lien (comportement actuel).
- **Révocation** : la seule révocation existante (effacement RGPD, hash mis à NULL) est conservée telle quelle. L'annulation (patient ou cabinet) ne révoque **pas** le lien : la page du RDV annulé doit rester accessible (voir plus bas). La fusion de fiches ne touche pas aux jetons. `manage_token_version` sert de mécanisme de révocation futur : fournir une requête/fonction `RevokeManageToken` (version+1, hash réécrit) testée, sans l'appeler ailleurs. Le report (`RescheduleByPatient`) conserve le même jeton : ne rien changer.
- **Identifiant connu avant l'insertion** : le jeton dépend de `appointment.id` ; générer l'UUID côté Go (`uuid.New()`) et le passer à l'INSERT partout où un RDV est créé (vérifier les requêtes sqlc qui s'appuient sur le `DEFAULT gen_random_uuid()`).
- **SecretKey obligatoire** au démarrage dans tous les environnements (≥ 32 octets) : durcir `Config.Validate` (aujourd'obligatoire seulement en `prod`) et adapter les tests/config de dev. Documenter dans `docs/install.md` et `deploy/.env.example` que **changer la clé invalide tous les liens déjà envoyés**.
- **Rappels** (`reminders.go`) et **annulation cabinet** (`notifications.go`, `cancelled`) passent `manage_url` à `n.data`. Les templates de rappel ont déjà `{{if .manage_url}}` ; vérifier celui de l'annulation et l'ajouter.
- **Page du lien pour un RDV annulé** : `public_confirmed.html` n'affiche rien pour reprendre RDV quand `Cancelled` (ni bouton, ni lien). Ajouter un lien « Reprendre rendez-vous » vers la page publique du praticien (`/p/{practice}/{practitioner}`, cf. `practitionerView`).
- Décision à ajouter dans `docs/decisions.md` (jeton dérivé vs jeton aléatoire stocké ; conséquence de la rotation de clé).

## Critères d'acceptation
- [x] Un RDV créé (en ligne, staff, série, offre d'attente) est gérable par son lien dérivé.
- [x] Le rappel J-1 d'un RDV neuf contient `manage_url` ; celui d'un ancien RDV sans correspondance de hash n'en contient pas.
- [x] L'email/SMS d'annulation par le cabinet contient le lien ; la page du lien d'un RDV annulé renvoie vers la réservation du praticien.
- [x] Démarrage refusé sans `SecretKey` valide (message clair), quel que soit l'environnement.
- [x] Tests : dérivation stable ; changement de version → jeton différent ; séance de série gérable par son lien ; `RevokeManageToken` invalide l'ancien lien ; lien d'un RDV annulé toujours lisible ; effacement RGPD invalide le lien ; clé absente → erreur de config.
- [x] Décision ajoutée dans `docs/decisions.md`.

## Vérification
```bash
make sqlc
make check
```

## Hors scope
Lien de série pour le patient (T055). Régénération des jetons des RDV existants.

## Suivi

> Rempli par l'agent pendant et à la fin de chaque session sur cette tâche. Le statut officiel est dans `tasks/README.md`.

- **Sessions** : 2026-10-16 (sonnet)
- **Fait** : migration 00018 (`manage_token_version`) ; `internal/service/manage_token.go` (`ManageKey`, `NewManageKey`, `DeriveManageToken`, `mint`, `forAppointment` en temps constant, `Booking.RevokeManageToken` non branché) ; requête `SetManageToken` ; `CreateAppointment` prend l'id généré en Go (`store.NewAppointment.ID`) ; création en ligne, staff, séances de série, offre d'attente = jeton dérivé ; rappels (`Reminders.BaseURL` + `ManageKey`) et annulation cabinet avec `manage_url` ; templates d'annulation (email, SMS) et SMS de rappel avec lien (variante sans lien inchangée, 1 segment GSM-7) ; page d'un RDV annulé : bouton « Reprendre rendez-vous » vers `/p/{cabinet}/{praticien}` ; `SecretKey` obligatoire partout (`Config.Validate`), `.env.example` avec clé de dev, `deploy/.env.example` et `docs/install.md` (rotation de clé) ; décision dans `docs/decisions.md` ; tests (`manage_token_test.go`, config, web, goldens notify).
- **Reste à faire** : rien dans le périmètre.
- **Notes de reprise** : tests avec `testutil.ManageSecret` + `service.NewManageKey`. Un `.env` de dev existant doit recevoir une `DOCTOLIBRE_SECRET_KEY` (voir `.env.example`). Les offres de liste d'attente gardent un jeton aléatoire (`newManageToken`).
- **Écarts avec la spécification** : le SMS de rappel n'avait pas de `{{if .manage_url}}` (seuls les emails l'avaient) : ajouté. Textes SMS d'annulation/rappel reformulés (variante avec lien) pour tenir en un segment. Le report par le cabinet (`Move`) passe toujours `""` comme jeton (inchangé).
