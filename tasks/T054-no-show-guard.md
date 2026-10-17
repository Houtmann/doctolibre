# T054 — Garde-fou absences répétées

- **Phase** : 5
- **Dépend de** : T014, T017
- **Taille estimée** : S
- **Exécution** : sonnet (`task-implementer`) — revue Opus : standard

## Objectif
Un cabinet peut refuser la réservation en ligne aux patients ayant trop de rendez-vous manqués récents ; le patient est invité à appeler le cabinet.

## Contexte à lire
`internal/service/booking.go` (réservation en ligne, rapprochement du patient après OTP), `internal/service/otp.go`, `internal/service/patients.go` (l. ~124-150, compteur `NoShows`), `internal/service/calendar.go` (`SetStatus` l. ~793), `internal/service/practice.go` (`SettingsInput`), `internal/web/admin_config.go`, `admin_settings.html`, `admin_patient.html`, `public_verify.html`/`public_details.html`, `docs/rgpd.md`, `internal/domain` (`Clock`), `migrations/00002_core.sql` (index `appointment`).

## Spécification
- Migration : `practice.no_show_block_threshold smallint NULL` (NULL = désactivé, valeur ≥ 1 si présente via `CHECK`). Fenêtre fixe de 6 mois. Champ dans `/admin/settings`.
- Requête sqlc de comptage : RDV `status = 'no_show'` d'un `patient_id` dont le début est dans les 6 derniers mois (`Clock` injecté, jamais `time.Now()`). Vérifier par `EXPLAIN` qu'elle est couverte par un index existant (`appointment_patient_idx` ou autre) ; sinon proposer un index dans la migration. Consigner l'`EXPLAIN` dans Suivi.
- À la réservation en ligne, **après** vérification OTP, avec la même logique de rapprochement que la réservation : si le patient existant a ≥ seuil no-shows sur la fenêtre, la réservation est refusée avec un message neutre (« Cette réservation en ligne n'est pas possible. Merci d'appeler le cabinet au … ») et le téléphone du cabinet. **Aucune mention des absences** côté patient. Erreur métier dédiée dans `domain` mappée en un seul endroit par les handlers.
- Le cabinet peut toujours réserver pour ce patient (réservation staff non concernée).
- Badge visible sur la fiche patient côté cabinet (« Réservation en ligne bloquée ») quand le seuil est atteint.
- `docs/rgpd.md` : paragraphe sur ce traitement automatisé (finalité, 6 mois, intervention humaine possible : le cabinet réserve pour le patient).
- Pas de nom, téléphone ni email dans les logs : logguer les IDs.

## Critères d'acceptation
- [x] Seuil NULL : comportement inchangé.
- [x] Patient à seuil−1 no-shows : réservation passe ; à seuil : refus neutre avec téléphone du cabinet.
- [x] No-shows plus vieux que 6 mois ignorés (test avec horloge fixe, frontière incluse/exclue documentée).
- [x] Badge sur la fiche patient ; la réservation staff reste possible.
- [x] `docs/rgpd.md` à jour ; `EXPLAIN` dans Suivi.

## Vérification
```bash
make sqlc
make check
```

## Hors scope
Re-confirmation J-2, libération automatique de créneau (écartées), seuil par praticien.

## Suivi

> Rempli par l'agent pendant et à la fin de chaque session sur cette tâche. Le statut officiel est dans `tasks/README.md`.

- **Sessions** : 2026-10-17 (sonnet).
- **Fait** : migration 00020 (`practice.no_show_block_threshold smallint NULL`, CHECK >= 1) ; requêtes `CountPatientNoShowsSince` et `SetPracticeNoShowThreshold` ; `domain.ErrOnlineBookingBlocked` ; contrôle dans `Booking.Book` après OTP et rapprochement du patient (dans la transaction : l'OTP n'est pas consommé en cas de refus) ; champ « Absences avant blocage » dans `/admin/settings` (1..99, vide = désactivé) ; badge sur la fiche patient (`PatientDetail.OnlineBlocked`) ; message neutre + téléphone du cabinet (`onlineBookingBlockedMsg`, statut 403, un seul endroit dans `public.go`) ; paragraphe dans `docs/rgpd.md` ; tests service (seuil NULL, seuil-1, seuil, frontière incluse/exclue), validation des réglages, test handler public, test badge ; `make css` fait.
- **Reste à faire** : rien. Non vu au navigateur : champ de réglage et badge.
- **Notes de reprise** : fenêtre = `now.AddDate(0,-6,0)`, borne incluse (`lower(during) >= since`). Refus loggé en INFO avec practice_id et patient_id seulement. La réservation staff ne passe pas par `Book` : non concernée. `StartVerification` ne bloque pas (envoi du code) : le refus intervient après vérification du code, comme demandé.
- **EXPLAIN** (base de dev, `enable_seqscan=off` car table vide) : `Aggregate -> Index Scan using appointment_patient_idx on appointment`, `Index Cond: patient_id = ...`, `Filter: practice_id, status = 'no_show', lower(during) >= ...`. Couverte par l'index existant `appointment_patient_idx` (quelques dizaines de RDV par patient) ; pas de nouvel index.
- **Écarts avec la spécification** : aucun (comparaison `>=` seuil ; refus en HTTP 403).
