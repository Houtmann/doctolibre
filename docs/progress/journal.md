# Journal des sessions

Append-only, **la plus récente en haut**. Une entrée par session de travail (humain ou agent).
Rester court : le détail technique va dans la section « Suivi » de la tâche, les décisions dans [`decisions.md`](../decisions.md).

<!-- Modèle d'entrée :

## AAAA-MM-JJ — <titre court> (<agent/modèle ou humain>)

- **Tâches** : Txxx (todo → in_progress | done), Tyyy
- **Fait** : 2–5 puces, ce qui est vrai maintenant
- **Vérification** : `make check` ✅ / ❌ (+ ce qui échoue)
- **Reste / reprise** : ce que la prochaine session doit faire en premier
- **Décisions** : liens vers les lignes ajoutées dans decisions.md, ou « aucune »
-->

## 2026-10-18 — T099 agenda : praticiens non chargés au changement de vue (sonnet ; contrôle : opus)

- **Tâches** : T099 todo → review
- **Fait** : cause 100 % JS : « Jour » et « Par praticien » ont la même plage, EventCalendar ne relance ni la requête ni `datesSet` ; `viewDidMount` recharge si la liste de praticiens voulue change, `wantedIds()` honore `practitioner=` de l'URL (dont `all`), `syncView()` met la vue à jour dans l'URL
- **Vérification** : `make check` ✅ (rapport de l'exécutant), vu au navigateur sur base de démo à 5 praticiens ; diff relu par l'orchestrateur. Pas de test Go (serveur correct)
- **Reste** : relecture humaine ; en semaine/jour avec `practitioner=all`, les RDV de tous les praticiens se superposent dans une seule colonne (voulu ?)
- **Décisions** : aucune

## 2026-10-17 — T051 annulation groupée et T055 lien de série, en parallèle (sonnet ; contrôle : opus)

- **Tâches** : T051 todo → review, T055 todo → review (deux `task-implementer` en worktree)
- **Fait** : T051 : aperçu puis confirmation en une transaction (absence `closed` créée avant les annulations, audit, outbox avec motif + `/r/`), validation partagée `closureRange`, motif optionnel dans `TemplateCancellation` (tronqué à 20 car. en SMS), plafond 300 RDV. T055 : migration 00025 (`care_plan.token_version`, `token_hash`), `/s/{token}` et `/s/{token}/ics` (seau `manage`, `no-store`, `no-referrer`, 404 neutre), lien dans la confirmation unique de série, `series_url` effacé à l'envoi, révocation à la fusion et à l'effacement
- **Vérification** : `make check` ✅ relancé par l'orchestrateur sur chaque branche et sur la fusion des deux (branche `trial-merge-t051-t055`, un conflit d'une ligne dans `notifier.data` résolu, `ui.css` régénéré). Non vu au navigateur : `/admin/absences` (aperçu, confirmation, bouton de l'agenda, 360 px), `/s/{token}`
- **Reste** : relecture humaine, fusion dans `main` ; EXPLAIN de `ListCarePlanSessions` sur données réelles ; T071 débloquée quand T055 sera `done`
- **Décisions** : 4 lignes du 2026-10-17 (T051 ×2, T055 ×2)

## 2026-10-17 — Analyse de l'audit UX des parcours patients, tâches T093–T098 (sonnet ; contrôle : opus)

- **Tâches** : T093 à T098 créées (todo)
- **Fait** : constats de l'audit UX vérifiés dans le code par deux sous-agents Sonnet : presque tous confirmés, dont 3 bugs (offre de liste d'attente envoyée à un patient bloqué T054, déplacement sur le même créneau qui renvoie un email et annule les rappels, consigne interne visible sur la page vie privée) ; aucune tâche existante ne les couvrait ; découpage en 6 fiches
- **Vérification** : aucune (documentaire)
- **Reste** : lancer T093 (dépend de T088, en review) ; T094 est prête dès que T081 est validée
- **Décisions** : 3 lignes du 2026-10-17 dans decisions.md (accusé d'annulation patient, reprise sans OTP sous 30 jours, redirection de `/` en mono-cabinet)

## 2026-10-17 — T092 page « réservation en ligne indisponible » (sonnet ; contrôle : opus)

- **Tâches** : T092 créée → review (dépend de T088, encore en review : lancée à la demande de la session principale)
- **Fait** : page `public_booking_blocked.html` (titre neutre, pas de code HTTP affiché, bouton `tel:` normalisé, retour vers la page du praticien) ; `renderBookingBlocked` (403, `no-store`) partagée par la réservation, l'inscription en liste d'attente et `/w/{token}` ; `onlineBookingBlockedMsg` supprimée ; tests des 3 parcours + cabinet sans téléphone
- **Vérification** : `make check` ✅ relancé par l'orchestrateur ; navigateur (`doctolibre_demo`, port 8092 car 8090 occupé) : inscription en liste d'attente de « Test Navigateur » → 403, page conforme, `tel:0102030405`, retour `/p/cabinet-demo/claire-martin`, 1024 px clair et 360 px sombre ✅. Réservation et `Claim` non vus au navigateur (même fonction de rendu, couverts par tests).
- **Reste** : relecture humaine ; le libellé du lien de retour est passé depuis le Go (même texte partout, pourrait vivre dans le template)
- **Décisions** : aucune

## 2026-10-17 — Passage navigateur T050/T054, T088 corrections RDV (sonnet ; contrôle : opus)

- **Tâches** : T050 review → done, T054 review → done (validées par l'humain après passage navigateur) ; T088 todo → review
- **Fait** : passage navigateur sur `doctolibre_demo` (réservation, OTP, lien `/r/…`, annulation, seuil 2 absences, refus, badge fiche) ; T088 : trajet domicile conservé au déplacement patient, garde-fou T054 dans `Waitlist.Claim`/`Join`, une offre par patient, `SetStatus` ne réactive plus un RDV annulé, lien de gestion dans le déplacement par le cabinet, `<title>` du RDV annulé, refus T054 en page 403 à part
- **Vérification** : `make check` ✅ relancé par l'orchestrateur ; diff SQL et transactions relus. Navigateur : titre du RDV annulé ✅, refus de réservation et d'inscription en liste d'attente ✅ (aucune inscription ni RDV créé) ; `Claim` via `/w/{token}` non vu (couvert par test service).
- **Reste** : page de refus trop brutale pour un patient (grand « 403 Accès refusé », téléphone non cliquable, retour vers `/` et non vers le cabinet) → à reprendre dans une petite tâche ; EXPLAIN de `ListWaitlistCandidates` sur données réelles ; T091 débloquée par T088 une fois validée
- **Décisions** : 2 lignes T088 du 2026-10-17 dans decisions.md

## 2026-10-17 — T060 ressources partagées (sonnet ; contrôle : opus)

- **Tâches** : T060 todo → review
- **Fait** : migration 00024 (`resource`, `appointment_type_resource`, `appointment_resource` + EXCLUDE GiST `WHERE (active)`, trigger `appointment_sync_resource`) ; moteur `booking` : `Request.ResourceBusy` ; `slotContext` charge les occupations de la ressource ; violation traduite en `ErrSlotTaken` (`store.isSlotConflict`) ; CRUD ressources et select « Ressource exigée » dans le panneau des motifs
- **Vérification** : `make check` ✅ relancé par l'orchestrateur ; tests ressources (concurrence 2 praticiens, annulation, report, staff, inactive, tenancy, DST moteur) ✅ ; relus en entier : migration/trigger, requêtes, traduction d'erreur, moteur, service ; toutes les écritures d'`appointment` passent par `store.CreateAppointment` / `MoveAppointment`. EXPLAIN : index GiST de l'EXCLUDE, 0,3 ms sur 200 000 lignes. Navigateur **non vu**.
- **Reste** : passage navigateur `/admin/appointment-types` (section ressources, select) ; `goose down` de 00024 non testé
- **Décisions** : ligne T060 du 2026-10-17 dans decisions.md

## 2026-10-17 — T056 et T087 en parallèle (sonnet ×2 en worktrees ; contrôle : opus)

- **Tâches** : T056, T087 todo → review
- **Fait** : T056 « Mon profil » (`/admin/profile`) séparé des réglages du cabinet (admin seul, notifications et fournisseurs compris), 403 sur champ cabinet posté au profil, séries de séances ouvertes au secrétariat avec choix du praticien, doc des rôles dans `docs/install.md` ; T087 `httpx.Limiter` (seaux partagés par parcours, IPv6 /64, map bornée, `Retry-After` calculé, fragment htmx en 429, métrique `doctolibre_rate_limited_total{limiter}`), limites admin-ip / admin-user / admin-heavy dans `RequireUser`, seaux publics browse / search / book / manage / offer, quota de 30 réservations/h par praticien
- **Vérification** : `make check` ✅ dans chaque worktree puis sur main après fusion par patchs ; `go test -race ./internal/httpx` ✅ ; T087 relue en entier (revue renforcée) ; T056 : policy, service et handlers relus. Navigateur **non vu**.
- **Reste** : passage navigateur (profil / cabinet selon le rôle, formulaire de série côté secrétariat, alerte 429 htmx) ; non limités : `GET /ics/{file}`, `/p/{practice}/privacy`
- **Décisions** : trois lignes du 2026-10-17 (T056 ×2, T087) dans decisions.md

## 2026-10-17 — T086 fournisseurs email/SMS par cabinet (sonnet ; contrôle : opus)

- **Tâches** : T086 créée (spec opus, demande humaine : par cabinet, désactivable) → in_progress → review
- **Fait** : migration 00023 `practice_notify_transport` (modes `server` / `custom` / `disabled`), secrets AES-256-GCM (`internal/secretbox`), dispatcher par message avec cache par `updated_at` (sans redémarrage), garde-fou SSRF à la connexion (`notify/guard.go`, `DOCTOLIBRE_NOTIFY_ALLOW_PRIVATE_HOSTS`), page `/admin/settings/notifications/transport` avec envoi de test, canal désactivé → messages `cancelled`, code patient restreint aux canaux actifs, réservation en ligne et liste d'attente fermées sans canal, invitations refusées sans email ; docs install/sms/rgpd
- **Vérification** : `make sqlc css check` ✅ relancé par l'orchestrateur ; relus : migration, secretbox, guard, transport, worker, senders custom, SendTest (hors transaction), booking. Navigateur **non vu**.
- **Reste** : passage navigateur (page de réglage clair/sombre 360/1280 px, pages publiques fermées, test vers Mailpit)
- **Décisions** : deux lignes T086 du 2026-10-17 dans decisions.md

## 2026-10-17 — T054, T059, T072 en parallèle (sonnet ×3 en worktrees ; contrôle : opus)

- **Tâches** : T054, T059, T072 todo → review
- **Fait** : T054 blocage de la réservation en ligne au-delà d'un seuil d'absences sur 6 mois (migration 00020, badge fiche patient) ; T059 délégation d'agenda à un remplaçant bornée en jours locaux, relue à chaque requête (00021, section « Remplaçants » de l'équipe) ; T072 page « Ma journée » mobile avec pointage Fait/Absent en un clic et `status_changed_by/at` (00022)
- **Vérification** : `make check` ✅ dans chaque worktree puis sur main après fusion par patchs (sqlcgen et `ui.css` régénérés) ; T059 relue en entier (revue renforcée), SQL et transactions relus pour T054 et T072. Navigateur **non vu** pour les trois.
- **Reste** : passage navigateur (réglage du seuil et badge ; section Remplaçants ; « Ma journée » à 360 px, critère non coché) ; vérifier que le lien d'itinéraire OpenStreetMap (`directions?route=adr1;adr2`) géocode bien des adresses texte ; ICS du remplaçant hors scope
- **Décisions** : trois lignes du 2026-10-17 dans decisions.md

## 2026-10-16 — T053 import CSV des patients (sonnet ; contrôle : opus)

- **Tâches** : T053 todo → review (spec précisée par l'orchestrateur : limite multipart du CSRF)
- **Fait** : `GET/POST /admin/patients/import` (3 rôles) en deux étapes aperçu → confirmation ; parseur pur (BOM, UTF-8/cp1252, `;`/`,`, en-têtes sans accents, ≤ 2 Mo / 5 000 lignes) ; doublons base (une requête, index phone/email) et intra-fichier ; insertion atomique en lot ; opt-in à faux par défaut ; audit à compteurs
- **Vérification** : `make check` ✅ (relancé par l'orchestrateur) ; diff CSRF, SQL, service et handler relus. EXPLAIN de `FindImportDuplicates` sur 200 000 patients : BitmapOr sur les deux index, ~65 ms (mesuré par l'agent). Navigateur **non vu**.
- **Reste** : passage navigateur (upload, aperçu, confirmation, 375 px) ; un fichier > 4 Mio reçoit le 403 générique du CSRF au lieu du message « dépasse 2 Mo »
- **Décisions** : import CSV (voir decisions.md)

## 2026-10-16 — T085 choix de la couleur du praticien (sonnet ; contrôle : opus)

- **Tâches** : T085 créée (écart signalé en T057) puis todo → review ; lancée avant que T057 soit `done`, à la demande de l'humain
- **Fait** : champ couleur au profil (`UpdateSettings`, validé par `domain.ValidColor`, rien d'écrit si invalide) ; colonne « Couleur » dans l'équipe, `POST /admin/team/practitioners/{id}/color` → `auth.SetPractitionerColor` (permission de gestion d'équipe, filtre `practice_id`, audit `practitioner_color_changed`)
- **Vérification** : `make css` + `make check` ✅ (relancés par l'orchestrateur) ; tests 303/422/403/404 et relecture en base après échec. Navigateur **non vu**.
- **Reste** : vérifier au navigateur l'en-tête de colonne en vue « Par praticien » après changement (clair/sombre) ; un praticien sans compte utilisateur lié n'a pas de champ couleur dans l'équipe
- **Décisions** : couleur du praticien par deux voies (voir decisions.md)

## 2026-10-16 — T057 couleurs dans l'agenda (sonnet ; contrôle : opus)

- **Tâches** : T057 (todo → review), spécification réécrite par l'orchestrateur pour EventCalendar (plus de route `/admin/colors.css`)
- **Fait** : `domain.ValidColor` / `ColorOrDefault` (validation unique, défaut `#2563eb`) ; `type_color` dans le JSON de l'agenda et `color` dans les ressources de la page ; `agenda.js` pose `--ty-color` (liseré du RDV) et `--pr-color` (liseré bas de l'en-tête de colonne) par CSSOM ; le statut reste porté par le fond, le texte, le libellé et le barré.
- **Vérification** : `make check` ✅ (relancé par l'orchestrateur) ; couleurs corrompues en base remplacées par le défaut (test) ; navigateur : vue par praticien 1280 px clair et sombre, en-têtes et liserés colorés, aucune violation CSP.
- **Reste** : pas de champ couleur pour le praticien (en-têtes tous au défaut tant qu'aucune UI ne la règle) ; non vu : 360 px, changement de couleur d'un motif via l'écran.
- **Décisions** : couleurs par JSON + CSSOM (voir decisions.md).

## 2026-10-16 — T084 pages restantes, retrait de Pico et de l'ancien CSS (sonnet ; contrôle : opus)

- **Tâches** : T084 (todo → review)
- **Fait** : login, mot de passe oublié/réinitialisation, invitation, tableau de bord et statut en daisyUI ; supprimés : `pico.min.css`, `tokens.css`, `components.css`, `base.css`, `app.css`, `dev_styleguide_v2` et sa route, `design/`, `poc/` ; `static/` = `ui.css`, `app.js`, `agenda.js`, `htmx.min.js`, `vendor/` ; CSP inchangée. Binaire : 26 562 114 → 26 362 242 octets.
- **Vérification** : `make check` ✅ (relancé par l'orchestrateur) ; les 3 grep des critères vides ; navigateur : contraste AA et débordement mesurés par script sur ~25 pages à 360 et 1280 px clair/sombre, agenda au clavier (Entrée, Échap rend le focus), connexion en erreur ; contrôle orchestrateur : connexion 1280 clair, tableau de bord 1280 sombre.
- **Reste** : écrans non vus listés dans STATUS (prochaine action 3) ; `ui_review.md` (audit Pico obsolète à la racine) à supprimer ou garder : question à l'humain.
- **Décisions** : suppression de `design/`, `poc/` et des CSS hérités (voir decisions.md).

## 2026-10-16 — T083 agenda staff avec EventCalendar (sonnet ; contrôle : opus)

- **Tâches** : T083 (todo → review)
- **Fait** : EventCalendar 5.15.0 vendored (`static/vendor/`, VERSION + sha256 + licence MIT), chargé seulement par la page agenda ; `static/agenda.js` (semaine / jour / par praticien, buffers hachurés, indisponibilités en fond) ; endpoint `GET /admin/agenda/events` (cabinet de la session, 403 confrère / 404 autre cabinet, plage ≤ 42 jours calendaires, JSON sans contact) ; dialogue htmx (`appointment_detail`, `appointment_form`) avec 204 + `HX-Trigger agenda:changed` puis `refetchEvents` ; repli sans JS = liste du jour serveur ; grille maison `calendar_grid` supprimée ; `ListCalendarAppointments` multi-praticiens.
- **Vérification** : `make check` ✅ (relancé par l'orchestrateur) ; EXPLAIN : `Bitmap Index Scan on appointment_no_overlap`, 3,9 ms sur 610 k RDV ; `TestCalendarWeekWith150AppointmentsIsFast` adapté (~1,3 ms) ; tests DST 29/03 et 25/10. Navigateur (base demo, 4 praticiens, 88 RDV) : vue par praticien 1280 px clair, semaine 1280 px sombre, jour 360 px sombre sans débordement ; Entrée sur un RDV ouvre le dialogue, « Marquer honoré » ferme et rafraîchit, focus 2 px visible ; aucune erreur console nouvelle.
- **Reste** : non vu dans un vrai navigateur : JS désactivé, déplacer/annuler via dialogue (tests HTTP seulement) ; le focus revient en haut de page à la fermeture du dialogue (au lieu du RDV) ; en vue semaine/jour seul le premier praticien coché est affiché. Ensuite T084.
- **Décisions** : EventCalendar en `timeZone: "UTC"` sur l'heure murale ; CSS/JS d'EventCalendar chargés par la seule page agenda ; dialogue 204 + `HX-Trigger` (voir decisions.md).

## 2026-10-16 — T082 back-office en daisyUI (sonnet ; contrôle : opus)

- **Tâches** : T082 (todo → review)
- **Fait** : 14 pages `admin_*` et 5 partials (patient_fields, patient_results, types/availability/absences_panel) en daisyUI : tables `overflow-x-auto` avec colonnes secondaires masquées en petit écran, pagination `join`, états vides et note administrative en `alert`, erreurs de champ `input-error` + `aria-invalid`/`aria-describedby`. Après revue : select de rôle de l'équipe lisible à 375 px, journal d'accès réduit à Date + Action sur mobile (lignes de 130 à 46 px).
- **Vérification** : `make check` ✅ ; aucun `id`/`name`/`hx-*`/`action`/`aria-*` perdu, aucun `style=`, plus de `dl-` ; navigateur (base demo) : 375 px sombre sur toutes les pages sans débordement, formulaire patient en erreur, focus visible.
- **Reste** : états avec données non vus (liste d'attente, absences, invitations, pagination) → contrôle final T084 ; `admin_test.go` et `styleguide_test.go` cherchent encore `dl-field is-invalid` (login et styleguide, T084).

## 2026-10-16 — T081 parcours patient public en daisyUI (sonnet ; contrôle : opus)

- **Tâches** : T081 (todo → review)
- **Fait** : 14 pages publiques + `booking_grid`, `booking_summary` migrés en daisyUI (stepper `steps`, cartes de motifs, jours repliables en `<details>`, honeypot en `sr-only`) ; corrections de revue : navigation de semaine qui débordait à 360 px, taille de la case de consentement
- **Vérification** : `make check` ✅ ; parcours complet sur la base de démo (motif → créneau → coordonnées → OTP → confirmé → annuler/déplacer, liste d'attente) à 360 et 1280 px, clair et sombre, `scrollWidth` = 360 ; formulaires en POST classique + liens pour la navigation (pas de test réel avec JS désactivé)

## 2026-10-16 — Choix daisyUI, POC, T080 (exécutant : sonnet ; contrôle : opus)

- **Tâches** : T080–T084 créées ; T065–T067 → done, T068 → superseded ; T080 (todo → review).
- **Fait** : POC `poc/daisyui/` validé par l'humain ; outillage Tailwind v4 standalone (`make tools` avec sha256 épinglé, `make css`, `make css-check` en CI), daisyUI 5.7.47 versionné dans `tools/daisyui/`, thèmes `doctolibre`/`doctolibre-dark`, layouts public/admin et partials `flash`/`form_errors` en daisyUI, `/dev/styleguide` réécrit, AGENTS.md/PLAN.md/design-system.md à jour. `base.html` ne charge plus que `ui.css`.
- **Vérification** : `make check` ✅ (relancé par l'orchestrateur), `go build` sans binaire ✅ ; navigateur : styleguide clair/sombre OK, pages non migrées sommaires (attendu).
- **Reste / reprise** : relecture humaine de T080 ; puis T081, T082, T083 en parallèle (worktrees). Non testé : `make tools` sous Linux et en CI réelle ; drawer admin mobile avec un vrai compte.
- **Décisions** : `decisions.md`, 2026-10-16 (front daisyUI, css-check, `--fx-noise`).

## 2026-10-16 — T065, T066, T067 en parallèle (exécutant : sonnet ; contrôle : opus)

- **Tâches** : T064 (review → done, validée par l'humain) ; T065, T066, T067 (todo → review) en trois worktrees, fusionnées sur main.
- **Fait** : formulaires et tableaux admin en `.dl-field`/`.dl-table` avec erreurs par champ (patient, connexion) ; parcours patient en `.dl-steps`/`.dl-choices`/`.dl-slots`/`.dl-card` ; agenda en `.dl-cal`/`.dl-appt`, `.dl-segmented`, dialogue `.dl-dialog`.
- **Fusion** : conflits attendus en fin de `components.css` (blocs concaténés, `.dl-field-row` dédoublonné) et dans `app.css` (suppressions des deux côtés).
- **Vérification** : `make check` ✅ sur main après fusion ; navigateur : parcours public 375 px sombre, styleguide 1280 px clair. Pages admin authentifiées non revues (session de démo expirée).
- **Incidents** : deux agents ont fait `pkill -f 'doctolibre serve'` (serveurs des autres tués) ; cache Go vidé avant lancement (15 → 23 Go libres).
- **Reste / reprise** : relecture humaine des pages admin (agenda, dialogue, formulaires, tableaux) ; puis T068 (retrait de Pico).
- **Décisions** : `decisions.md`, 2026-10-16 (T065–T067).

## 2026-10-16 — T064 habillage v2 (exécutant : sonnet ; contrôle : opus)

- **Tâches** : T063 (review → done, validée par l'humain), T064 (todo → review).
- **Fait** : layouts, en-tête et nav v2 avec `aria-current`, messages `.dl-alert`, tous les boutons en `.dl-btn` (~40 templates), bloc `[data-theme="light"]` dans les jetons, focus pétrole ; classes v1 mortes retirées d'`app.css`.
- **Revue** : 2 corrections (variable globale `navLinks`, couleur du focus).
- **Vérification** : `make check` ✅ ; navigateur à 375 et 1280 px, clair et sombre, sur la démo.
- **Reste / reprise** : valider T064, puis T065, T066, T067 en parallèle (worktrees, templates disjoints ; attention aux deux copies `components.css` si deux agents y ajoutent un composant), puis T068.
- **Décisions** : liens-boutons sans `role="button"`, `navCurrent` (`decisions.md`, 2026-10-16).

## 2026-10-16 — Design system v2 et T063 (exécutant : sonnet ; contrôle : opus)

- **Tâches** : design v2 « Pétrole & papier » dans `design/` (jetons, composants `.dl-*`, aperçus, `MIGRATION.md`) ; migration découpée en T063–T068 ; T063 (todo → review).
- **Fait** : `tokens.css`, `components.css` servis à côté de Pico (`base.css` embarqué non lié) ; fuites Pico neutralisées ; page `/dev/styleguide/v2` (dev seulement), clair + sombre par bloc.
- **Vérification** : `make check` ✅ ; styles calculés identiques avec/sans les feuilles v2 sur `/dev/styleguide` et `/` (clair et sombre).
- **Reste / reprise** : valider T063 puis T064 (habillage général, + bloc `[data-theme="light"]` des jetons) ; ensuite T065, T066, T067 en parallèle, puis T068.
- **Décisions** : copie `design/` → `static/` et bloc Base séparé (`decisions.md`, 2026-10-16).

## 2026-10-16 — T052, T058, T062 en parallèle (exécutant : sonnet ; contrôle : opus)

- **Tâches** : T052, T058, T062 (todo → review), en trois worktrees ; T050 reste en review (relecture humaine en attente).
- **Fait** : jours fériés français (option cabinet, Alsace-Moselle, migration 00019, « Férié » dans l'agenda) ; deux `VALARM` (-P1D, -PT2H) dans le `.ics` patient, aucun dans le flux praticien ; tokens `--app-*` et mode sombre dans `app.css`, `docs/design-system.md`, page `/dev/styleguide` (dev seulement).
- **Revue** : T052 en revue renforcée, une correction renvoyée : une ouverture exceptionnelle partielle rouvrait tout le jour férié ; désormais férié = journée moins les ouvertures explicites. T062 vérifiée dans le navigateur (1280 px clair, 360 px sombre). Conflit `app.css` T052 × T062 résolu à la main (ajout `.cal-holiday` en fin de fichier).
- **Vérification** : `make check` ✅ sur chaque worktree puis sur main après fusion ; `make sqlc` stable.
- **Incident** : disque plein pendant T052 (cache Go des sous-agents, 19 Go dans `/tmp/go-build`), vidé avec `go clean -cache` après accord.
- **Reste / reprise** : valider T050, T052, T058, T062 ; ensuite T051 et T055 (après T050), T057 et T061 (après T062), T053, T054, T056, T059, T060.
- **Décisions** : jours fériés en `Exception{Closed}` (journée moins ouvertures) ; design system par tokens (`decisions.md`, 2026-10-16).

## 2026-10-16 — Phase 5 ouverte, T050 (exécutant : sonnet ; contrôle : opus)

- **Tâches** : T014–T046 en review → done (validées par l'humain) ; 12 tâches T050–T061 créées (phase 5 « Retours terrain ») ; T050 → review.
- **Fait** : liste de propositions produit challengée contre le code (écartés : tournées par secteur, Web Push/Wallet, re-confirmation J-2 avec libération auto, PDF, capacité N). T050 : jeton de gestion dérivé HMAC (`manage_token_version`, migration 00018), lien dans les rappels J-1 et les annulations par le cabinet, séances de série gérables par lien, bouton « Reprendre rendez-vous » sur un RDV annulé, `DOCTOLIBRE_SECRET_KEY` obligatoire partout.
- **Revue** : spec T050 corrigée avant délégation (l'annulation ne révoque pas le lien, sinon la page « reprendre RDV » serait inaccessible ; UUID tiré en Go avant l'INSERT). Diff relu en entier.
- **Vérification** : `make check` ✅, tests DB lancés sans cache.
- **Reste / reprise** : un `.env` de dev existant doit recevoir `DOCTOLIBRE_SECRET_KEY`. Le déplacement par le cabinet (`Calendar.Move`) envoie encore un message sans lien. Prochaines : T051, T055 (débloquées après validation de T050), ou T052–T058 en parallèle.
- **Décisions** : jeton dérivé HMAC et rotation de clé (`decisions.md`, 2026-10-16).

## 2026-10-16 — T031, T033, T043, T044, T046 (exécutant : sonnet ; contrôle : opus)

- **Tâches** : T031, T033, T043, T044, T046 → review — toutes les tâches du plan sont implémentées.
- **Fait** : liste d'attente (propositions à jeton, premier qui clique gagne) ; multi-praticiens (matrice de permissions, invitations, vue jour multi-colonnes, premier disponible) ; RGPD (export, suppression/anonymisation, purge quotidienne, audit, `docs/rgpd.md`) ; doc d'installation et de coûts ; observabilité (`/metrics` branché, `/admin/status`, alertes email aux admins).
- **Revue** : T043 en revue renforcée. Trois bugs d'intégration entre tâches parallèles corrigés sur main avec tests : fausse alerte « messages en échec » après chaque purge (T043 × T046) ; suppression d'un patient inscrit en liste d'attente en échec FK + jeton `offer_url` en clair + fusion/export (T031 × T043) ; lien d'invitation en clair, invitations expirées jamais purgées, alertes envoyées aux admins désactivés (T033 × T043/T046). Lien « Confidentialité » réservé à l'admin dans la navigation.
- **Vérification** : `make check` ✅ après chaque merge (`TestCalendarWeekWith150AppointmentsIsFast` a échoué une fois sous charge, passe seul). Non vérifié : rendu 360 px et vue jour 4 praticiens à 1280 px, installation réelle sur VM (T044).
- **Reste / reprise** : relecture humaine des 14 tâches en review ; passage navigateur.
- **Décisions** : 9 entrées dans `decisions.md` (2026-10-16).

## 2026-10-15 — T023 et T030 (exécutant : sonnet ; contrôle : opus)

- **Tâches** : T023, T030 → review
- **Fait** : confirmations, annulations, déplacements et rappels J-1/H-2 (email/SMS) enfilés dans la transaction métier, tâche périodique de rappels idempotente, page `/admin/settings/notifications` avec estimation SMS ; séries de séances (proposition éditable, création atomique, badge « séance n/N », remplacement d'une séance annulée).
- **Revue** : T023 a intégré T030 (une confirmation par série). Merges sans conflit. Directive ajoutée aux sous-agents : pas de build complet à chaque étape.
- **Vérification** : `make check` ✅. Non vérifié : bout en bout réservation → Mailpit + SMS dans les logs (critère T023 non coché), rendu 360 px.
- **Reste / reprise** : relecture des 9 tâches en review ; stash `t023-wip-sonnet` laissé dans la pile git (à supprimer par l'humain) ; puis T031, T033, T043, T044.
- **Décisions** : 6 entrées dans `decisions.md` (de « Séries de séances » à « Sous-agents »).

## 2026-10-15 — T015, T017, T032, T041, T042 (exécutant : sonnet ; contrôle : opus)

- **Tâches** : T015, T017, T032, T041, T042 → review (lancées sur T014/T016 encore en review, code déjà sur `main`)
- **Fait** : annulation/déplacement par le patient via `/r/{token}` ; fiches patients (recherche sans accents, fusion de doublons) ; visites à domicile (trajet dans `during_blocked`, zones, adresse) ; sauvegardes chiffrées age + restauration testée ; flux ICS praticien à jeton révocable.
- **Revue** : 2 corrections renvoyées (T015 : SQL brut remplacé par une requête sqlc `FOR UPDATE` ; T042 : test du masquage des jetons dans les logs). Migrations pré-numérotées 00009–00011, pas de collision. Conflits résolus : `ics_test.go` (flag `-update` en double), `public.go` (union T015/T032), `sqlcgen` régénéré.
- **Vérification** : `make check` ✅ après chaque merge. `TestCalendarWeekWith150AppointmentsIsFast` a échoué une fois sous forte charge (load 20, agents en parallèle), passe seul. Non vérifié : rendu 360 px, import ICS dans un vrai client, S3 réel.
- **Reste / reprise** : relecture humaine des 7 tâches en review ; puis T023 (notifications), T030, T033, T043, T044.
- **Décisions** : 6 entrées dans `decisions.md` (de « Sauvegardes » à « Domicile »).

## 2026-10-15 — T014 et T016 (exécutant : sonnet ; contrôle : opus)

- **Tâches** : T012, T021, T022, T024, T040 → done (validées par l'humain) ; T014, T016 → review
- **Fait** : réservation en ligne patient (créneaux, OTP SMS/email via l'outbox, patient retrouvé ou créé, page `/r/{token}` + .ics) ; agenda praticien jour/semaine (création, déplacement, annulation, statut, `<dialog>` htmx, liste mobile). `domain.NormalizePhone` partagé.
- **Revue** : T014 en revue renforcée — transaction de `Book` relue (OTP consommé et créneau revalidé dans la même transaction, `EXCLUDE` tranche), test de concurrence 1 succès / 1 `ErrSlotTaken`. Conflits de merge résolus par union (`serve.go`, `routes.go`, `render_test.go`, `app.css`).
- **Vérification** : `make check` ✅. Non vérifié : rendu 360 px, agenda au clavier en vrai navigateur.
- **Reste / reprise** : relecture de T014/T016, puis T015, T017, T032, T041, T042.
- **Décisions** : 7 entrées dans `decisions.md` (de « OTP de réservation » à « `store.MoveAppointment` »).

## 2026-10-15 — 5 tâches suivantes (exécutant : sonnet ; contrôle : opus)

- **Tâches** : T012, T021, T022, T024, T040 → review
- **Fait** : paramétrage cabinet/motifs/plages/absences ; email SMTP stdlib + mot de passe oublié ; SMS (log, HTTP générique, Android, OVH) ; worker outbox dans `serve` + sous-commande `worker` + `/admin/messages` ; Dockerfile/compose/Caddy/systemd/release + `DOCTOLIBRE_TRUSTED_PROXIES`. Dispatchers réels branchés après merge.
- **Revue** : 1 correction renvoyée (T040 : `tls` vide refusé par Caddy). À la fusion : migrations renumérotées 00005–00007, conflits `config.go`/`routes.go`/`schema_test.go` résolus, doublon du switch `DOCTOLIBRE_WORKER` retiré, `gofmt` du Makefile limité hors `.claude/`.
- **Vérification** : `make check` ✅ après chaque merge (Postgres.app). Non vérifié : Docker (build, HTTPS, mémoire), Mailpit, CI.
- **Reste / reprise** : relecture humaine des 5 tâches, puis T014 et T016.
- **Décisions** : 16 entrées dans `decisions.md` (de « IP client via TrustedProxies » à « Migrations renumérotées ») ; `AGENTS.md` : `auth → service` autorisé.

## 2026-10-15 — 10 premières tâches (exécutant : sonnet ; contrôle : opus)

- **Tâches** : T001, T002, T003, T004, T005, T010, T011, T013, T020, T045 → done ; créées : T024 (reste de T020), T046 (reste de T045)
- **Fait** : phase 0 complète ; schéma v1 + EXCLUDE buffers inclus ; moteur de créneaux ; auth praticien ; outbox + worker ; socle métriques. Exécution en parallèle dans des worktrees, merge `--no-ff` par l'orchestrateur.
- **Revue** : 2 corrections renvoyées et faites — T005 (course `CREATE EXTENSION` entre process `go test`), T020 (double envoi possible quand un lot dépasse `StuckAfter`). Vérifications Opus : `make check` après chaque merge, `-race` sur store/outbox, page d'accueil dans le navigateur (console, CSP), login/logout de bout en bout en curl.
- **Vérification** : `make check` ✅ (avec `DOCTOLIBRE_TEST_DATABASE_URL` sur Postgres.app 17). Non vérifié : Docker (`make db-up`), CI (pas de remote), Lighthouse.
- **Reste / reprise** : T012 en priorité (chemin du MVP), puis T021/T022, T024, T040. IP client derrière Caddy à traiter dans T040.
- **Décisions** : 10 entrées dans `decisions.md` (2026-10-15, de « go.mod en go 1.26.0 » à « Travail parallèle »)

## 2026-10-15 — Répartition Opus / Sonnet (Claude Opus)

- **Tâches** : toutes (champ **Exécution** ajouté)
- **Fait** : sous-agent `.claude/agents/task-implementer.md` (Sonnet) ; commande `.claude/commands/run-task.md` (Opus : choisir, déléguer, contrôler, clore) ; section « Rôles » dans `AGENTS.md` ; revue renforcée pour T010, T011, T013, T014, T020, T043.
- **Vérification** : n/a (pas de code) — l'agent et la commande ne sont pas encore testés en réel.
- **Reste / reprise** : `git init` (prérequis du contrôle par `git diff`), puis `/run-task T001`.
- **Décisions** : répartition Opus/Sonnet

## 2026-10-15 — Suivi de l'avancement multi-session (Claude Opus)

- **Tâches** : aucune (documentation)
- **Fait** : création de `docs/README.md` (index), `docs/progress/STATUS.md` (tableau de bord), ce journal ; section « Suivi » ajoutée à chaque tâche ; statuts `in_progress`, `blocked`, `review` ajoutés ; workflow de début/fin de session dans `AGENTS.md`.
- **Vérification** : n/a (pas de code)
- **Reste / reprise** : `git init`, puis T001.
- **Décisions** : aucune

## 2026-10-15 — Intégration de la review externe (Claude Opus)

- **Tâches** : T005, T010, T013, T014, T016, T020, T023, T032, T033, T040 (spécifications modifiées)
- **Fait** : contrainte d'exclusion sur `during_blocked` (buffers inclus) et `status <> 'cancelled'` ; outbox liée au RDV par `appointment_id` ; index partiels ; tests DB par schéma ; dépendances T033→T021 et T040→T010.
- **Vérification** : n/a
- **Reste / reprise** : —
- **Décisions** : 4 entrées dans `decisions.md` (2026-10-15)

## 2026-10-15 — Comparatif SMS (Claude Opus)

- **Tâches** : T022, T023, T044 (spécifications modifiées)
- **Fait** : `docs/sms.md` (OVH ≈ 0,06 € HT, Twilio ≈ 0,07 €, passerelle Android) ; réglages par défaut : confirmation par email, rappel J-1 par SMS.
- **Vérification** : n/a
- **Reste / reprise** : vérifier les prix sur le simulateur OVH avant de chiffrer.
- **Décisions** : stratégie SMS par défaut

## 2026-10-15 — Plan initial (Claude Opus)

- **Tâches** : création de T001–T045
- **Fait** : `PLAN.md`, `AGENTS.md`, `CLAUDE.md`, `docs/decisions.md`, `tasks/`.
- **Vérification** : n/a
- **Reste / reprise** : —
- **Décisions** : stack (Go + Postgres + htmx, pas de Redis, AGPL)
