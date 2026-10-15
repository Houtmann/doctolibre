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
- [x] Un backup n'est pas lisible sans la clé privée.
- [x] Restauration documentée testée de bout en bout.

## Vérification
```bash
make backup-test
```

## Hors scope
Réplication / haute dispo.

## Suivi

> Rempli par l'agent pendant et à la fin de chaque session sur cette tâche. Le statut officiel est dans `tasks/README.md`.

- **Sessions** : 2026-10-15 (sonnet)
- **Fait** : option « scripts » retenue. `deploy/backup/` (Dockerfile postgres:17-alpine + age + rclone, `backup.sh`, `restore.sh`, `entrypoint.sh` = boucle quotidienne à `BACKUP_TIME`, `test.sh`). Service compose `backup` (profil `backup`, volume `backups`) ; variables dans `deploy/.env.example` (`COMPOSE_PROFILES=backup` + `DOCTOLIBRE_BACKUP_AGE_RECIPIENT` vide à renseigner). Rétention 7 daily + 4 weekly (liens durs), envoi S3 via rclone configuré par variables d'env. `docs/backup.md` + entrée dans `docs/README.md`. `make backup-test` vert (Docker local) : illisible sans la clé, restauration en base vierge, comptes identiques, contrainte EXCLUDE restaurée, rétention.
- **Reste à faire** : vérification manuelle de l'envoi S3 réel (rclone, aucun bucket de test) et d'un `docker compose up` complet avec le profil backup sur un vrai serveur.
- **Notes de reprise** : `make backup-test` exige Docker (construit l'image, Postgres jetable) ; `TEST_TRACE=1` active `set -x` du test. Sans clé publique, le conteneur `backup` sort en erreur (volontaire).
- **Écarts avec la spécification** : pas de sous-commande Go `doctolibre backup/restore` (alternative « script » acceptée par la spec ; évite une dépendance age). Pas de migration.
