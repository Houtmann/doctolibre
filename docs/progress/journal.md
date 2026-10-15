# Journal des sessions

Append-only, **la plus récente en haut**. Une entrée par session de travail (humain ou agent).
Rester court : le détail technique va dans la section « Suivi » de la tâche, les décisions dans [`decisions.md`](../decisions.md).

<!-- Modèle d'entrée :

## AAAA-MM-JJ — <titre court> (<agent/modèle ou humain>)

- **Tâches** : Txxx (todo → in_progress | done), Tyyy
- **Fait** : 2–5 puces, ce qui est vrai maintenant
- **Vérification** : `make check` ✅ / ❌ (+ ce qui échoue)
- **Reste / reprise** : ce que la prochaine session doit faire en premier
- **Décisions** : liens vers les lignes ajoutées dans decisions.md, ou « aucune »
-->

## 2026-10-15 — Répartition Opus / Sonnet (Claude Opus)

- **Tâches** : toutes (champ **Exécution** ajouté)
- **Fait** : sous-agent `.claude/agents/task-implementer.md` (Sonnet) ; commande `.claude/commands/run-task.md` (Opus : choisir, déléguer, contrôler, clore) ; section « Rôles » dans `AGENTS.md` ; revue renforcée pour T010, T011, T013, T014, T020, T043.
- **Vérification** : n/a (pas de code) — l'agent et la commande ne sont pas encore testés en réel.
- **Reste / reprise** : `git init` (prérequis du contrôle par `git diff`), puis `/run-task T001`.
- **Décisions** : répartition Opus/Sonnet

## 2026-10-15 — Suivi de l'avancement multi-session (Claude Opus)

- **Tâches** : aucune (documentation)
- **Fait** : création de `docs/README.md` (index), `docs/progress/STATUS.md` (tableau de bord), ce journal ; section « Suivi » ajoutée à chaque tâche ; statuts `in_progress`, `blocked`, `review` ajoutés ; workflow de début/fin de session dans `AGENTS.md`.
- **Vérification** : n/a (pas de code)
- **Reste / reprise** : `git init`, puis T001.
- **Décisions** : aucune

## 2026-10-15 — Intégration de la review externe (Claude Opus)

- **Tâches** : T005, T010, T013, T014, T016, T020, T023, T032, T033, T040 (spécifications modifiées)
- **Fait** : contrainte d'exclusion sur `during_blocked` (buffers inclus) et `status <> 'cancelled'` ; outbox liée au RDV par `appointment_id` ; index partiels ; tests DB par schéma ; dépendances T033→T021 et T040→T010.
- **Vérification** : n/a
- **Reste / reprise** : —
- **Décisions** : 4 entrées dans `decisions.md` (2026-10-15)

## 2026-10-15 — Comparatif SMS (Claude Opus)

- **Tâches** : T022, T023, T044 (spécifications modifiées)
- **Fait** : `docs/sms.md` (OVH ≈ 0,06 € HT, Twilio ≈ 0,07 €, passerelle Android) ; réglages par défaut : confirmation par email, rappel J-1 par SMS.
- **Vérification** : n/a
- **Reste / reprise** : vérifier les prix sur le simulateur OVH avant de chiffrer.
- **Décisions** : stratégie SMS par défaut

## 2026-10-15 — Plan initial (Claude Opus)

- **Tâches** : création de T001–T045
- **Fait** : `PLAN.md`, `AGENTS.md`, `CLAUDE.md`, `docs/decisions.md`, `tasks/`.
- **Vérification** : n/a
- **Reste / reprise** : —
- **Décisions** : stack (Go + Postgres + htmx, pas de Redis, AGPL)
