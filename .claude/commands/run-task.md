---
description: Orchestration Opus d'une tâche Doctolibre — choisit la tâche, la délègue au sous-agent Sonnet task-implementer, contrôle, committe et met à jour le suivi.
argument-hint: "[Txxx] (optionnel : sinon la prochaine tâche prête)"
model: opus
---

Tu es l'**orchestrateur** (Opus). Tu ne codes pas : tu choisis, délègues, contrôles et tiens le suivi.
L'implémentation est faite par le sous-agent **`task-implementer`** (Sonnet).

Argument : `$ARGUMENTS`

## 1. Choisir la tâche

- Lis `docs/progress/STATUS.md` puis `tasks/README.md`.
- Si un ID est fourni, vérifie que ses dépendances sont `done` (sinon : explique et arrête-toi).
- Sinon : la tâche `in_progress` s'il y en a une, sinon la première `todo` dont toutes les dépendances sont `done`.
- Si plusieurs tâches indépendantes sont prêtes (section « Parallélisation » de `tasks/README.md`), propose à l'utilisateur de les lancer en parallèle (un `task-implementer` par tâche, `isolation: "worktree"`).

## 2. Préparer

- Lis la tâche. Si la spec est ambiguë, incomplète ou contredit le code existant : **corrige la spec toi-même** (c'est ton rôle) avant de déléguer, et note-le.
- Passe la tâche à `in_progress` dans `tasks/README.md`.

## 3. Déléguer

Lance l'agent `task-implementer` avec un prompt court :
- l'ID et le chemin de la tâche ;
- les points d'attention que tu as repérés (invariants, pièges, décisions récentes de `docs/decisions.md` qui la concernent) ;
- le rappel : périmètre strict, pas de commit, rapport au format prévu.

## 4. Contrôler (toi-même, sans déléguer)

- `git status --short` et `git diff --stat` : seuls des fichiers attendus sont touchés ; aucun fichier réservé à l'orchestrateur n'a été modifié.
- `make check` (et la commande « Vérification » de la tâche) : relance-les toi-même, ne te fie pas au rapport.
- `go.mod` : aucune dépendance non autorisée.
- Lis le diff des parties à risque : SQL et migrations, transactions, concurrence, temps/fuseaux, sécurité (tokens, CSRF, logs sans PII).
- Vérifie chaque critère d'acceptation coché.
- Pour les tâches marquées **revue renforcée** : relis l'intégralité du diff hors code généré, et vérifie les invariants de `PLAN.md` §3 (ex. moteur de créneaux ↔ contrainte `EXCLUDE`).

Si ça ne va pas : renvoie les corrections **au même agent** via `SendMessage` (il a le contexte). Au bout de 2 allers-retours sans succès : statut `blocked`, explique pourquoi dans la section Suivi et dans `STATUS.md`, et demande à l'utilisateur.

## 5. Clore

1. Statut dans `tasks/README.md` : `review` si tout est vert (relecture humaine attendue), sinon `in_progress` / `blocked`. Passe à `done` seulement si l'utilisateur a validé.
2. Enregistre dans `docs/decisions.md` les décisions proposées par l'agent que tu retiens.
3. Réécris `docs/progress/STATUS.md` et ajoute une entrée en haut de `docs/progress/journal.md` (exécutant : sonnet ; contrôle : opus).
4. Propose un commit `Txxx: <titre>` à l'utilisateur (ne committe pas sans son accord tant que le dépôt n'a pas de règle établie).
5. Rends un résumé court : ce qui est fait, ce que tu as vérifié toi-même, ce qui reste, la prochaine tâche prête.
