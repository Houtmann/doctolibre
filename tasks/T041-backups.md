# T041 — Sauvegardes et restauration

- **Phase** : 4
- **Dépend de** : T040
- **Taille estimée** : S
- **Exécution** : sonnet (`task-implementer`) — revue Opus : standard

## Objectif
Des sauvegardes chiffrées, automatiques, testables, envoyables hors du serveur à moindre coût.

## Contexte à lire
`deploy/docker-compose.yml`, `PLAN.md` §4.

## Spécification
- Sous-commande `doctolibre backup --out <file>` : `pg_dump` format custom (le binaire `pg_dump` est dans un service compose `backup` basé sur `postgres:17-alpine`), chiffré avec **age** (clé publique en config `DOCTOLIBRE_BACKUP_AGE_RECIPIENT`).
  Alternative plus simple acceptée : script `deploy/backup.sh` lancé par un service compose cron (ex. `mcuadros/ofelia`) ou un timer systemd. Choisir et documenter.
- Rétention locale : 7 quotidiennes + 4 hebdo.
- Envoi optionnel vers un stockage S3-compatible (Scaleway, OVH, Backblaze B2 ≈ quelques centimes/mois) via `rclone` ou upload S3 minimal.
- `doctolibre restore --in <file>` / `deploy/restore.sh` + procédure documentée.
- Test automatisé : dump → restore dans une base vierge → comptes de lignes identiques.

## Critères d'acceptation
- [ ] Un backup n'est pas lisible sans la clé privée.
- [ ] Restauration documentée testée de bout en bout.

## Vérification
```bash
make backup-test
```

## Hors scope
Réplication / haute dispo.

## Suivi

> Rempli par l'agent pendant et à la fin de chaque session sur cette tâche. Le statut officiel est dans `tasks/README.md`.

- **Sessions** : —
- **Fait** : —
- **Reste à faire** : —
- **Notes de reprise** (pièges, état intermédiaire, commandes utiles) : —
- **Écarts avec la spécification** (et pourquoi) : —
