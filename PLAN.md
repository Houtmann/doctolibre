# Doctolibre — plan

Prise de rendez-vous en ligne **libre et auto-hébergeable**, pensée d'abord pour les
**kinésithérapeutes** (cabinet seul ou petit cabinet de groupe).

## 1. Objectifs

| Objectif | Traduction concrète |
|---|---|
| Léger | 1 binaire Go + 1 PostgreSQL. Tourne sur un VPS à ~5 €/mois ou un mini-PC au cabinet. |
| Pas cher | Pas de SaaS obligatoire. Email via n'importe quel SMTP, SMS via le fournisseur le moins cher (ou un téléphone Android en passerelle). |
| Auto-hébergeable | `docker compose up -d` + un fichier `.env`. TLS automatique (Caddy). Sauvegardes documentées. |
| Maintenable | Peu de dépendances, SQL explicite, rendu serveur, pas de build JS. |
| Compatible LLM | Code prévisible, conventions écrites (`AGENTS.md`), tâches découpées et vérifiables (`tasks/`). |

### Hors scope v1
Téléconsultation, paiement en ligne, facturation / télétransmission (SESAM-Vitale),
dossier médical, application mobile native, place de marché multi-cabinets.

## 2. Architecture

```
            ┌──────────────────────── binaire unique `doctolibre` ────────────────────────┐
navigateur ─┤ HTTP (net/http)  →  handlers  →  services (métier)  →  store (sqlc/pgx) ─────┼─→ PostgreSQL
 (htmx)     │        │ html/template + htmx (embed)          │                             │
            │        └─ assets statiques (embed)              └─ outbox ──→ worker ─────────┼─→ SMTP / fournisseur SMS
            └────────────────────────────────────────────────────────────────────────────┘
```

- **Un seul process** : serveur HTTP + worker de notifications (goroutine), activables séparément par flag si besoin.
- **PostgreSQL fait tout** : données, sessions, file de jobs (`FOR UPDATE SKIP LOCKED`), anti double-réservation (contrainte d'exclusion).
- **Redis : non utilisé en v1.** Rien ne le justifie à cette échelle (quelques centaines de RDV/semaine). À reconsidérer seulement si on multiplie les instances.

### Choix techniques

| Sujet | Choix | Pourquoi |
|---|---|---|
| Langage | Go ≥ 1.24 | Binaire statique, perf, stdlib riche. |
| Routing | `net/http` (patterns `GET /x/{id}`) | Zéro dépendance, suffisant. |
| Accès DB | `pgx/v5` + **sqlc** | SQL écrit à la main, code Go généré typé : idéal pour un LLM (pas de magie ORM). |
| Migrations | **goose** (fichiers SQL, embarqués) | Simple, SQL pur. |
| Front | `html/template` + **htmx** + un peu d'**Alpine.js** si besoin + **Pico CSS** | Pas de toolchain JS, pas de `node_modules`, tout est embarqué dans le binaire. Très bien connu des LLM. |
| Agenda praticien | Grille HTML/CSS maison (vue jour/semaine) rendue serveur | Évite FullCalendar (lourd). |
| Auth praticien | Mot de passe argon2id + session cookie stockée en DB | Simple, pas d'OAuth. |
| Patient | **Pas de compte**. Vérification par code OTP (SMS ou email) à la réservation, puis lien signé pour gérer le RDV. | Moins de friction, moins de données. |
| Jobs | Table `outbox` + worker `SKIP LOCKED` | Pas de Redis/RabbitMQ. |
| Email | Interface `EmailSender`, impl **SMTP générique** (+ `log` en dev) | Marche avec OVH, Brevo, Gmail, Postfix local… |
| SMS | Interface `SMSSender`, impls : `log`, **HTTP générique (template configurable)**, OVH, **passerelle Android** (ex. app « SMS Gateway for Android ») | Comparatif des coûts : [`docs/sms.md`](docs/sms.md). OVH ≈ 0,06 € HT/SMS, Twilio ≈ 0,07 €, passerelle Android ≈ forfait (2–5 €/mois). Par défaut : confirmation par email, **rappel J-1 seul par SMS** (~25 € HT/mois chez OVH pour un kiné solo). |
| Config | Variables d'environnement (+ `.env` en dev) | 12-factor, Docker-friendly. |
| Logs / métriques | `log/slog` JSON + `/metrics` Prometheus (optionnel) | Léger. |
| Déploiement | Image Docker distroless + `docker-compose.yml` (app, postgres, caddy) | Une commande. |
| Temps | Tout en `timestamptz` UTC en base, fuseau du cabinet (`Europe/Paris` par défaut) pour l'affichage et le calcul des créneaux | Évite les bugs de changement d'heure. |

## 3. Modèle de domaine (v1)

- **practice** (cabinet) : nom, adresse, fuseau, paramètres (délai mini de réservation / d'annulation, fenêtre de réservation).
- **practitioner** : appartient à un cabinet, profil public (nom, slug, bio), couleur agenda.
- **user** : compte de connexion (praticien ou secrétariat), rôle.
- **appointment_type** (motif) : nom, durée, buffer avant/après, réservable en ligne ou non, « à domicile », nombre de séances par défaut.
- **availability_rule** : plages hebdo récurrentes (jour, début, fin, motifs autorisés, date de validité).
- **availability_exception** : absences / ouvertures exceptionnelles.
- **patient** : nom, prénom, téléphone, email, date de naissance (optionnelle), consentements notifications. **Aucune donnée médicale.**
- **appointment** : praticien, patient, motif, `tstzrange`, statut (`booked`, `cancelled`, `no_show`, `done`), source (`online`, `staff`), lien vers une série éventuelle.
- **care_plan** (série kiné) : nb de séances prescrites, séances faites/planifiées, fréquence souhaitée.
- **waitlist_entry**, **outbox_message**, **session**, **audit_log**.

Invariant clé : **pas de chevauchement** de RDV non annulés pour un même praticien, **buffers inclus**.
Chaque RDV stocke `during` (le soin) et `during_blocked` (soin + buffers avant/après, copiés du motif) →
`EXCLUDE USING gist (practitioner_id WITH =, during_blocked WITH &&) WHERE (status <> 'cancelled')`.
Le moteur de créneaux (T013) applique exactement la même règle.

## 4. Points d'attention

- **Données de santé / RGPD** : un RDV chez un kiné est une donnée de santé indirecte. Si un tiers héberge pour le compte d'un praticien, la certification **HDS** s'applique. Le praticien qui héberge lui-même ses données reste responsable de traitement : on fournit minimisation (pas de motif médical libre côté patient), chiffrement des sauvegardes, export/suppression patient, purge automatique, journal d'accès, doc RGPD. → tâche T043.
- **Coût SMS** : c'est la stratégie d'envoi (quels messages par SMS) qui fait la facture, plus que le fournisseur — voir [`docs/sms.md`](docs/sms.md).
- **SMS** : obligation de mention STOP pour le marketing uniquement ; les rappels de RDV sont transactionnels mais on gère quand même l'opt-out.
- **Changement d'heure** : le moteur de créneaux est la partie la plus sujette aux bugs → fonction pure, tests table-driven sur les dates de DST.
- **Concurrence** : deux patients sur le même créneau → c'est la contrainte d'exclusion qui tranche, l'app traduit l'erreur en « créneau plus disponible ».
- **Abus** : rate limiting en mémoire sur OTP et réservation, captcha-free (honeypot + limites).

## 5. Phases

| Phase | Contenu | Résultat |
|---|---|---|
| **0 — Fondations** | T001–T005 | Squelette qui démarre, DB migrée, page d'accueil, tests qui tournent. |
| **1 — Cœur RDV** | T010–T017 | Un praticien configure ses dispos, un patient réserve en ligne, le praticien voit son agenda. **= MVP utilisable sans notifications.** |
| **2 — Notifications** | T020–T023 | Confirmations, rappels, annulations par email et SMS. **= v0.1 publiable.** |
| **3 — Spécificités kiné** | T030–T033 | Séries de séances, liste d'attente, domicile, multi-praticiens. |
| **4 — Exploitation** | T040–T045 | Packaging, sauvegardes, ICS, RGPD, doc d'install, observabilité. |

Le détail, les dépendances et l'état de chaque tâche sont dans [`tasks/README.md`](tasks/README.md).
Les conventions de code pour humains et LLM sont dans [`AGENTS.md`](AGENTS.md).

## 6. Arborescence cible

```
cmd/doctolibre/          main : serve, migrate, worker, create-user
internal/
  config/                chargement env
  httpx/                 serveur, middlewares, helpers de rendu
  web/                   handlers HTTP (public/, admin/)
    templates/           html/template (layout, partials, pages)
    static/              htmx.min.js, pico.min.css, app.css
  domain/                types métier purs + erreurs
  booking/               moteur de créneaux (pur, sans DB)
  service/               orchestration métier (utilise store + notify)
  store/                 requêtes SQL (queries/*.sql) + code sqlc généré
  notify/                interfaces Email/SMS + implémentations + templates
  outbox/                file de messages + worker
  auth/                  mots de passe, sessions, OTP, liens signés
migrations/              goose *.sql
deploy/                  Dockerfile, docker-compose.yml, Caddyfile, .env.example
docs/                    installation, RGPD, fournisseurs SMS
tasks/                   tâches LLM
```
