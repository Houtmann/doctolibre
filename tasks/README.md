# Tâches

Chaque fichier `Txxx-*.md` est une tâche **autonome, exécutable par un LLM en une session**
(≈ 1 PR, < ~800 lignes de diff hors code généré). Format : voir [`_TEMPLATE.md`](_TEMPLATE.md).

## Mode d'emploi (pour un agent)

**Mode normal : orchestré.** Dans une session Opus, lancer `/run-task` (ou `/run-task Txxx`) : Opus choisit et contrôle, le sous-agent Sonnet `task-implementer` implémente. Rôles détaillés dans [`AGENTS.md`](../AGENTS.md).

Revue Opus **renforcée** (diff relu en entier, invariants vérifiés) : T010, T011, T013, T014, T020, T043, T050, T051, T052, T053, T055, T059, T060, T067.

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
| T014 | [Réservation en ligne patient](T014-public-booking.md) | 1 | T012, T013 | done |
| T015 | [Gestion du RDV par le patient](T015-patient-manage-link.md) | 1 | T014 | done |
| T016 | [Agenda praticien](T016-staff-calendar.md) | 1 | T012, T013 | done |
| T017 | [Fiches patients](T017-patients.md) | 1 | T016 | done |
| T020 | [Outbox et worker](T020-outbox-worker.md) | 2 | T010, T005 | done |
| T021 | [Email SMTP générique](T021-email-smtp.md) | 2 | T020 | done |
| T022 | [SMS : interface et fournisseurs](T022-sms-providers.md) | 2 | T020 | done |
| T023 | [Confirmations et rappels](T023-reminders.md) | 2 | T021, T022, T015 | done |
| T024 | [Outbox : câblage du worker et page admin](T024-outbox-wiring.md) | 2 | T020, T011 | done |
| T030 | [Séries de séances kiné](T030-care-plans.md) | 3 | T016, T017 | done |
| T031 | [Liste d'attente](T031-waitlist.md) | 3 | T023 | done |
| T032 | [Visites à domicile](T032-home-visits.md) | 3 | T014 | done |
| T033 | [Multi-praticiens et rôles](T033-multi-practitioner.md) | 3 | T016, T021 | done |
| T040 | [Packaging Docker + Caddy](T040-packaging.md) | 4 | T004, T010 | done |
| T041 | [Sauvegardes et restauration](T041-backups.md) | 4 | T040 | done |
| T042 | [Flux ICS](T042-ics-feed.md) | 4 | T016 | done |
| T043 | [RGPD : export, purge, audit](T043-gdpr.md) | 4 | T017 | done |
| T044 | [Documentation d'installation](T044-install-docs.md) | 4 | T040, T041 | done |
| T045 | [Observabilité légère](T045-observability.md) | 4 | T003 | done |
| T046 | [Observabilité : branchements et /admin/status](T046-observability-wiring.md) | 4 | T045, T014, T020, T041 | done |
| T050 | [Jeton de gestion dérivé (HMAC)](T050-derived-manage-token.md) | 5 | T023, T030 | review |
| T051 | [Absence imprévue : annulation groupée](T051-sick-day-bulk-cancel.md) | 5 | T050 | todo |
| T052 | [Jours fériés français automatiques](T052-public-holidays.md) | 5 | T013 | review |
| T053 | [Import CSV des patients](T053-patient-csv-import.md) | 5 | T017 | todo |
| T054 | [Garde-fou absences répétées](T054-no-show-guard.md) | 5 | T014, T017 | todo |
| T055 | [Lien de série pour le patient](T055-care-plan-patient-link.md) | 5 | T050 | todo |
| T056 | [Réglages : profil vs cabinet, séries au secrétariat](T056-settings-split-and-secretary-plans.md) | 5 | T033, T030 | todo |
| T057 | [Couleurs dans l'agenda compatibles CSP](T057-calendar-colors.md) | 5 | T016 | todo |
| T058 | [Alarmes dans les fichiers ICS patient](T058-ics-valarm.md) | 5 | T042 | review |
| T059 | [Remplaçant : accès borné dans le temps](T059-locum-delegation.md) | 5 | T033 | todo |
| T060 | [Ressources partagées (box, appareils)](T060-shared-resources.md) | 5 | T013, T014, T016 | todo |
| T061 | [Affiche imprimable avec QR code](T061-qr-poster.md) | 5 | T014 | blocked |
| T062 | [Design system léger : tokens, catalogue, styleguide](T062-design-system.md) | 5 | T004, T016 | review |
| T063 | [Design v2 : socle embarqué et styleguide v2](T063-design-v2-foundation.md) | 5 | T062 | done |
| T064 | [Design v2 : layouts, navigation, messages, boutons](T064-design-v2-shell.md) | 5 | T063 | done |
| T065 | [Design v2 : formulaires et tableaux](T065-design-v2-forms.md) | 5 | T064 | review |
| T066 | [Design v2 : parcours patient](T066-design-v2-booking.md) | 5 | T064 | review |
| T067 | [Design v2 : agenda et dialogue de RDV](T067-design-v2-calendar.md) | 5 | T064 | review |
| T068 | [Design v2 : retrait de Pico et de l'ancien CSS](T068-design-v2-cleanup.md) | 5 | T065, T066, T067 | todo |
| T070 | [Consignes par motif et prérequis à la réservation](T070-appointment-type-instructions.md) | 5 | T014, T023, T058 | todo |
| T071 | [Auto-planification de la série par le patient](T071-care-plan-self-scheduling.md) | 5 | T055, T030, T014 | todo |
| T072 | [« Ma journée » : tournée domicile et pointage en un clic](T072-my-day-check-in.md) | 5 | T016, T032, T033 | todo |
| T073 | [Statistiques de séances et export pour rétrocession](T073-session-stats.md) | 5 | T072, T059 | todo |

## Parallélisation possible

- Après T001 : **T002**, **T003**, **T013** en parallèle (fichiers disjoints).
- Après T020 : **T021** et **T022** en parallèle.
- Phase 3 et 4 : la plupart des tâches sont indépendantes entre elles.
- Phase 5 : T050 d'abord (débloque T051, T055) ; T052, T053, T054, T056, T058 indépendantes. T062 (design system) avant T057 et T061, qui touchent aussi `app.css` : pas en parallèle. Design v2 (`design/`) : T063 → T064 → {T065, T066, T067 en parallèle, templates disjoints} → T068 ; T057 et T061 après T064 ou à coordonner (`app.css`, agenda). T070 et T072 indépendantes (T070 attend T058, en review) ; T071 après T055 ; T073 après T072 et T059. T070, T071, T072 ajoutent chacune une migration : numéros à attribuer au lancement, un seul agent à la fois sur `sqlc`.

```
T001 ─┬─ T002 ─┬─ T005 ─┐
      │        └─ T010 ─┼─ T011 ─ T012 ─┬─ T014 ─ T015 ─┐
      ├─ T003 ─ T004 ───┘               └─ T016 ─ T017  │
      └─ T013 ──────────────────────────────┘           │
                 T010+T005 ─ T020 ─┬─ T021 ─┬─ T023 ────┘
                                   └─ T022 ─┘
```
