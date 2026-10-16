# Documentation

Point d'entrée de la doc, pour les humains comme pour les agents LLM.
Chaque document a **un seul rôle** : ne pas dupliquer une information, mettre un lien.

## Par où commencer

| Je suis… | Je lis, dans l'ordre |
|---|---|
| L'orchestrateur (Opus) | `/run-task` — la commande [`../.claude/commands/run-task.md`](../.claude/commands/run-task.md) déroule tout |
| Un agent qui démarre une session | [`progress/STATUS.md`](progress/STATUS.md) → [`../AGENTS.md`](../AGENTS.md) → la tâche en cours ou la suivante dans [`../tasks/`](../tasks/README.md) |
| Un humain qui découvre le projet | [`../PLAN.md`](../PLAN.md) → [`progress/STATUS.md`](progress/STATUS.md) → [`decisions.md`](decisions.md) |
| Un praticien qui veut installer | [`install.md`](install.md), puis [`cost.md`](cost.md) |

## Carte des documents

| Document | Rôle | Qui le met à jour | Quand |
|---|---|---|---|
| [`../PLAN.md`](../PLAN.md) | Vision, périmètre, architecture, modèle de domaine, phases | Humain (ou agent sur demande) | Rarement : changement de cap |
| [`../AGENTS.md`](../AGENTS.md) | Conventions de code et workflow de session | Humain / agent | Quand une règle manque |
| [`../tasks/README.md`](../tasks/README.md) | **Source de vérité du statut** de chaque tâche + dépendances | Orchestrateur | À chaque changement de statut |
| `../tasks/Txxx-*.md` | Spécification d'une tâche + section **Suivi** (avancement, notes de reprise) | Exécutant (Suivi) / orchestrateur (spec) | Pendant et à la fin de chaque session sur la tâche |
| [`progress/STATUS.md`](progress/STATUS.md) | Tableau de bord : où on en est, prochaine action, blocages | Orchestrateur | **Fin de chaque session** (réécrit, pas d'historique) |
| [`progress/journal.md`](progress/journal.md) | Historique des sessions (append-only) | Orchestrateur | **Fin de chaque session** (ajout en haut) |
| [`../.claude/agents/task-implementer.md`](../.claude/agents/task-implementer.md) | Consignes du sous-agent exécutant (Sonnet) | Humain / orchestrateur | Quand le workflow change |
| [`../.claude/commands/run-task.md`](../.claude/commands/run-task.md) | Procédure d'orchestration (Opus) : choisir, déléguer, contrôler, clore | Humain / orchestrateur | Quand le workflow change |
| [`decisions.md`](decisions.md) | Décisions d'architecture/produit, avec l'alternative écartée | Orchestrateur / humain | Dès qu'une décision est prise |
| [`sms.md`](sms.md) | Fournisseurs SMS, coûts, réglages par défaut | Agent (T022) | Quand les prix ou drivers changent |
| [`backup.md`](backup.md) | Sauvegardes chiffrées (age), restauration, envoi S3 | Agent (T041) | Quand le dispositif change |
| `email.md`, `rgpd.md`, `install.md`, `cost.md` | Docs d'exploitation | Créés par T021, T043, T044 | — |

## Règles d'écriture

- Français, phrases courtes, tableaux plutôt que paragraphes quand c'est comparatif.
- Dates absolues (`2026-10-15`), jamais « hier » ou « la semaine dernière ».
- Chemins relatifs au dépôt, cliquables : `[T013](../tasks/T013-slot-engine.md)`.
- Une info = un endroit. Le statut d'une tâche n'est écrit **que** dans `tasks/README.md` ; les autres docs y renvoient.
