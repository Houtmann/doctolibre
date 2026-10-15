# Tâches

Chaque fichier `Txxx-*.md` est une tâche **autonome, exécutable par un LLM en une session**
(≈ 1 PR, < ~800 lignes de diff hors code généré). Format : voir [`_TEMPLATE.md`](_TEMPLATE.md).

## Mode d'emploi (pour un agent)

**Mode normal : orchestré.** Dans une session Opus, lancer `/run-task` (ou `/run-task Txxx`) : Opus choisit et contrôle, le sous-agent Sonnet `task-implementer` implémente. Rôles détaillés dans [`AGENTS.md`](../AGENTS.md).

Revue Opus **renforcée** (diff relu en entier, invariants vérifiés) : T010, T011, T013, T014, T020, T043.

Sans orchestrateur, suivre le **workflow de session** de [`AGENTS.md`](../AGENTS.md) (début / pendant / fin). En résumé :

1. Lire [`docs/progress/STATUS.md`](../docs/progress/STATUS.md). Reprendre la tâche `in_progress` s'il y en a une (section **Suivi**), sinon la première `todo` dont toutes les dépendances sont `done`.
2. Implémenter **strictement le périmètre** ; tout ce qui est en « Hors scope » est interdit.
3. Exécuter les commandes de « Vérification » ; toutes doivent passer.
4. En fin de session : section Suivi de la tâche, statut ci-dessous, `STATUS.md`, `journal.md`.

**Ce tableau est la seule source de vérité des statuts** : `todo` · `in_progress` · `blocked` · `review` · `done` (définitions dans `AGENTS.md`).

## État

| ID | Tâche | Phase | Dépend de | Statut |
|---|---|---|---|---|
| T001 | [Bootstrap du dépôt Go](T001-bootstrap.md) | 0 | — | done |
| T002 | [PostgreSQL dev, goose, sqlc](T002-database-tooling.md) | 0 | T001 | done |
| T003 | [Serveur HTTP et middlewares](T003-http-server.md) | 0 | T001 | done |
| T004 | [Socle front : templates, htmx, CSS](T004-frontend-base.md) | 0 | T003 | done |
| T005 | [Infra de tests](T005-test-infra.md) | 0 | T002 | done |
| T010 | [Schéma de base v1](T010-schema-v1.md) | 1 | T002 | done |
| T011 | [Auth praticien](T011-auth-staff.md) | 1 | T004, T005, T010 | done |
| T012 | [Paramétrage cabinet, motifs, disponibilités](T012-availability-admin.md) | 1 | T011 | done |
| T013 | [Moteur de calcul de créneaux](T013-slot-engine.md) | 1 | T001 | done |
| T014 | [Réservation en ligne patient](T014-public-booking.md) | 1 | T012, T013 | review |
| T015 | [Gestion du RDV par le patient](T015-patient-manage-link.md) | 1 | T014 | todo |
| T016 | [Agenda praticien](T016-staff-calendar.md) | 1 | T012, T013 | review |
| T017 | [Fiches patients](T017-patients.md) | 1 | T016 | todo |
| T020 | [Outbox et worker](T020-outbox-worker.md) | 2 | T010, T005 | done |
| T021 | [Email SMTP générique](T021-email-smtp.md) | 2 | T020 | done |
| T022 | [SMS : interface et fournisseurs](T022-sms-providers.md) | 2 | T020 | done |
| T023 | [Confirmations et rappels](T023-reminders.md) | 2 | T021, T022, T015 | todo |
| T024 | [Outbox : câblage du worker et page admin](T024-outbox-wiring.md) | 2 | T020, T011 | done |
| T030 | [Séries de séances kiné](T030-care-plans.md) | 3 | T016, T017 | todo |
| T031 | [Liste d'attente](T031-waitlist.md) | 3 | T023 | todo |
| T032 | [Visites à domicile](T032-home-visits.md) | 3 | T014 | todo |
| T033 | [Multi-praticiens et rôles](T033-multi-practitioner.md) | 3 | T016, T021 | todo |
| T040 | [Packaging Docker + Caddy](T040-packaging.md) | 4 | T004, T010 | done |
| T041 | [Sauvegardes et restauration](T041-backups.md) | 4 | T040 | todo |
| T042 | [Flux ICS](T042-ics-feed.md) | 4 | T016 | todo |
| T043 | [RGPD : export, purge, audit](T043-gdpr.md) | 4 | T017 | todo |
| T044 | [Documentation d'installation](T044-install-docs.md) | 4 | T040, T041 | todo |
| T045 | [Observabilité légère](T045-observability.md) | 4 | T003 | done |
| T046 | [Observabilité : branchements et /admin/status](T046-observability-wiring.md) | 4 | T045, T014, T020, T041 | todo |

## Parallélisation possible

- Après T001 : **T002**, **T003**, **T013** en parallèle (fichiers disjoints).
- Après T020 : **T021** et **T022** en parallèle.
- Phase 3 et 4 : la plupart des tâches sont indépendantes entre elles.

```
T001 ─┬─ T002 ─┬─ T005 ─┐
      │        └─ T010 ─┼─ T011 ─ T012 ─┬─ T014 ─ T015 ─┐
      ├─ T003 ─ T004 ───┘               └─ T016 ─ T017  │
      └─ T013 ──────────────────────────────┘           │
                 T010+T005 ─ T020 ─┬─ T021 ─┬─ T023 ────┘
                                   └─ T022 ─┘
```
