# T102 — Sous-réseau Docker configurable (plusieurs piles par machine)

- **Phase** : 5
- **Dépend de** : T101
- **Taille estimée** : S (< 200 lignes)
- **Exécution** : sonnet (`task-implementer`) — revue Opus : standard

## Objectif
Le sous-réseau interne de `deploy/docker-compose.yml` n'est plus figé à `172.30.0.0/24` : une variable de `.env` le change, et `DOCTOLIBRE_TRUSTED_PROXIES` suit automatiquement. Deux piles (projets compose différents) tournent sur une même machine sans fichier d'override.

## Contexte à lire
- `deploy/docker-compose.yml` (réseau `internal`, service `app` : `DOCTOLIBRE_TRUSTED_PROXIES`)
- `deploy/.env.example` (commentaire `DOCTOLIBRE_TRUSTED_PROXIES`)
- `docs/install.md` (§ variables fixées par compose, § 8 FAQ « subnet overlaps »)

## Spécification
1. Nouvelle variable `DOCTOLIBRE_SUBNET`, défaut `172.30.0.0/24` (comportement inchangé sans `.env`) :
   - `networks.internal.ipam.config[0].subnet: ${DOCTOLIBRE_SUBNET:-172.30.0.0/24}`
   - service `app` : `DOCTOLIBRE_TRUSTED_PROXIES: ${DOCTOLIBRE_SUBNET:-172.30.0.0/24}`.
2. `.env.example` : ligne commentée `#DOCTOLIBRE_SUBNET=172.30.0.0/24` avec une phrase (« à changer pour faire tourner une seconde pile sur la même machine, ex. 172.31.0.0/24 ; les proxys de confiance suivent »). Mettre à jour le commentaire existant de `DOCTOLIBRE_TRUSTED_PROXIES` s'il dit qu'il faut l'aligner à la main.
3. `docs/install.md` : remplacer la ligne de FAQ « subnet overlaps » (définir `DOCTOLIBRE_SUBNET` dans `.env`, plus besoin d'éditer `docker-compose.yml`) ; ajouter une courte note « Deux instances sur une même machine » : `DOCTOLIBRE_SUBNET` différent, `docker compose -p <nom>` différent, et les ports 80/443 de Caddy, qui ne peuvent être pris que par une pile (renvoyer vers un reverse proxy commun, sans le détailler).
4. Ne pas toucher à `deploy/.env` (fichier local non versionné).

## Critères d'acceptation
- [x] `docker compose config` sans variable : subnet et `DOCTOLIBRE_TRUSTED_PROXIES` = `172.30.0.0/24` (sortie dans Suivi).
- [x] `DOCTOLIBRE_SUBNET=172.31.0.0/24 docker compose config` : les deux valeurs = `172.31.0.0/24`.
- [x] Deux piles réellement lancées en parallèle (`-p doctolibre-t102a` et `-p doctolibre-t102b`, subnets différents, ports Caddy différents via un override hors dépôt si besoin) : les deux `app` sont `healthy` ; puis `down -v` des deux. Si Docker (OrbStack) est indisponible, le dire dans Suivi.
- [x] `docs/install.md` et `.env.example` à jour.

## Vérification
```bash
make check
cd deploy && docker compose config | grep -n "172.30" && DOCTOLIBRE_SUBNET=172.31.0.0/24 docker compose config | grep -n "172.31"
```
(`docker compose config` exige les variables obligatoires : partir d'une copie temporaire de `.env.example` complétée, supprimée ensuite.)

## Hors scope
- Rendre les ports 80/443 configurables, ajouter un reverse proxy partagé.
- Toute modification du code Go.

## Suivi

> Rempli par l'agent pendant et à la fin de chaque session sur cette tâche. Le statut officiel est dans `tasks/README.md`.

- **Sessions** : 2026-11-22 (sonnet)
- **Fait** : `DOCTOLIBRE_SUBNET` (défaut 172.30.0.0/24) dans `docker-compose.yml` (subnet + `DOCTOLIBRE_TRUSTED_PROXIES`) ; `.env.example` (variable commentée avant le bloc « Fixées », commentaire TRUSTED_PROXIES mis à jour) ; `docs/install.md` (FAQ « subnet overlaps », phrase sur `TRUSTED_PROXIES`, section « Deux instances sur une même machine » avant le §8).
- **Sortie `docker compose config`** (copie temporaire de `.env.example` + variables obligatoires, supprimée) : sans variable : `DOCTOLIBRE_TRUSTED_PROXIES: 172.30.0.0/24` et `- subnet: 172.30.0.0/24` ; avec `DOCTOLIBRE_SUBNET=172.31.0.0/24` : les deux valent `172.31.0.0/24`.
- **Test deux piles (fait ensuite, OrbStack lancé)** : `-p doctolibre-t102a` (172.31.10.0/24, Caddy 18080) et `-p doctolibre-t102b` (172.31.11.0/24, Caddy 18081) : les deux `app` et `postgres` `(healthy)` ; `docker network inspect` : t102a_internal = 172.31.10.0/24, t102b_internal = 172.31.11.0/24 ; `down -v` des deux faits, fichiers temporaires supprimés.
- **Reste à faire** : rien (ancien constat : test non fait faute de démon Docker) : le démon Docker (OrbStack) n'était pas lancé (`docker.sock` absent). À faire avec OrbStack démarré : `-p doctolibre-t102a/b`, subnets 172.31.10.0/24 et 172.31.11.0/24, override `ports: !override` pour Caddy, vérifier les `app` healthy puis `down -v`.
- **Notes de reprise** : `docker compose config` marche sans démon. Pile `doctolibre-test` non touchée.
- **Écarts avec la spécification** : aucun (hors critère 3 non vérifié).
