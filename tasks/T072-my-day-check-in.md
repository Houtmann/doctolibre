# T072 — « Ma journée » : tournée domicile et pointage en un clic

> UI à faire en daisyUI ; à lancer après T080 (décision du 2026-10-16).

- **Phase** : 5
- **Dépend de** : T016, T032, T033
- **Taille estimée** : M
- **Exécution** : sonnet (`task-implementer`) — revue Opus : standard

## Objectif
Sur son téléphone, le praticien ouvre une page « Ma journée » : la liste chronologique de ses RDV du jour (adresses et liens de navigation pour le domicile, téléphone cliquable), et marque chaque séance « faite » ou « absent » d'un clic.

## Contexte à lire
`internal/service/calendar.go` (`View`, `SetStatus`, `Detail`, `ViewablePractitioners`), `internal/web/calendar.go` + `routes.go`, `internal/web/access.go` (`Actor`), `admin_calendar.html` / `admin_appointment.html` (badge domicile, liens `geo:` / OpenStreetMap de T032), `docs/design-system.md` (partials, jetons v2), T054 (garde-fou absences : s'appuie sur le statut `no_show`), T059 (remplaçant : accès délégué).

## Spécification
- Route `GET /admin/today` (lien « Ma journée » en tête de la navigation pour les rôles praticien et admin-praticien ; le secrétariat y accède avec un sélecteur de praticien parmi `ViewablePractitioners`). Paramètre `?date=AAAA-MM-JJ` optionnel (jour précédent / suivant), jour courant calculé dans le fuseau du cabinet avec le `Clock` injecté.
- Service : `Calendar.Day(ctx, actor, practitionerID, date)` réutilisant les requêtes de `View` (pas de nouvelle requête si `View` suffit) ; RDV non annulés triés par début ; pour chacun : heure, durée, motif, patient (nom + téléphone), statut, badge série « séance n/N », et pour le domicile l'adresse + trajet réservé.
- Rendu mobile-first (360 px), une carte par RDV :
  - domicile : adresse, lien `geo:` et lien OpenStreetMap ; en tête de page, nombre de visites à domicile et lien « itinéraire de la journée » OpenStreetMap avec les adresses dans l'ordre (aucune API cartographique, aucun géocodage) ;
  - lien `tel:` sur le téléphone ;
  - boutons « Fait » et « Absent » (`<button>` dans un `<form method="post">`, CSRF), `POST /admin/appointments/{id}/status` existant ou nouvelle route fine qui appelle **`Calendar.SetStatus`** (aucune règle dupliquée). Avec htmx : la carte seule est remplacée (`hx-target` sur la carte, partial `day_card.html`) ; sans JS : redirect vers `/admin/today?date=…`.
  - annuler le pointage (revenir à `booked`) depuis la carte, si `SetStatus` l'autorise ; sinon le documenter.
- Migration (prochain numéro libre) : `appointment.status_changed_by uuid NULL REFERENCES app_user(id)` et `appointment.status_changed_at timestamptz NULL`, renseignés par `SetStatus` (utilisateur de l'`Actor`, y compris un remplaçant T059). Sert aux statistiques T073 ; vérifier le nom réel de la table des utilisateurs.
- Récapitulatif en bas de page : faites / absents / à venir du jour.
- Accès : mêmes règles que l'agenda (`MayAccessPractitioner`, délégations T059 si mergée) ; autre praticien non autorisé → 403.

## Critères d'acceptation
- [x] La page liste les RDV du jour du praticien connecté dans l'ordre, sans les annulés ; navigation jour précédent/suivant.
- [x] Jour courant correct à 00:30 heure de Paris (horloge fixe), et le jour du changement d'heure.
- [x] Pointage « Fait » / « Absent » en htmx (partial seul) et sans JS (redirect) ; `status_changed_by` = utilisateur connecté.
- [x] Domicile : adresse, liens `geo:` / OSM, lien d'itinéraire avec les adresses dans l'ordre.
- [x] Secrétariat : sélecteur de praticien ; praticien sans droit → 403.
- [ ] Rendu 360 px vérifié dans `/dev/styleguide` ou capture.

## Vérification
```bash
make sqlc
make check
```

## Hors scope
Optimisation de l'ordre de tournée, géocodage, carte embarquée, mode hors ligne, statistiques (T073).

## Suivi

> Rempli par l'agent pendant et à la fin de chaque session sur cette tâche. Le statut officiel est dans `tasks/README.md`.

- **Sessions** : 2026-10-17 (sonnet).
- **Fait** : migration 00022 (`status_changed_by`, `status_changed_at`) ; `SetAppointmentStatus` les renseigne (utilisateur de l'`Actor`, `Clock`) ; requête `ListDayAppointments` (téléphone, adresse, rang `n/N` dans la série, trajet) ; service `Calendar.Day` (fichier `internal/service/calendar_day.go`, types `MyDay*`) ; handlers `internal/web/today.go` (`GET /admin/today`, `POST /admin/today/appointments/{id}/status` qui appelle `Calendar.SetStatus`) ; templates `admin_today`, partials `day_card`, `day_status_form`, `day_summary`, `day_card_update` (carte + récapitulatif hors-bande) ; lien « Ma journée » en tête du menu ; tests service (DST 25/10/2026 et 28/03/2027, 00:30 Paris, tri, annulés exclus, status_changed_by, 403) et web (page, htmx, redirect, 403/400/404, CSRF) ; `make css`.
- **Reste à faire** : vérification visuelle à 375 px dans un navigateur (non faite) ; l'URL d'itinéraire OSM `directions?route=adr1;adr2` : à confirmer que OSM géocode bien des adresses textuelles.
- **Notes de reprise** : lien visible pour les trois rôles ; le secrétariat voit un sélecteur si plusieurs praticiens (sinon l'unique praticien). « Annuler le pointage » = `SetStatus(booked)`, autorisé par `SetStatus`.
- **Écarts avec la spécification** : nouvelle requête nécessaire (`View`/`AgendaDay` n'a ni téléphone, ni adresse, ni rang de série) ; méthode nommée `Day` avec types `MyDay*` (`DayView` existait déjà) ; critère « rendu 360 px » non coché.
