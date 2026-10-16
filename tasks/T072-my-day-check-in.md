# T072 — « Ma journée » : tournée domicile et pointage en un clic

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
- [ ] La page liste les RDV du jour du praticien connecté dans l'ordre, sans les annulés ; navigation jour précédent/suivant.
- [ ] Jour courant correct à 00:30 heure de Paris (horloge fixe), et le jour du changement d'heure.
- [ ] Pointage « Fait » / « Absent » en htmx (partial seul) et sans JS (redirect) ; `status_changed_by` = utilisateur connecté.
- [ ] Domicile : adresse, liens `geo:` / OSM, lien d'itinéraire avec les adresses dans l'ordre.
- [ ] Secrétariat : sélecteur de praticien ; praticien sans droit → 403.
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

- **Sessions** : —
- **Fait** : —
- **Reste à faire** : —
- **Notes de reprise** (pièges, état intermédiaire, commandes utiles) : —
- **Écarts avec la spécification** (et pourquoi) : —
