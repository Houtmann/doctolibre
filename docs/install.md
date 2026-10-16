# Installer Doctolibre

Objectif : une instance en production en environ 30 minutes, sans être développeur. Il faut savoir ouvrir un terminal et copier-coller des commandes.

Résumé : un serveur Linux, un nom de domaine, Docker. Tout tourne dans 3 conteneurs (application, PostgreSQL, Caddy qui gère le HTTPS) + un 4e pour les sauvegardes.

> **Responsabilité.** En hébergeant vous-même Doctolibre, vous êtes responsable du traitement des données de vos patients (un rendez-vous chez un kiné est une donnée de santé indirecte). Doctolibre limite les données collectées (pas de dossier médical, pas de motif médical libre côté patient) et chiffre les sauvegardes, mais les obligations RGPD restent les vôtres, et la certification HDS s'applique si un tiers héberge pour votre compte (voir [`../PLAN.md`](../PLAN.md) §4). Ce document est technique, ce n'est pas un conseil juridique : en cas de doute, rapprochez-vous de votre ordre professionnel ou d'un juriste.

## 1. Choisir un hébergement

| Option | Coût indicatif | Pour qui |
|---|---|---|
| VPS Hetzner (CX22 ou équivalent), OVH VPS, Scaleway (petite instance) | environ 4 à 6 €/mois | Cas général : toujours en ligne, IP fixe, rien à entretenir côté matériel |
| Raspberry Pi 4/5 (4 Go) au cabinet + nom de domaine | environ 80 € une fois + électricité | À l'aise avec sa box internet (redirection de ports, IP changeante) |
| Mini-PC au cabinet | idem Raspberry Pi | Idem |

Minimum : 1 vCPU, 1 Go de RAM, 10 Go de disque, Debian 12 ou Ubuntu 22.04/24.04 (amd64 ou arm64 : l'image est publiée pour les deux). Au repos l'application tient dans quelques dizaines de Mo.

Choisissez de préférence un hébergeur dans l'Union européenne. Les coûts annuels totaux sont détaillés dans [`cost.md`](cost.md).

Au cabinet : il faut une box qui redirige les ports 80 et 443 vers la machine, et un nom de domaine qui suit votre IP (DNS dynamique). C'est le point le plus fragile ; un VPS est plus simple.

## 2. Nom de domaine et DNS

1. Achetez un domaine (environ 8 à 15 €/an) chez le registrar de votre choix, ou utilisez un domaine que vous possédez déjà. Exemple dans la suite : `rdv.cabinet-dupont.fr`.
2. Chez le gestionnaire DNS du domaine, créez :
   - un enregistrement **A** : `rdv` -> IPv4 de votre serveur ;
   - si le serveur a une IPv6, un enregistrement **AAAA** correspondant.
3. Attendez la propagation (quelques minutes, parfois une heure) : `dig +short rdv.cabinet-dupont.fr` doit renvoyer l'IP du serveur.
4. **Email** : pour que les confirmations n'aillent pas en spam, publiez SPF, DKIM et DMARC sur le domaine de l'expéditeur. La marche à suivre est dans [`email.md`](email.md#pour-que-les-emails-naillent-pas-en-spam--spf-dkim-dmarc) ; les valeurs exactes viennent de votre fournisseur d'envoi (OVH, Brevo...).

## 3. Installer et démarrer

Sur le serveur, en SSH.

**Pare-feu** : ouvrez les ports 22 (SSH), 80 et 443 (TCP) et 443 (UDP, HTTP/3). Le port 8080 de l'application n'est jamais publié : seul Caddy est exposé.

**Docker** (Debian/Ubuntu) :

```bash
curl -fsSL https://get.docker.com | sh      # installe Docker et le plugin compose
sudo usermod -aG docker "$USER"             # puis se déconnecter / reconnecter
docker compose version                      # doit répondre
```

**Récupérer le projet** :

```bash
git clone <URL-DU-DEPOT> doctolibre         # l'URL officielle sera indiquée ici une fois le dépôt publié
cd doctolibre/deploy
cp .env.example .env
```

**Remplir `.env`** (éditeur : `nano .env`). Le strict minimum :

| Variable | Valeur |
|---|---|
| `DOMAIN` | `rdv.cabinet-dupont.fr` (sans `https://`) |
| `CADDY_TLS` | **Obligatoire.** Votre adresse email (sert à Let's Encrypt pour le certificat HTTPS). `internal` uniquement pour un test local avec `DOMAIN=localhost` |
| `POSTGRES_PASSWORD` | un mot de passe généré : `openssl rand -hex 24` |
| `DOCTOLIBRE_SECRET_KEY` | un secret de 32 octets minimum : `openssl rand -hex 32`. **Ne le perdez pas et ne le changez pas à la légère.** |
| `DOCTOLIBRE_SMTP_HOST`, `_SMTP_FROM`, ... | voir §5 |
| `DOCTOLIBRE_BACKUP_AGE_RECIPIENT` | clé publique de sauvegarde, voir §6 |

Les autres lignes ont des valeurs par défaut adaptées ; le fichier `.env.example` explique chacune. Ne committez jamais `.env` et protégez-le : `chmod 600 .env`.

Variables utiles, toutes préfixées `DOCTOLIBRE_` :

| Variable | Défaut | Rôle |
|---|---|---|
| `TIMEZONE` | `Europe/Paris` | Fuseau du cabinet (créneaux, affichage) |
| `AUTO_MIGRATE` | `true` | Applique les migrations de base de données au démarrage |
| `WORKER` | `on` | Envoi des emails et SMS dans le même processus. `off` : ne rien envoyer depuis ce processus |
| `METRICS_TOKEN` | vide | Jeton (16 caractères minimum) qui active `/metrics` pour Prometheus ; vide = désactivé |
| `IMAGE` | `ghcr.io/doctolibre/doctolibre:latest` | Image utilisée. Pour figer une version : `ghcr.io/doctolibre/doctolibre:v0.1.0` |

Fixées par `docker-compose.yml`, ne les définissez pas dans `.env` : `ENV=prod`, `HTTP_ADDR`, `BASE_URL` (déduite de `DOMAIN`), `DATABASE_URL`, `TRUSTED_PROXIES` (le sous-réseau Docker `172.30.0.0/24`, pour que le limiteur de débit voie l'IP réelle des visiteurs derrière Caddy).

**Démarrer** :

```bash
docker compose up -d
docker compose ps                  # app (healthy), postgres (healthy), caddy, backup
docker compose logs -f app         # Ctrl+C pour quitter
```

Les migrations s'appliquent toutes seules. Si `DOMAIN`, `CADDY_TLS`, `POSTGRES_PASSWORD` ou `DOCTOLIBRE_SECRET_KEY` manque, `docker compose` refuse de démarrer en le disant.

Ouvrez `https://rdv.cabinet-dupont.fr` : la page d'accueil doit s'afficher en HTTPS. Le premier certificat peut prendre une minute ; si rien ne vient, voir le §8.

> Pas d'image publiée ? Construisez-la localement : `docker compose up -d --build` (compter plusieurs minutes, surtout sur Raspberry Pi).

## 4. Créer le compte administrateur et configurer le cabinet

Il n'y a pas d'inscription publique : le premier compte se crée en ligne de commande, dans le conteneur de l'application.

```bash
docker compose exec app /doctolibre create-user \
  --email vous@cabinet-dupont.fr --role admin --practice-name "Cabinet Dupont"
```

Le mot de passe est demandé au clavier (sans affichage), 12 caractères minimum. Le cabinet est créé s'il n'existe pas ; vous pouvez ajouter d'autres comptes avec la même commande (`--role practitioner` ou `secretary`, même `--practice-name`).

Ensuite, dans le navigateur :

1. Connectez-vous sur `https://<votre domaine>/admin/login`.
2. **Paramètres du cabinet** (`/admin/settings`) : nom, adresse, téléphone ; délai minimal avant un rendez-vous, délai minimal pour annuler, nombre de jours de réservation à l'avance ; les visites à domicile et leurs zones ; votre profil de praticien (prénom, nom, titre, présentation) et l'**identifiant dans l'adresse de réservation**.
3. **Motifs de rendez-vous** (`/admin/appointment-types`) : durée, temps avant/après, réservable en ligne ou non.
4. **Disponibilités** (`/admin/availability`) : vos plages hebdomadaires ; **absences** (`/admin/absences`) pour les congés.
5. **Notifications** (`/admin/settings/notifications`) : canal (email ou SMS) de la confirmation, des rappels et des changements, rappel la veille (et l'heure d'envoi), rappel 2 h avant, rappel pour les séances d'une série, signature des emails. La page estime le nombre de SMS envoyés.
6. Testez comme un patient : la page publique de réservation est `https://<votre domaine>/p/<identifiant-du-cabinet>`.

Les écrans sont décrits ici mais pas illustrés : les captures seront ajoutées quand l'interface sera stabilisée.

Rappel de bon usage : le champ libre côté cabinet est une **note administrative**, n'y écrivez pas d'information médicale.

## 5. Configurer email et SMS

- **Email** (indispensable : confirmations, code de vérification, mot de passe oublié) : [`email.md`](email.md). Renseignez `DOCTOLIBRE_SMTP_HOST`, `_PORT`, `_USER`, `_PASSWORD`, `_FROM` (une adresse sur votre propre domaine) et `_TLS` (`starttls` par défaut) dans `.env`. Des exemples (OVH, Brevo, Gmail) sont dans ce document. `DOCTOLIBRE_EMAIL_DRIVER=log` n'envoie rien : à réserver aux essais.
- **SMS** (rappels) : [`sms.md`](sms.md). Par défaut le driver est `log` et **aucun SMS ne part**. Choisissez `DOCTOLIBRE_SMS_DRIVER=ovh`, `android` ou `http` et remplissez les variables correspondantes. La stratégie d'envoi (rappel de la veille seul par SMS) pèse plus sur la facture que le fournisseur.

Après toute modification de `.env`, appliquez-la : `docker compose up -d` (les services concernés sont recréés). Les variables sont validées au démarrage : une valeur invalide empêche l'application de démarrer, la raison est dans `docker compose logs app`.

Pour vérifier l'envoi : créez un rendez-vous de test avec votre propre adresse, puis ouvrez `/admin/messages` (liste des messages, état, renvoi manuel).

## 6. Sauvegardes et test de restauration

Le détail est dans [`backup.md`](backup.md). En bref :

1. **Sur votre poste** (pas sur le serveur) : `age-keygen -o doctolibre-backup.key` ; il affiche `Public key: age1...`.
2. Dans `.env` : `DOCTOLIBRE_BACKUP_AGE_RECIPIENT=age1...` et `COMPOSE_PROFILES=backup` (déjà présent dans `.env.example`). Rangez `doctolibre-backup.key` dans un gestionnaire de mots de passe **et** sur un support hors ligne : sans elle, les sauvegardes sont inutilisables.
3. `docker compose up -d`, puis sauvegarde immédiate de contrôle : `docker compose exec backup backup.sh`.
4. Recommandé : un envoi hors serveur (stockage S3-compatible, quelques centimes par mois) via `DOCTOLIBRE_BACKUP_S3_*`. Une sauvegarde sur le même disque que la base ne protège pas d'une panne du serveur.
5. **Testez une restauration** maintenant, puis chaque trimestre (commande dans [`backup.md`](backup.md#restaurer)) : une sauvegarde jamais restaurée n'est pas une sauvegarde.

Conservez aussi une copie de votre `.env` (hors serveur, dans le gestionnaire de mots de passe) : il contient `DOCTOLIBRE_SECRET_KEY`.

## 7. Mettre à jour

```bash
cd doctolibre/deploy
git pull                         # met à jour deploy/ (compose, Caddyfile) et la doc
docker compose pull              # nouvelle image de l'application
docker compose up -d             # recrée ce qui a changé ; les migrations s'appliquent
docker compose logs --tail 50 app
```

Avant de mettre à jour : lisez les notes de version de la release (page des releases du dépôt, tags `vX.Y.Z` ; il n'y a pas encore de fichier `CHANGELOG`) et faites une sauvegarde (`docker compose exec backup backup.sh`). Les migrations ne sont jamais modifiées après publication : elles s'ajoutent. Pour figer une version plutôt que `latest`, renseignez `DOCTOLIBRE_IMAGE` dans `.env`.

Si l'image est construite localement : `docker compose up -d --build` après `git pull`.

## 8. Dépannage (FAQ)

| Symptôme | Cause probable et remède |
|---|---|
| `docker compose` dit « CADDY_TLS requis » (ou `DOMAIN`, `POSTGRES_PASSWORD`...) | Variable manquante dans `deploy/.env`. Lancez la commande depuis `deploy/`. |
| Le navigateur affiche une erreur de certificat ou rien du tout | Le DNS ne pointe pas encore vers le serveur, ou les ports 80/443 sont fermés (pare-feu du système, de l'hébergeur ou de la box). Consultez `docker compose logs caddy`. Let's Encrypt limite le nombre d'essais : corrigez avant de relancer en boucle. |
| `app` redémarre en boucle | `docker compose logs app` : la configuration est validée au démarrage (ex. `DOCTOLIBRE_SECRET_KEY` trop courte, SMTP incomplet, driver SMS mal configuré). |
| `app` « unhealthy » | La base n'est pas joignable ou les migrations échouent : `docker compose logs postgres app`. |
| Erreur « subnet overlaps » / conflit de réseau | Le sous-réseau `172.30.0.0/24` est déjà pris sur la machine : changer `subnet` **et** `DOCTOLIBRE_TRUSTED_PROXIES` dans `docker-compose.yml` de manière cohérente. |
| Les patients ne reçoivent pas d'email | `/admin/messages` pour l'état et l'erreur ; vérifiez le SMTP et SPF/DKIM/DMARC ([`email.md`](email.md)) ; regardez les spams. |
| Aucun SMS ne part | `DOCTOLIBRE_SMS_DRIVER` vaut `log` (défaut) ; et vérifiez les réglages de `/admin/settings/notifications`. |
| Trop de requêtes bloquées / tout le monde semble avoir la même IP | `DOCTOLIBRE_TRUSTED_PROXIES` ne correspond pas à votre reverse proxy. Avec le compose fourni, ne changez rien. |
| Mot de passe administrateur oublié | Page « Mot de passe oublié ? » (`/admin/password/forgot`, nécessite l'email configuré), ou recréer un compte avec `create-user`. |
| Changement de nom de domaine | Modifiez `DOMAIN` dans `.env` et le DNS, `docker compose up -d`. Les anciens liens envoyés aux patients pointent vers l'ancien domaine. |
| Disque plein | `docker system df` ; réduisez la rétention des sauvegardes (`BACKUP_KEEP_DAILY`, `BACKUP_KEEP_WEEKLY`). |
| Voir l'état | `docker compose ps`, `curl -fsS https://<domaine>/healthz`, `docker compose logs --tail 100 app`. |

## Sans Docker (avancé)

Binaire + PostgreSQL du système + reverse proxy TLS local (Caddy ou nginx), géré par systemd. Les étapes (utilisateur système, `/etc/doctolibre/doctolibre.env`, unité `doctolibre.service`) sont en commentaire dans [`../deploy/doctolibre.service`](../deploy/doctolibre.service). Les migrations s'appliquent au démarrage (`serve`), ou à la main : `doctolibre migrate up|down|status`. Le compte administrateur se crée avec `doctolibre create-user ...`.

## Aide-mémoire des commandes

| Commande | Rôle |
|---|---|
| `doctolibre serve` | Serveur HTTP (+ worker si `DOCTOLIBRE_WORKER=on`) ; commande par défaut de l'image |
| `doctolibre worker` | Worker d'envoi seul |
| `doctolibre migrate [up\|down\|status]` | Migrations de la base |
| `doctolibre create-user --email ... --role ... --practice-name ...` | Crée un compte (et le cabinet) |
| `doctolibre healthcheck` | Sonde utilisée par Docker |
| `doctolibre version` | Version |
