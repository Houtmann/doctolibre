# T017 — Fiches patients

- **Phase** : 1
- **Dépend de** : T016
- **Taille estimée** : M
- **Exécution** : sonnet (`task-implementer`) — revue Opus : standard

## Objectif
Le cabinet retrouve un patient, voit ses RDV passés/futurs, corrige ses coordonnées et fusionne les doublons.

## Contexte à lire
Table `patient`, `internal/store/queries/patient.sql`, templates admin.

## Spécification
- `GET /admin/patients?q=` : recherche par nom/prénom/téléphone/email, insensible à la casse **et aux accents** (`unaccent` + index trigram `pg_trgm` — migration de cette tâche), pagination keyset (25/page).
- `GET /admin/patients/{id}` : coordonnées, préférences de notification, RDV à venir et historique (avec nb d'absences), note administrative courte (avertissement visible : « pas de données médicales »).
- `POST /admin/patients/{id}` : édition.
- `POST /admin/patients/{id}/merge?into={id2}` : transaction, réassigne les RDV, soft-delete du doublon, audit.
- Détection de doublon à la création (même téléphone ou même email) → proposition.

## Critères d'acceptation
- [ ] Recherche « helene » trouve « Hélène ».
- [ ] `EXPLAIN` de la requête de recherche utilise l'index trigram (noter le plan dans la PR).
- [ ] Fusion testée (RDV réassignés, audit écrit).

## Vérification
```bash
make check
```

## Hors scope
Export / suppression RGPD (T043).

## Suivi

> Rempli par l'agent pendant et à la fin de chaque session sur cette tâche. Le statut officiel est dans `tasks/README.md`.

- **Sessions** : —
- **Fait** : —
- **Reste à faire** : —
- **Notes de reprise** (pièges, état intermédiaire, commandes utiles) : —
- **Écarts avec la spécification** (et pourquoi) : —
