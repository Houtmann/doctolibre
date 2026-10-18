# Doctolibre

Prise de rendez-vous en ligne **libre et auto-hébergeable**, pensée d'abord pour les
**kinésithérapeutes** (cabinet seul ou petit cabinet de groupe).

Un seul binaire Go et une base PostgreSQL. Pas de SaaS obligatoire, pas de compte patient,
pas de Node ni de Redis. Cela tient sur un VPS à 4 à 6 € par mois ou sur un mini-PC au cabinet ;
le budget total, SMS compris, est détaillé dans [`docs/cost.md`](docs/cost.md).

Le logiciel est développé presque entièrement par des assistants LLM, sous la direction et la
relecture d'un humain : voir [Comment ce projet est construit](#comment-ce-projet-est-construit--vibe-coding-orchestré).

![Page de réservation publique : choix du motif](docs/screenshots/01-reservation-choix-motif.png)

## Statut

Projet en **développement actif, pas encore éprouvé en production réelle**. Les phases 0 à 4
(fondations, cœur de la prise de rendez-vous, notifications, spécificités kiné, exploitation) sont
terminées ; la phase 5 (retours terrain) est en cours, avec de nombreuses fonctions en attente de
relecture humaine. Les tests automatisés sont verts, mais plusieurs choses n'ont pas encore été
vérifiées dans des conditions réelles :

- installation complète sur un serveur neuf en suivant [`docs/install.md`](docs/install.md) ;
- construction de l'image Docker, HTTPS via Caddy, reboot de la machine ;
- envois réels d'emails et de SMS de bout en bout (réservation, confirmation, rappel) ;
- import du flux d'agenda (ICS) dans de vrais clients, sauvegardes vers un stockage S3 réel ;
- plusieurs écrans n'ont été vus qu'à certaines tailles d'écran (voir les tâches concernées).

N'hébergez pas de vrais patients sans avoir fait ces vérifications vous-même. Les données des
captures ci-dessous sont entièrement fictives. Les chiffres exacts sont dans
[`tasks/README.md`](tasks/README.md) et l'état courant dans [`docs/progress/STATUS.md`](docs/progress/STATUS.md).

## Fonctionnalités

### Côté patient (sans compte)

Le patient choisit un motif ou un praticien, un créneau, saisit ses coordonnées, confirme par un
code à 6 chiffres (email ou SMS) et reçoit un lien pour déplacer ou annuler son rendez-vous.
Aucune donnée médicale n'est demandée.

| | |
|---|---|
| ![Grille de créneaux de la semaine](docs/screenshots/02-reservation-creneaux.png) | ![Vérification par code à 6 chiffres](docs/screenshots/04-reservation-code-verification.png) |
| Grille de créneaux, calculée avec les marges avant et après chaque soin | Vérification par code, à la place d'un compte |

![Confirmation avec ajout à l'agenda, déplacement et annulation](docs/screenshots/05-reservation-confirmee.png)

Le parcours est pensé pour le téléphone (créneaux, coordonnées, confirmation) :

<p>
  <img src="docs/screenshots/06-mobile-creneaux.png" alt="Mobile : créneaux" width="240">
  <img src="docs/screenshots/07-mobile-coordonnees.png" alt="Mobile : coordonnées" width="240">
  <img src="docs/screenshots/08-mobile-confirmation.png" alt="Mobile : confirmation" width="240">
</p>

Aussi côté patient : lien de gestion du rendez-vous (déplacer, annuler, ajouter à son agenda),
inscription en liste d'attente avec proposition d'un créneau libéré, lien de suivi d'une série de
séances.

### Côté cabinet

![Agenda de la semaine](docs/screenshots/10-agenda-semaine.png)

| | |
|---|---|
| ![Agenda par praticien](docs/screenshots/11-agenda-par-praticien.png) | ![Dialogue d'un rendez-vous](docs/screenshots/12-agenda-dialogue-rendez-vous.png) |
| Agenda du cabinet, une colonne par praticien | Déplacer, annuler ou pointer un rendez-vous |

![Agenda en thème sombre](docs/screenshots/13-agenda-theme-sombre.png)

Thème clair et thème sombre sont fournis (réglage de l'appareil).

| | |
|---|---|
| ![Liste des patients](docs/screenshots/14-patients.png) | ![Fiche patient avec série de séances](docs/screenshots/15-fiche-patient-serie.png) |
| Fiches patients, recherche, import CSV | Séries de séances kiné (nombre prescrit, fréquence) |
| ![Ma journée](docs/screenshots/16-ma-journee.png) | ![Liste d'attente](docs/screenshots/17-liste-attente.png) |
| « Ma journée » : rendez-vous du jour et pointage | Liste d'attente |
| ![Horaires hebdomadaires](docs/screenshots/18-horaires.png) | ![Motifs de consultation](docs/screenshots/19-motifs.png) |
| Horaires d'ouverture, absences, jours fériés | Motifs : durée, marges, réservable en ligne, domicile |

Autres fonctions : plusieurs praticiens et trois rôles (administrateur, praticien, secrétariat),
visites à domicile avec zones, absence imprévue avec annulation groupée, garde-fou contre les
absences répétées, ressources partagées (une salle, un appareil), remplaçant,
notifications par email et SMS avec fournisseurs configurables par cabinet.

### Exploitation

- Déploiement par `docker compose` (application, PostgreSQL, Caddy pour le HTTPS automatique).
- Sauvegardes chiffrées (age), restauration testée, envoi optionnel vers un stockage S3 : [`docs/backup.md`](docs/backup.md).
- Flux d'agenda ICS pour les praticiens.
- RGPD : export et effacement d'un patient, purge automatique, journal d'audit : [`docs/rgpd.md`](docs/rgpd.md).
- Observabilité légère : journaux JSON (`slog`) sans donnée patient, métriques Prometheus optionnelles, page d'état `/admin/status`.

## Architecture

Un seul processus : serveur HTTP et worker de notifications. PostgreSQL fait tout le reste :
données, sessions, file de messages (`FOR UPDATE SKIP LOCKED`) et garantie contre la
double réservation.

```
            ┌──────────────────────── binaire unique `doctolibre` ────────────────────────┐
navigateur ─┤ HTTP (net/http)  →  handlers  →  services (métier)  →  store (sqlc/pgx) ─────┼─→ PostgreSQL
 (htmx)     │        │ html/template + htmx (embed)          │                             │
            │        └─ assets statiques (embed)              └─ outbox ──→ worker ─────────┼─→ SMTP / fournisseur SMS
            └────────────────────────────────────────────────────────────────────────────┘
```

| Sujet | Choix |
|---|---|
| Langage, routage | Go (bibliothèque standard, `net/http`) |
| Accès base | `pgx/v5` et `sqlc` : SQL écrit à la main, code Go généré et typé |
| Migrations | `goose`, SQL pur, embarquées dans le binaire |
| Front | `html/template` et htmx, rendu serveur ; Tailwind CSS v4 (exécutable autonome) et daisyUI 5 |
| Agenda du cabinet | EventCalendar (vkurko) ; repli sans JavaScript : liste du jour rendue par le serveur |
| Notifications | table `outbox_message` et worker PostgreSQL ; email SMTP générique, SMS par fournisseur interchangeable |
| Double réservation | contrainte `EXCLUDE` sur la plage bloquée (soin et marges) : la base tranche, pas l'application |
| Authentification | cabinet : mot de passe argon2id et session en base ; patient : code à usage unique et lien signé |

Les détails sont dans [`PLAN.md`](PLAN.md) et les décisions argumentées dans [`docs/decisions.md`](docs/decisions.md).

## Contraintes du projet

**Techniques**

- Pas de Node, pas de bundler, pas de framework JavaScript. Le CSS est généré par l'exécutable autonome de Tailwind et commité.
- Pas de Redis ni de file de messages externe : une seule base à héberger et à sauvegarder.
- Politique de sécurité du contenu stricte (`default-src 'self'`) : pas de script ni de style en ligne, aucune ressource externe (CDN, polices).
- Chaque formulaire fonctionne sans JavaScript ; htmx n'est qu'une amélioration.
- Tous les horaires en `timestamptz` (UTC), calculs de créneaux dans le fuseau du cabinet ; les changements d'heure sont couverts par des tests.
- Dépendances Go limitées à une liste blanche (`pgx`, `goose`, `sqlc`, `uuid`, `x/crypto`, `x/term`, `x/time`, client Prometheus). Voir [`THIRD_PARTY.md`](THIRD_PARTY.md).
- Les invariants vivent en base quand c'est possible (contraintes `CHECK`, `UNIQUE`, `EXCLUDE`).

**Réglementaires et économiques**

- Aucune donnée médicale dans le modèle ; le champ libre côté cabinet est une « note administrative », signalée comme telle.
- Un rendez-vous chez un kiné est une donnée de santé indirecte. Le praticien qui s'héberge est responsable de traitement ; si un tiers héberge pour son compte, la certification HDS s'applique. Ce dépôt n'est pas un conseil juridique : voir [`docs/rgpd.md`](docs/rgpd.md).
- Le SMS est le poste de coût dominant ; la stratégie d'envoi compte plus que le fournisseur : voir [`docs/sms.md`](docs/sms.md) et [`docs/cost.md`](docs/cost.md).

## Démarrage rapide (développement)

Prérequis : Go récent (voir `go.mod`), Docker (pour PostgreSQL et Mailpit de développement).

```bash
cp .env.example .env     # valeurs de développement, dont DOCTOLIBRE_SECRET_KEY
make db-up               # PostgreSQL (et Mailpit) via docker compose
make migrate             # applique les migrations
make tools               # une fois : télécharge Tailwind dans bin/ (sha256 vérifié)
make run                 # serveur sur http://localhost:8080
make check               # fmt + vet + lint + tests + vérification de ui.css
```

Il n'y a pas d'inscription publique : le premier compte se crée en ligne de commande.

```bash
go run ./cmd/doctolibre create-user \
  --email vous@example.com --role admin --practice-name "Cabinet de démonstration"
```

Le mot de passe est demandé au clavier (12 caractères minimum). Puis connexion sur
`http://localhost:8080/admin/login`. Après toute modification d'un template ou de `input.css`,
lancer `make css`. Les tests qui touchent la base sont ignorés si PostgreSQL n'est pas disponible.

Pour une installation en production : [`docs/install.md`](docs/install.md).

## Comment ce projet est construit : vibe coding orchestré

Le code est écrit par des modèles de langage ; l'humain fixe le cap, tranche les décisions et
relit. Le dépôt est conçu pour qu'un agent sans mémoire puisse reprendre le travail :

- un modèle plus puissant (**Opus**) joue l'orchestrateur : il choisit la tâche, précise la spécification, délègue, contrôle et tient le suivi ;
- un modèle plus économique (**Sonnet**), dans un sous-agent, implémente une seule tâche à la fois, écrit les tests et rend un rapport ;
- la mémoire du projet est dans le dépôt, pas dans les conversations : [`PLAN.md`](PLAN.md), [`AGENTS.md`](AGENTS.md), une fiche par tâche avec critères d'acceptation et section « Suivi », [`docs/progress/STATUS.md`](docs/progress/STATUS.md), un journal et un registre de décisions ;
- le contrôle ne se fie pas aux rapports : l'orchestrateur relance `make check`, lit les diffs à risque et vérifie les critères ; des audits de code et d'ergonomie génèrent de nouvelles tâches ;
- une relecture humaine reste la porte vers le statut `done`, et elle prend du retard : beaucoup de tâches attendent en `review`.

Le détail, les chiffres, les limites et la façon de reproduire le processus sont dans
[`docs/vibe-coding.md`](docs/vibe-coding.md).

## Documentation

Index complet : [`docs/README.md`](docs/README.md).

| Document | Sujet |
|---|---|
| [`PLAN.md`](PLAN.md) | Vision, périmètre, architecture, modèle de domaine, phases |
| [`AGENTS.md`](AGENTS.md) | Conventions de code et workflow de session |
| [`docs/install.md`](docs/install.md) | Installer en production |
| [`docs/cost.md`](docs/cost.md), [`docs/sms.md`](docs/sms.md), [`docs/email.md`](docs/email.md) | Coûts, SMS, email |
| [`docs/backup.md`](docs/backup.md), [`docs/rgpd.md`](docs/rgpd.md) | Sauvegardes, RGPD |
| [`docs/design-system.md`](docs/design-system.md) | Thèmes, composants, page de contrôle `/dev/styleguide` |
| [`docs/vibe-coding.md`](docs/vibe-coding.md) | Le processus de développement piloté par LLM |
| [`THIRD_PARTY.md`](THIRD_PARTY.md) | Composants tiers et licences |

## Licence

[AGPL-3.0](LICENSE) : un hébergeur qui modifie le logiciel doit en publier les sources.
Les composants tiers gardent leur licence : voir [`THIRD_PARTY.md`](THIRD_PARTY.md).
Dépôt : `<URL-DU-DEPOT>`.
