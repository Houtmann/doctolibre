# Sauvegardes et restauration

Les données d'un cabinet sont des données de santé indirectes (PLAN.md §4) : toute sauvegarde est **chiffrée** avec [age](https://age-encryption.org). Le serveur ne détient que la clé **publique** : un attaquant qui vole le disque ou le bucket ne peut rien lire sans la clé privée, que vous gardez hors du serveur.

## Principe

- Service compose `backup` (image `postgres:17-alpine` + `age` + `rclone`, voir `deploy/backup/`).
- Une fois par jour (`BACKUP_TIME`, UTC, défaut 03:30) : `pg_dump` (format custom) envoyé directement dans `age`. Le dump en clair n'est jamais écrit sur disque.
- Rétention locale (volume `backups`) : 7 quotidiennes (`daily/`) + 4 hebdomadaires (`weekly/`, la sauvegarde du dimanche).
- Envoi optionnel vers un stockage S3-compatible (Scaleway, OVH, Backblaze B2 : quelques centimes par mois). La rétention distante se règle avec une règle de cycle de vie du bucket.
- Choix : scripts shell (`backup.sh`, `restore.sh`) plutôt qu'une sous-commande Go, car `age` en Go demanderait une dépendance hors liste et `pg_dump` est de toute façon requis.

## Mise en place

1. Générer une paire de clés **sur votre poste** (pas sur le serveur) :
   ```bash
   age-keygen -o doctolibre-backup.key      # affiche « Public key: age1... »
   ```
2. Dans `deploy/.env` : `DOCTOLIBRE_BACKUP_AGE_RECIPIENT=age1...` (clé publique) et `COMPOSE_PROFILES=backup`.
3. **Copier `doctolibre-backup.key` dans un gestionnaire de mots de passe et sur un support hors ligne.** Sans elle, les sauvegardes sont inutilisables ; ne jamais la mettre sur le serveur ni dans le dépôt.
4. `docker compose up -d` puis vérifier : `docker compose logs backup`.
5. Sauvegarde immédiate de contrôle : `docker compose exec backup backup.sh` (fichier dans `/backups/daily/`).

Plusieurs destinataires sont possibles (clés publiques séparées par des espaces) : pratique pour avoir une clé de secours.

### Envoi hors serveur (recommandé)

Renseigner `DOCTOLIBRE_BACKUP_S3_ENDPOINT`, `_BUCKET`, `_ACCESS_KEY`, `_SECRET_KEY` (et `_REGION`, `_PREFIX`) dans `.env`. Utiliser une clé d'accès limitée à l'écriture sur ce bucket. Un échec d'envoi est journalisé et fait échouer le script, mais la copie locale est conservée. Les identifiants ne sont jamais écrits dans les journaux.

Sans Docker (systemd) : installer `postgresql-client`, `age`, `rclone`, puis appeler `deploy/backup/backup.sh` depuis un timer ou une ligne cron, avec `BACKUP_DATABASE_URL`, `BACKUP_DIR` et les variables ci-dessus dans l'environnement.

## Restaurer

Sur une machine disposant de `age`, `pg_restore` et de la clé privée (le serveur n'en a pas besoin) :

```bash
# 1. Récupérer une sauvegarde (volume local ou bucket) :
docker compose cp backup:/backups/daily/doctolibre-AAAAMMJJ-HHMMSS.dump.age .
# 2. Créer une base vierge et y restaurer :
createdb -h HOTE -U doctolibre doctolibre_restore
deploy/backup/restore.sh --in doctolibre-AAAAMMJJ-HHMMSS.dump.age \
  --identity doctolibre-backup.key \
  --target 'postgres://doctolibre:MOT_DE_PASSE@HOTE:5432/doctolibre_restore'
```

Remise en service après sinistre : stopper `app`, restaurer dans la base de production (vide), relancer `app` (les migrations sont rejouées si besoin). Le script restaure la table de version des migrations avec les données. La cible est toujours explicite (`--target`) : le script ne devine jamais la base de production, et `--clean --if-exists` le rend rejouable.

**Testez vos restaurations** : une sauvegarde jamais restaurée n'est pas une sauvegarde. Faites l'exercice après la mise en place, puis chaque trimestre.

## Test automatisé

```bash
make backup-test   # nécessite Docker
```

Démarre un Postgres jetable, remplit des tables, sauvegarde, vérifie que le fichier est illisible (ni contenu ni en-tête `PGDMP` en clair) et refusé par une mauvaise clé, restaure dans une base vierge, compare les nombres de lignes et la contrainte d'exclusion, vérifie la rétention (7 + 4).

## Réglages

| Variable | Défaut | Rôle |
|---|---|---|
| `COMPOSE_PROFILES` | `backup` (dans `.env.example`) | Active le service `backup` |
| `DOCTOLIBRE_BACKUP_AGE_RECIPIENT` | — | Clé(s) publique(s) age (obligatoire) |
| `BACKUP_TIME` | `03:30` | Heure UTC de la sauvegarde quotidienne |
| `BACKUP_KEEP_DAILY` / `BACKUP_KEEP_WEEKLY` | `7` / `4` | Rétention locale |
| `DOCTOLIBRE_BACKUP_S3_*` | — | Envoi S3 optionnel |
