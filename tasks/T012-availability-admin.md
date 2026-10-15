# T012 — Paramétrage cabinet, motifs, disponibilités

- **Phase** : 1
- **Dépend de** : T011
- **Taille estimée** : L
- **Exécution** : sonnet (`task-implementer`) — revue Opus : standard

## Objectif
Depuis `/admin`, le praticien configure son cabinet, ses motifs de consultation, ses horaires hebdomadaires et ses absences.

## Contexte à lire
`AGENTS.md` (Front, Handlers), `internal/web/routes.go`, `internal/web/templates/layout/admin.html`, tables `practice`, `practitioner`, `appointment_type`, `availability_rule`, `availability_exception`.

## Spécification
Pages (htmx pour les formulaires inline, mais **chaque page fonctionne aussi sans JS**) :
- `/admin/settings` : nom, adresse, téléphone, délais (notice mini de réservation, d'annulation, horizon). Profil public du praticien connecté (nom, titre, bio, slug).
- `/admin/appointment-types` : liste triable (position), création/édition/désactivation. Préremplir à la création du cabinet : « Bilan initial » 45 min, « Séance » 30 min, « Séance à domicile » 30 min (at_home, non réservable en ligne par défaut).
- `/admin/availability` : grille lundi→dimanche ; ajout/suppression de plages (ex. 08:00–12:00, 14:00–19:00) ; motifs autorisés par plage (défaut tous).
- `/admin/absences` : ajout d'absences (date+heure début/fin ou journées entières), liste à venir, suppression.
- `internal/service/practice.go` : validations métier (plages non chevauchantes le même jour pour un même praticien, durée > 0, etc.). Erreurs typées → affichées dans le formulaire.

## Critères d'acceptation
- [ ] Un praticien neuf peut, en < 5 min, configurer horaires + motifs.
- [ ] Deux plages qui se chevauchent le même jour → erreur lisible.
- [ ] Tests service (validation) + tests handler (POST valide/invalide, CSRF).

## Vérification
```bash
make check
```

## Hors scope
Calcul des créneaux (T013), multi-praticiens (T033).

## Suivi

> Rempli par l'agent pendant et à la fin de chaque session sur cette tâche. Le statut officiel est dans `tasks/README.md`.

- **Sessions** : —
- **Fait** : —
- **Reste à faire** : —
- **Notes de reprise** (pièges, état intermédiaire, commandes utiles) : —
- **Écarts avec la spécification** (et pourquoi) : —
