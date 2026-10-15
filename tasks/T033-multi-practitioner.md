# T033 — Multi-praticiens et rôles

- **Phase** : 3
- **Dépend de** : T016, T021
- **Taille estimée** : L
- **Exécution** : sonnet (`task-implementer`) — revue Opus : standard

## Objectif
Un cabinet de groupe (2–10 kinés + secrétariat) partage une instance : chacun gère son agenda, le secrétariat gère tout.

## Contexte à lire
`internal/auth/`, middleware `RequireUser`, `internal/web/routes.go`, handlers admin.

## Spécification
- Matrice de permissions dans `internal/auth/policy.go` (fonction pure `Can(user, action, resource) bool`, testée en table) :
  - `admin` : tout ; `secretary` : agendas et patients de tous, pas les réglages ; `practitioner` : son agenda, patients du cabinet en lecture/écriture, ses dispos.
- `/admin/team` (admin) : inviter un membre (email avec lien d'activation 48 h, via T021), changer le rôle, désactiver.
- Agenda : sélecteur de praticien(s) ; vue « jour » multi-colonnes (une colonne par praticien).
- Page publique : liste des praticiens + option « premier disponible » pour un motif commun.
- **Toutes** les requêtes `store` filtrent par `practice_id` (vérifier l'existant ; ajouter un test qui tente l'accès cross-cabinet).

## Critères d'acceptation
- [ ] Un praticien ne peut pas modifier les dispos d'un autre (test 403).
- [ ] Vue jour à 4 praticiens lisible sur 1280 px.

## Vérification
```bash
make check
```

## Hors scope
Multi-cabinets sur une instance (hébergement mutualisé) — non prévu.

## Suivi

> Rempli par l'agent pendant et à la fin de chaque session sur cette tâche. Le statut officiel est dans `tasks/README.md`.

- **Sessions** : —
- **Fait** : —
- **Reste à faire** : —
- **Notes de reprise** (pièges, état intermédiaire, commandes utiles) : —
- **Écarts avec la spécification** (et pourquoi) : —
