# T016 — Agenda praticien

- **Phase** : 1
- **Dépend de** : T012, T013
- **Taille estimée** : L
- **Exécution** : sonnet (`task-implementer`) — revue Opus : standard

## Objectif
Le praticien voit et gère son agenda jour/semaine : RDV, absences, création manuelle (patient au téléphone), déplacement, annulation, statut honoré/absent.

## Contexte à lire
`internal/service/booking.go` (si existe, sinon `internal/booking`), layout admin, tables `appointment`, `patient`, `availability_*`.

## Spécification
- `GET /admin/calendar?view=day|week&date=YYYY-MM-DD` : grille CSS (`display:grid`, lignes de 15 min, 07:00–21:00 configurable), RDV positionnés en absolu par calcul serveur (top/height en %). Zones hors dispo grisées. Navigation précédent/suivant/aujourd'hui via htmx (swap de la grille seule).
- Vue mobile : liste chronologique du jour au lieu de la grille (< 640 px, via CSS + template alternatif).
- Clic sur créneau vide → `GET /admin/appointments/new?start=` (modale `<dialog>`) : recherche patient (htmx, 300 ms debounce) ou création rapide, motif, praticien.
  Le personnel **peut** forcer un RDV hors dispo (checkbox « hors horaires ») mais **jamais** en chevauchement de zone bloquée (`during_blocked`, buffers inclus — cf. T010). Création et déplacement passent par la même fonction du store que T014.
- Clic sur RDV → détail : patient, motif, source, historique ; actions : déplacer (choisir nouvelle date/heure), annuler (motif), marquer `done` / `no_show`.
- Indicateur « maintenant » (ligne rouge) dans la vue du jour.
- Rafraîchissement : `hx-trigger="every 60s"` sur la grille (pas de websocket).

## Critères d'acceptation
- [x] Afficher une semaine avec 150 RDV en < 100 ms côté serveur (log de durée).
- [x] Toutes les actions passent par `service` et émettent un `EventSink`.
- [x] Tests handler pour création / déplacement / annulation / conflit.
- [x] Utilisable au clavier (tab vers chaque RDV, Entrée ouvre le détail).

## Vérification
```bash
make check
```

## Hors scope
Drag & drop (plus tard, éventuellement), multi-praticiens côte à côte (T033), séries (T030).

## Suivi

> Rempli par l'agent pendant et à la fin de chaque session sur cette tâche. Le statut officiel est dans `tasks/README.md`.

- **Sessions** : 2026-10-15 (sonnet).
- **Fait** :
  - Requêtes `internal/store/queries/calendar.sql` (agenda, détail, déplacement, recherche patient, historique audit) ; `store.MoveAppointment` (`internal/store/appointment_move.go`, traduit 23P01 en `ErrSlotTaken`).
  - `service.Calendar` (`internal/service/calendar.go`) : `View` (jour/semaine, grille de lignes de 15 min, bandes hors dispo, ligne « maintenant », fenêtre élargie pour ne jamais cacher un RDV), `Create` (patient existant ou création rapide, hors horaires refusé sauf `Force`), `Move`, `Cancel`, `SetStatus`, `Detail` (historique via `audit_log`), `SearchPatients`, `ActiveTypes`. Chaque action écrit dans `audit_log` (IDs seulement) et émet un événement.
  - `service.EventSink` / `Event` / `NopSink` (`internal/service/events.go`), émis après commit.
  - Handlers `internal/web/calendar.go` ; routes `/admin/calendar`, `/admin/appointments/new`, `POST /admin/appointments`, `/admin/appointments/{id}` (+ `/move`, `/cancel`, `/status`), `/admin/patients/search`.
  - Templates `admin_calendar`, `admin_appointment_new`, `admin_appointment` + partials `calendar_grid`, `appointment_form`, `appointment_detail`, `patient_results`, `status_button` ; CSS dans `app.css`, 8 lignes de JS dans `app.js` (ouvre le `<dialog>` après un swap htmx).
  - Câblage : `Deps.Calendar`, `adminRoutes`, `cmd/doctolibre/serve.go` (avec `NopSink`), lien « Agenda » dans le layout admin, cas ajoutés à `render_test.go`.
  - Tests : `internal/service/calendar_test.go` (cycle de vie, chevauchement avec buffer, hors horaires, autre cabinet → NotFound, semaines DST 25/10 et 29/03, ligne « maintenant », recherche), `internal/web/calendar_test.go` (accès, CSRF, page, création, déplacement, annulation, statut, conflit, 404 inter-cabinets, 150 RDV).
  - Perf : semaine de 150 RDV, page complète (requête + calcul + rendu) ≈ 9 ms (test < 100 ms, meilleur de 3) ; durée loguée à chaque rendu (`agenda rendu`, `duration_ms`).
  - EXPLAIN (base jetable, ~40 000 RDV, 5 praticiens, semaine de 80 RDV du praticien) : `Bitmap Index Scan on appointment_no_overlap` (GiST `practitioner_id, during_blocked`), 0,146 ms d'exécution ; aucun nouvel index ni migration nécessaire (pas de `migrations/00009`).
- **Reste à faire** : rien d'obligatoire. Vérification visuelle dans un navigateur non faite (pas de rendu réel, seulement des tests de HTML).
- **Notes de reprise** (pièges, état intermédiaire, commandes utiles) :
  - Tests DB : l'URL `localhost` échoue (auth par mot de passe sur `::1`), utiliser `DOCTOLIBRE_TEST_DATABASE_URL='postgres://hadrien@127.0.0.1:5432/doctolibre_test?sslmode=disable'`. Postgres.app n'était pas démarré : lancé avec `pg_ctl -D "$HOME/Library/Application Support/Postgres/var-17" start`.
  - CSP : pas de `style=""`. Position des RDV par classes générées `.gr-N`/`.gs-N`/`.gc-N` (fin de `app.css`, 98 lignes, 96 spans, 8 colonnes) ; si la grille dépassait 96 lignes (24 h), il faudrait en générer plus.
  - Les lignes de la grille sont indexées sur l'heure murale (pas sur le temps écoulé) : un jour de changement d'heure, 09:00 tombe sur la même ligne que les autres jours.
  - `normalizePhoneTmp` (service/calendar.go) est un helper temporaire : `TODO(orchestrateur): remplacer par domain.NormalizePhone (T014)`.
- **Écarts avec la spécification** (et pourquoi) :
  - RDV positionnés par lignes de grille CSS (`grid-row`, classes générées) et non par `top/height` en % : un style inline est interdit par la CSP.
  - Pas de couleur par motif sur les RDV (elle exigerait un style inline) : la couleur dépend du statut (à venir / honoré / absent).
  - Les zones hors dispo sont grisées tous motifs confondus (règles hebdo + ouvertures − absences).
  - Le contrôle « hors horaires » (création, déplacement) tient compte du motif et des absences ; la case « Hors horaires » les ignore tous deux, mais jamais un chevauchement de `during_blocked`.
  - Heures de la grille : paramètres `from`/`to` dans l'URL (défaut 7–21), élargis automatiquement pour inclure tous les RDV.
  - Un RDV `done`/`no_show` n'est ni déplaçable ni annulable ; on peut le remettre « à venir ». Le motif d'annulation est facultatif.
  - Le praticien connecté voit l'agenda de son propre praticien ; la fiche d'un RDV est accessible à tout admin/praticien du même cabinet.
  - Le formulaire de création n'a pas de choix de praticien (un seul par utilisateur avant T033).
