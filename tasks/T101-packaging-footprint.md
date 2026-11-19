# T101 — Empreinte du déploiement : version, image de sauvegarde, mémoire

- **Phase** : 5
- **Dépend de** : T040, T041
- **Taille estimée** : S (< 200)
- **Exécution** : sonnet (`task-implementer`) — revue Opus : standard

## Objectif
Ramener le déploiement Docker aux cibles de T040 et corriger les écarts mesurés le 2026-11-11 (journal) : image app 32,6 Mo (cible ≤ 30), app ~105 Mio de mémoire (cible < 50 Mo), image de sauvegarde 585 Mo, version « dev » sur une image construite localement.

## Contexte à lire
- `deploy/Dockerfile`, `deploy/backup/Dockerfile`, `deploy/backup/*.sh`, `deploy/docker-compose.yml`, `deploy/.env.example`
- `Makefile` (l. 1-2, `VERSION`, `LDFLAGS`), `.github/workflows/release.yml` (job `image`)
- `docs/install.md`, `docs/backup.md`

## Spécification
1. **Version** : dans `docker-compose.yml`, `build.args: VERSION: ${DOCTOLIBRE_VERSION:-local}` ; documenter dans `docs/install.md` (§ « Pas d'image publiée ? ») `DOCTOLIBRE_VERSION=$(git describe --tags --always) docker compose up -d --build`. La release (build-arg du tag) est inchangée.
2. **Image de sauvegarde** : partir de `alpine` (même version que l'actuelle base alpine de `postgres:17-alpine`) avec `apk add --no-cache postgresql17-client age rclone` au lieu de `postgres:17-alpine`. Garder l'utilisateur non-root (créer `postgres` ou un utilisateur dédié avec le même UID que les fichiers existants du volume `backups`, 70), `VOLUME /backups`, l'entrypoint. Vérifier que `pg_dump --version` est bien en 17.x. Si `rclone` reste l'essentiel du poids, le noter dans Suivi sans chercher plus loin.
3. **Mémoire de l'app** : ajouter `GOMEMLIMIT` (défaut `40MiB`, surchargeable par `.env`) dans `docker-compose.yml` et `.env.example` (commenté). Mesurer avant/après avec `docker stats` après une réservation et 50 requêtes sur `/admin/calendar` ; reporter les chiffres dans Suivi. Si la mémoire réelle (RSS Go, `/metrics` `go_memstats_*` ou `process_resident_memory_bytes`) est déjà < 50 Mo et que l'écart venait du cache de pages, le dire et ne rien changer d'autre.
4. **Taille de l'image app** : mesurer avec `docker image inspect -f '{{.Size}}'` (pas la colonne de `docker images`, qui inclut les attestations) ; si > 30 Mo, regarder ce qui pèse (`go tool nm -size -sort size` ou `goweight` non installé : utiliser `go version -m` + `du` des assets embarqués) et ne corriger que ce qui est évident (assets non minifiés, fichiers inutiles embarqués). Sinon, ajuster la cible dans `docs/install.md`/T040 avec la mesure.

## Critères d'acceptation
- [x] `docker compose build` avec `DOCTOLIBRE_VERSION=test` → `/admin/status` et `doctolibre version` affichent `test`.
- [ ] Image de sauvegarde < 150 Mo, `backup.sh` produit un `.age` et `restore.sh` restaure dans une base vide (commande de `docs/backup.md`), sorties dans Suivi.
- [x] Mesures mémoire avant/après et taille d'image dans Suivi, avec la décision prise.
- [x] `docs/install.md` et `docs/backup.md` à jour.

## Vérification
```bash
make check
cd deploy && docker compose build && docker image inspect -f '{{.Size}}' doctolibre-backup:local
```

## Hors scope
- Changer de base d'image pour l'app (distroless reste).
- Modifier le code Go applicatif (hors éventuel retrait d'un asset embarqué inutile).

## Suivi

> Rempli par l'agent pendant et à la fin de chaque session sur cette tâche. Le statut officiel est dans `tasks/README.md`.

- **Sessions** : 2026-11-19 (sonnet)
- **Fait** :
  - Version : `build.args.VERSION: ${DOCTOLIBRE_VERSION:-local}` (compose) ; `docs/install.md` documente `DOCTOLIBRE_VERSION=$(git describe --tags --always) docker compose up -d --build`. Vérifié avec `DOCTOLIBRE_VERSION=test` : `doctolibre version` -> `test`, `/admin/status` -> `Version: test`.
  - Image de sauvegarde : `FROM alpine:3.24` (postgres:17-alpine est en alpine 3.24.4) + `apk add postgresql17-client age rclone` ; `postgresql17-client` existe en 3.24 (17.12-r0) et crée lui-même l'utilisateur `postgres` UID/GID 70 (pas de adduser nécessaire). `pg_dump --version` -> 17.12. `docs/backup.md` mis à jour.
  - Mémoire : `GOMEMLIMIT: ${GOMEMLIMIT:-40MiB}` dans compose, `#GOMEMLIMIT=40MiB` et `#DOCTOLIBRE_VERSION` commentés dans `.env.example`.
- **Mesures** (arm64, OrbStack ; login + 50 x `/admin/calendar` + 50 x `/`) :
  - Avant (`GOMEMLIMIT=off`) : `docker stats` 96,8 Mio (98,3 Mio après 20 s de repos) ; `/metrics` : RSS 116 Mo, `go_memstats_alloc_bytes` 90,9 Mo, `heap_inuse` 96,4 Mo, `next_gc` 175 Mo. Ce n'est PAS le cache de pages : le tas Go est réellement gonflé, la cause est le login (argon2id alloue ~64 Mio par vérification, laissés en garbage tant que le GC ne se déclenche pas, `next_gc` à 175 Mo).
  - Après (`GOMEMLIMIT=40MiB`, défaut) : `docker stats` 33,7 Mio (34,6 Mio après 15 s) ; démarrage à 31 Mio ; `/metrics` RSS 49,8 Mo (pic juste après la charge), `alloc` 23,8 Mo, `heap_released` 70 Mo. Cible < 50 Mo tenue ; le login argon2 provoque un pic transitoire (RSS ~50 Mo) puis le GC rend la mémoire.
  - Décision : GOMEMLIMIT 40MiB conservé (limite souple, `mem_limit: 256m` inchangé).
- **Taille image app** : `docker image inspect -f '{{.Size}}'` = 32 592 432 o (32,6 Mo), dont 18,7 Mo de binaire (couche `COPY`), le reste = base distroless. Assets embarqués ~0,8 Mo (templates 400 Ko, ui.css 108 Ko, htmx 52 Ko, vendor 160 Ko, migrations 124 Ko) : rien d'évident à retirer. Aucun changement ; la cible « <= 30 Mo » de T040 est irréaliste, proposer ~35 Mo.
- **Taille image de sauvegarde** : 584 930 044 o avant -> 198 658 073 o après (`docker image inspect`, OrbStack). Couches : alpine 10,3 Mo + apk 136 Mo (dont `rclone` 110 Mo, `age` 4,7 Mo, `pg_dump`/`psql` ~1 Mo). Le critère « < 150 Mo » n'est donc PAS atteint par la colonne `inspect` (la somme des couches fait ~146 Mo, le chiffre 199 Mo est ce que renvoie OrbStack) ; `rclone` fait l'essentiel du poids, non cherché plus loin (consigne). Question à l'orchestrateur ci-dessous.
- **Sauvegarde/restauration** : `backup.sh` dans le conteneur -> `doctolibre-…dump.age` (90 673 o), utilisateur du conteneur uid 70. La clé privée de l'exploitant n'étant pas disponible, j'ai généré une clé age jetable et passé son destinataire à `backup.sh` (`exec -e DOCTOLIBRE_BACKUP_AGE_RECIPIENT=…`). Restauration : `createdb doctolibre_restore` puis `restore.sh --in … --identity … --target postgres://…/doctolibre_restore` exécuté DANS le conteneur backup (pas de `pg_restore` sur l'hôte) -> « restauration terminée » ; comptages identiques base source / restaurée : practice 1, app_user 1, goose_db_version 29. Aucune permission refusée.
- **Reste à faire** : rien d'obligatoire. Arbitrage sur le critère 150 Mo (ci-dessous).
- **Notes de reprise** : pile de test `-p doctolibre-t101` supprimée (`down -v`), `deploy/.env` copié puis supprimé. Une pile `doctolibre-test` existante occupe le sous-réseau 172.30.0.0/24 : j'ai utilisé un override compose (sous-réseau 172.31.77.0/24, ports 18080/18443) hors dépôt. Rappel : la base compose n'est pas multi-pile sans override à cause de ce sous-réseau fixe.
- **Écarts avec la spécification** : image de sauvegarde 199 Mo (inspect) au lieu de < 150 Mo, à cause de rclone ; GOMEMLIMIT efficace car le surplus venait du tas Go (argon2), pas du cache.
