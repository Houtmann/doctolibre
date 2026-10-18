# Comment ce projet est construit : vibe coding orchestré

Ce document décrit le processus réellement utilisé dans ce dépôt, avec ses chiffres et ses
défauts. Il est factuel et daté (état au 2026-10-17 : 124 commits entre le 2026-10-15 et le
2026-10-17). Il ne prétend pas que la méthode soit meilleure qu'une autre.

## 1. Principe

- Le code (Go, SQL, templates, tests) est écrit par des modèles de langage.
- L'humain décide du produit, tranche les questions ouvertes, relit les tâches en `review` et valide les passages en `done`.
- Les modèles n'ont pas de mémoire d'une session à l'autre : **tout ce qui doit survivre est écrit dans le dépôt**. Une session qui démarre lit l'état du projet, travaille, puis le réécrit.

## 2. Les rôles

| Rôle | Modèle | Fait | Ne fait pas |
|---|---|---|---|
| Orchestrateur | Opus (session principale, commande `/run-task`) | Choisit la tâche, corrige la spécification si elle est ambiguë, délègue, contrôle, renvoie les corrections, tient le suivi, propose le commit | N'écrit pas le code applicatif |
| Exécutant | Sonnet (sous-agent `task-implementer`) | Implémente une seule tâche, écrit les tests, lance les vérifications, remplit la section Suivi de sa tâche, rend un rapport | Ne touche ni aux fichiers de suivi global, ni à `PLAN.md` / `AGENTS.md`, ne committe pas |

Les consignes complètes sont dans [`../.claude/agents/task-implementer.md`](../.claude/agents/task-implementer.md) et [`../.claude/commands/run-task.md`](../.claude/commands/run-task.md).

Déroulé de `/run-task` :

1. Choisir la tâche : celle déjà `in_progress`, sinon la première `todo` dont toutes les dépendances sont `done`.
2. Préparer : relire la spécification, la corriger si elle contredit le code, passer la tâche à `in_progress`.
3. Déléguer au `task-implementer` avec un prompt court (identifiant, pièges repérés, rappel du périmètre).
4. Contrôler **sans déléguer** : `git status` et `git diff --stat`, relance de `make check`, vérification de `go.mod`, lecture des parties à risque (SQL, transactions, concurrence, fuseaux, sécurité), vérification de chaque critère coché.
5. Clore : statut `review`, décisions retenues, `STATUS.md`, `journal.md`, proposition de commit.

**Revue standard ou renforcée.** Chaque tâche indique son niveau. En revue renforcée, l'orchestrateur relit tout le diff hors code généré et vérifie les invariants de `PLAN.md` (par exemple l'accord entre le moteur de créneaux et la contrainte `EXCLUDE`). Elle est réservée aux tâches sensibles : schéma, authentification, moteur de créneaux, réservation publique, outbox, RGPD, garde-fous, limitation de débit, ressources partagées, etc.

**Deux allers-retours maximum.** Les corrections demandées en revue repartent vers le même sous-agent (il garde son contexte). Si deux allers-retours ne suffisent pas, la tâche passe en `blocked`, la raison est écrite dans sa section Suivi et dans `STATUS.md`, et la question est posée à l'humain.

## 3. La mémoire est dans le dépôt

| Fichier | Rôle | Qui l'écrit |
|---|---|---|
| [`../PLAN.md`](../PLAN.md) | Vision, périmètre, architecture, modèle de domaine, phases | Humain |
| [`../AGENTS.md`](../AGENTS.md) | Conventions (Go, SQL, temps, front, sécurité, tests) et workflow de session | Humain / agent sur demande |
| `../tasks/Txxx-*.md` | Spécification d'une tâche : contexte à lire, périmètre, hors scope, critères d'acceptation cochables, vérification, section Suivi | Orchestrateur (spec), exécutant (Suivi) |
| [`../tasks/README.md`](../tasks/README.md) | **Seule source de vérité des statuts** et des dépendances | Orchestrateur |
| [`progress/STATUS.md`](progress/STATUS.md) | Tableau de bord réécrit à chaque fin de session : où on en est, prochaine action, blocages | Orchestrateur |
| [`progress/journal.md`](progress/journal.md) | Historique des sessions, la plus récente en haut | Orchestrateur |
| [`decisions.md`](decisions.md) | Décisions avec la forme « quoi / pourquoi / alternative écartée » | Orchestrateur / humain |

Pourquoi ce luxe de documents : une session qui démarre ne sait rien. Lire `STATUS.md`, `AGENTS.md`
et une tâche suffit pour reprendre. Une information n'est écrite qu'à un endroit (le statut d'une
tâche, par exemple, uniquement dans `tasks/README.md`), pour éviter les contradictions entre
documents. Les décisions écartées sont conservées pour qu'un agent ne les « redécouvre » pas.

## 4. Cycle de vie d'une tâche

| Statut | Signification |
|---|---|
| `todo` | Pas commencée |
| `in_progress` | Commencée ; la section Suivi dit où reprendre |
| `blocked` | Bloquée ; la raison est dans Suivi et dans `STATUS.md` |
| `review` | Implémentée, `make check` vert, en attente de relecture humaine |
| `done` | Relue, critères d'acceptation cochés |
| `superseded` | Remplacée par une autre tâche (nommée dans Suivi), à ne pas implémenter |

Une tâche est conçue pour tenir en une session (environ une PR, moins de 800 lignes de diff hors code généré). Elle contient un « Hors scope » explicite : l'exécutant n'a pas le droit d'y toucher. À la fin de chaque session, même inachevée, la section Suivi, le statut, `STATUS.md` et `journal.md` sont mis à jour ; un commit par tâche.

## 5. Parallélisme

- Des tâches indépendantes peuvent tourner en parallèle : un `task-implementer` par tâche, chacun dans son propre worktree git.
- L'orchestrateur est le **seul écrivain** des fichiers de suivi, ce qui évite les conflits de fusion sur `tasks/README.md` ou `STATUS.md`.
- Un seul agent à la fois touche à `sqlc` et aux migrations : les numéros de migration se séquencent (une tâche prend 00025, la suivante 00026) et la régénération du code SQL ne se fait pas en parallèle.
- Les fusions de branches parallèles sont essayées avant d'être proposées (par exemple une branche d'essai qui fusionne deux tâches, avec `make check` relancé dessus).

## 6. Garde-fous

- **`make check`** : formatage, `go vet`, linter, tests, vérification que `ui.css` généré est à jour. Il doit être vert avant le statut `review`, et l'orchestrateur le relance lui-même.
- **Invariants en base** : la contrainte `EXCLUDE` sur la plage bloquée (soin et marges) tranche les réservations concurrentes ; le moteur de créneaux applique la même règle.
- **Tests de changement d'heure** : tout code touchant aux créneaux a des tests sur les dates de passage heure d'été / heure d'hiver.
- **Golden files** pour les emails et les flux ICS (`-update` pour les régénérer).
- **Un bug corrigé, un test de non-régression.**
- **Passage navigateur** : les parcours sont regardés dans un vrai navigateur (360, 375 et 1280 px, thèmes clair et sombre). Ce qui n'a pas été vu est écrit explicitement dans le suivi, tâche par tâche.
- **Audits** : un audit de code (2026-10-17, 23 constats dont 9 confirmés) a produit les tâches T088 à T091 ; un audit d'ergonomie des parcours patients (même jour) a produit T093 à T098. Les constats deviennent des tâches avec critères, au lieu de corrections ad hoc.
- **Conventions vérifiables** : pas de `time.Now()` dans le métier, pas de donnée patient dans les logs, pas de classe CSS construite dynamiquement, CSP stricte.

## 7. Chiffres (2026-10-17)

71 tâches dans [`../tasks/README.md`](../tasks/README.md) :

| Phase | Tâches | `done` | Autres statuts |
|---|---|---|---|
| 0 — Fondations | 5 | 5 | |
| 1 — Cœur de la prise de rendez-vous | 8 | 8 | |
| 2 — Notifications | 5 | 5 | |
| 3 — Spécificités kiné | 4 | 4 | |
| 4 — Exploitation | 7 | 7 | |
| 5 — Retours terrain | 42 | 7 | 21 `review`, 12 `todo`, 1 `blocked`, 1 `superseded` |

Soit 36 `done`, 21 `review`, 12 `todo`, 1 `blocked` et 1 `superseded`. 124 commits, sur trois jours calendaires (2026-10-15 à 2026-10-17), avec plusieurs sessions parallèles.

## 8. Limites et leçons

- **La relecture humaine est le goulot.** 21 tâches attendent en `review` ; les agents produisent plus vite qu'un humain ne relit. Une tâche `review` n'est pas une tâche éprouvée, et la suite reste bloquée tant que ses dépendances ne sont pas `done`.
- **Vérifications manuelles non faites.** Aucune installation sur une machine neuve, aucun `docker build` ni test HTTPS réels, aucun envoi réel d'email ou de SMS, aucun import ICS dans un vrai client, aucun `EXPLAIN` sur des volumes réels. La liste tenue à jour est dans `STATUS.md`. Les tests automatisés ne remplacent pas cela.
- **Changement de direction coûteux.** Un « design system v2 » a été spécifié et en partie implémenté (T063 à T067) sur Pico CSS, puis jugé incohérent à l'écran ; le 2026-10-16, la décision a été prise de migrer vers Tailwind v4 standalone et daisyUI 5 (T080 à T084), qui ont remplacé T068 et supprimé le CSS précédent. Les tâches étaient bien découpées, mais du travail validé a été jeté. Voir [`decisions.md`](decisions.md).
- **Un agent peut passer à côté de l'écran.** Beaucoup de défauts (texte inadapté, parcours incohérent, bouton absent) n'apparaissent qu'au navigateur, d'où les audits d'ergonomie qui ont généré T093 à T098.
- **Tests sensibles à la charge.** Un test de performance (`TestCalendarWeekWith150AppointmentsIsFast`, seuil de 100 ms) échoue parfois quand la machine est occupée par des agents parallèles ; il passe seul. La consigne est de le relancer seul avant de chercher un bug.
- **Le disque se remplit.** Le cache de compilation Go de sous-agents parallèles a occupé jusqu'à 19 Go ; il faut penser à le vider.
- **Économie de contexte et de temps.** L'exécutant a l'ordre de ne pas lancer `make check` après chaque édition et de limiter la lecture aux fichiers listés dans la tâche.
- **Les décisions s'accumulent vite.** Le registre est utile mais devient long ; il doit être relu pour repérer celles qui en remplacent d'autres.
- **Pas de CI exécutée.** La CI décrite dans le dépôt n'a jamais tourné, faute de dépôt distant jusqu'ici.

## 9. Reproduire le processus sur votre projet

1. Écrivez un `PLAN.md` court : objectifs, hors scope, architecture, modèle de données, invariants, phases.
2. Écrivez un `AGENTS.md` de conventions **vérifiables** (ce qu'un linter ou un test peut contrôler) et le workflow de session.
3. Découpez en tâches autonomes d'une session, avec contexte à lire, périmètre, hors scope, critères d'acceptation cochables et commande de vérification. Mettez dépendances et statuts dans un seul tableau.
4. Créez un tableau de bord (`STATUS.md`) réécrit en fin de session et un journal en ajout seul.
5. Définissez un sous-agent exécutant avec ses interdits (fichiers de suivi, commit) et une commande d'orchestration qui impose de contrôler soi-même (relancer les vérifications, lire les diffs à risque).
6. Mettez les invariants en base ou dans des tests, pas seulement dans des consignes.
7. Traitez la relecture humaine comme une capacité limitée : n'ouvrez pas plus de tâches en parallèle que vous ne pouvez en relire, et tenez la liste de ce qui n'a pas été vérifié en conditions réelles.
8. Faites des audits périodiques (code, ergonomie) dont le produit est une liste de tâches.
