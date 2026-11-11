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
- [ ] `docker compose build` avec `DOCTOLIBRE_VERSION=test` → `/admin/status` et `doctolibre version` affichent `test`.
- [ ] Image de sauvegarde < 150 Mo, `backup.sh` produit un `.age` et `restore.sh` restaure dans une base vide (commande de `docs/backup.md`), sorties dans Suivi.
- [ ] Mesures mémoire avant/après et taille d'image dans Suivi, avec la décision prise.
- [ ] `docs/install.md` et `docs/backup.md` à jour.

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

- **Sessions** : —
- **Fait** : —
- **Reste à faire** : —
- **Notes de reprise** (pièges, état intermédiaire, commandes utiles) : —
- **Écarts avec la spécification** (et pourquoi) : —
