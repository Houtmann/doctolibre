# T071 — Auto-planification de la série par le patient

> UI à faire en daisyUI ; à lancer après T080 (décision du 2026-10-16).

- **Phase** : 5
- **Dépend de** : T055, T030, T014
- **Taille estimée** : L
- **Exécution** : sonnet (`task-implementer`) — revue Opus : renforcée (concurrence, quota, lien public)

## Objectif
À la fin du premier RDV, le praticien crée la série sans caler les dates : le patient reçoit le lien de série (T055) et y choisit lui-même ses séances restantes, proposées automatiquement selon ses jours et horaires préférés, dans la limite du nombre de séances prescrites.

## Contexte à lire
T055 (route `/s/{token}`, jeton de série, révocation), T030 Suivi (algorithme, `staffSlotContext`, `LineConflict`, remplacement), `internal/booking/series.go` (`ProposeSeries`), `internal/service/care_plans.go` (`Propose`, `Create`, `AddSession`, décompte), `internal/service/booking.go` (contexte de créneaux en ligne : préavis, horizon, fermetures, jours fériés T052), `internal/web/public.go` (rate limiting `manage`, CSRF public), `internal/web/care_plans.go`, `admin_care_plan_new.html`, `internal/notify/templates/email/series.*.tmpl`, `internal/service/notifications.go`.

## Spécification
- Migration (prochain numéro libre) : `practice.care_plan_self_scheduling boolean NOT NULL DEFAULT false` (réglage cabinet) et `care_plan.patient_self_schedule boolean NOT NULL DEFAULT false` (par série).
- Création de série côté cabinet : nouvelle case « Laisser le patient planifier les séances restantes » (visible si le réglage cabinet est actif). Le praticien peut planifier 0 à N séances lui-même puis cocher la case ; une série sans aucune séance planifiée devient possible dans ce cas (aujourd'hui `Create` attend des lignes : vérifier et adapter).
- Page `/s/{token}` (T055) : si la série est `active`, `patient_self_schedule` et le réglage cabinet actifs, et qu'il reste des séances (`sessions_prescribed` − séances non annulées > 0), afficher « Planifier mes séances » :
  1. `GET /s/{token}/plan` : formulaire jours préférés + fenêtre horaire (préremplis depuis la série), nombre de séances à planifier (1 … restant) ;
  2. `POST /s/{token}/plan/propose` : proposition via `booking.ProposeSeries` avec le **contexte de créneaux en ligne** (préavis mini, horizon de réservation, fermetures, jours fériés), pas `staffSlotContext` ; le motif de la série est accepté même s'il n'est pas `online_bookable` (le praticien l'a choisi). Écart mini et quota hebdo de la série respectés, séances existantes comprises. Le patient peut retirer une ligne ou demander le créneau suivant pour une ligne (htmx, avec repli sans JS) ;
  3. `POST /s/{token}/plan/confirm` : création **atomique** (tout ou rien) ; conflit → message « un créneau vient d'être pris » + nouvelle proposition, sans rien créer.
- **Quota infranchissable** : dans la transaction de confirmation, `SELECT … FROM care_plan WHERE id = $1 FOR UPDATE`, recompte des séances non annulées, refus si dépassement (deux onglets ou un double clic ne créent jamais plus que `sessions_prescribed`). Contrainte `EXCLUDE` existante pour les chevauchements.
- RDV créés : `source = 'online'`, rattachés à la série (`care_plan_id`), marges via `domain.HomeBuffers` (séries à domicile : adresse de la série / du patient ; si aucune adresse connue, refuser la planification en ligne et l'expliquer).
- Sécurité : le jeton de série est la seule authentification (lien envoyé à un contact déjà vérifié, pas d'OTP supplémentaire) ; CSRF sur les POST, rate limiting `manage` + un plafond de propositions par jeton et par heure, `Cache-Control: no-store`. Jeton révoqué → 404 neutre (tests T055 réutilisés).
- Notifications : **un seul** message récapitulatif (template `series`) listant les séances créées, pas un message par séance ; vérifier qu'aucun `appointment.created` par séance ne déclenche en plus une confirmation unitaire. Le praticien voit les séances apparaître dans l'agenda (pas de notification staff en v1).
- Métrique `AppointmentsCreated` incrémentée pour ces RDV (corrige au passage le manque noté dans STATUS.md pour ce chemin uniquement).
- Audit : création par le patient journalisée avec l'id de la série (pas de nom).

## Critères d'acceptation
- [x] Série « 10 séances, 2/semaine, mardi/jeudi » avec 1 séance posée par le praticien : le patient planifie les 9 autres en une validation.
- [x] Réglage cabinet off ou case série off ou série non `active` → pas de bouton, POST refusé (403/404 neutre).
- [x] Quota : deux confirmations concurrentes (goroutines, test DB) ne dépassent jamais `sessions_prescribed`.
- [x] Conflit au milieu de la proposition : rien n'est créé, nouvelle proposition affichée.
- [x] Préavis et horizon du cabinet respectés ; jour férié exclu ; test sur la semaine du changement d'heure (25/10/2026).
- [x] Un seul email récapitulatif (test sur les messages outbox).
- [x] Le parcours fonctionne sans JS (POST classique + redirect).

## Vérification
```bash
make sqlc
make check
```

## Hors scope
Modification des préférences de la série par le patient au-delà de ce formulaire, renouvellement de prescription, liste d'attente sur une séance de série, notification au praticien, paiement.

## Suivi

> Rempli par l'agent pendant et à la fin de chaque session sur cette tâche. Le statut officiel est dans `tasks/README.md`.

- **Sessions** : 2026-11-09 (sonnet).
- **Fait** : migration 00028 (`practice.care_plan_self_scheduling`, `care_plan.patient_self_schedule`) + sqlc (`CreateCarePlan` étendu, `CountCarePlanSessions`, `SetPracticeSelfScheduling`). Service `internal/service/care_plan_self.go` (sur `Booking`) : `SelfSchedule`, `ProposeSelf`, `NextSelf`, `ConfirmSelf` (transaction, `LockCarePlan` FOR UPDATE, recompte, revalidation de chaque créneau avec le contexte en ligne, `LineConflict`, un seul message, audit `care_plan.self_scheduled` acteur « patient », `OnCreated` + événements). `CarePlanByToken` expose `CanSelfSchedule`/`Remaining`. Staff : case de réglage dans `/admin/settings`, case « Laisser le patient planifier » + bouton « Créer la série sans séance » dans le formulaire de série ; `Create` accepte 0 séance si la case est cochée (réglage cabinet requis). Web : `GET /s/{token}/plan`, `POST .../plan/propose|adjust|confirm` (`public_series_plan.go`, page `public_series_plan.html`, partial `public_series_plan_proposal.html`, bouton sur `public_series.html`), limiteur `public-plan` (20 propositions/h par série), htmx en amélioration sur « Retirer » / « Créneau suivant ». `ui.css` régénéré.
- **Reste à faire** : passage au navigateur (parcours patient 360 px, clair/sombre, htmx réel) ; voir « Écarts ».
- **Notes de reprise** : tests `TestSelfSchedule*` (service, dont concurrence et DST 25/10/2026) et `TestSeriesPlan*` / `TestSettingsSelfSchedulingCheckbox` / `TestCarePlanFormOffersSelfScheduling` (web). `make` inutilisable : étapes à la main (`CGO_ENABLED=0`).
- **Écarts avec la spécification** (et pourquoi) :
  - Garde-fou no-show T054 (`onlineBookingBlocked`) appliqué à la proposition et à la confirmation (ajout demandé par l'orchestrateur) ; refus = page neutre 403 existante.
  - Décompte du quota = séances `done` + `booked` (décompte T030 existant), pas « non annulées » : une absence (`no_show`) ne consomme pas le quota, comme dans la vue du cabinet (sinon le patient ne pourrait pas replanifier ce que le cabinet peut).
  - Série à domicile : l'adresse n'est pas stockée sur la série ; elle est reprise de la dernière séance non annulée, à défaut de la fiche patient ; sans adresse, planification refusée (404 neutre, pas de bouton). Pour la même raison, une série à domicile ne peut pas être créée avec 0 séance (erreur de validation expliquée).
  - Envoi du lien (revue orchestrateur) : à la création d'une série `patient_self_schedule`, UNE invitation `series_invite` (email + SMS, nouveaux templates et golden files, consignes « À prévoir » reprises) part quel que soit le nombre de séances posées (0, 1, N), à la place de la confirmation / du récapitulatif ; message sans `appointment_id` (`notifier.seriesInvite`), canal et consentements de la confirmation du cabinet, dedup `series-invite:<série>:<canal>`. Le flash au cabinet garde le lien.
  - Revalidation à `confirm` (sous verrou) de l'écart minimal et du quota hebdomadaire, séances existantes comprises (`checkSeriesRules`) : test `TestSelfScheduleConfirmRevalidatesGapAndWeeklyQuota`.
  - Passage navigateur (orchestrateur) : (1) `adjust` affichait l'UTC (valeurs postées en `Z`) → converties au fuseau du cabinet (`SelfView.Loc`) ; (2) « Créneau suivant » sautait au jour suivant (`From` = lendemain) → `NextSelf` prend le premier créneau valide strictement après l'actuel, même jour possible, plage horaire respectée, écart minimal et quota hebdomadaire vérifiés avec les AUTRES séances (`checkSeriesRules`) ; le saut au 2/12 venait de semaines pleines (2/semaine) et du lendemain exclu ; (3) alertes d'erreur placées avant le formulaire sur la page (dans le fragment pour htmx, champ `Inline`). Tests : `TestSeriesPlanAdjustKeepsPracticeLocalTime`, `TestSeriesPlanNoSlotAlertPrecedesForm`, `TestSelfScheduleNextSlotIsFirstValidSlotSameDayIncluded`.
  - Passage navigateur (orchestrateur) : (1) `adjust` affichait l'UTC (valeurs postées en `Z`) → converties au fuseau du cabinet (`SelfView.Loc`) ; (2) « Créneau suivant » sautait au jour suivant (`From` = lendemain) → `NextSelf` prend le premier créneau valide strictement après l'actuel, même jour possible, plage horaire respectée, écart minimal et quota hebdomadaire vérifiés avec les AUTRES séances (`checkSeriesRules`) ; le saut au 2/12 venait de semaines pleines (2/semaine) et du lendemain exclu ; (3) alertes d'erreur placées avant le formulaire sur la page (dans le fragment pour htmx, champ `Inline`). Tests : `TestSeriesPlanAdjustKeepsPracticeLocalTime`, `TestSeriesPlanNoSlotAlertPrecedesForm`, `TestSelfScheduleNextSlotIsFirstValidSlotSameDayIncluded`.
  - Pas de notification au praticien (hors scope), `VerifiedChannel` vide (contact de la fiche, comme une série créée par le cabinet).
