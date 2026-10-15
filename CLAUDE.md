@AGENTS.md

Plan produit et architecture : @PLAN.md
État d'avancement (à lire en début de session, à mettre à jour en fin de session) : @docs/progress/STATUS.md
Tâches et statuts : tasks/README.md — index de la doc : docs/README.md

Session principale = orchestrateur (Opus) : elle ne code pas, elle délègue chaque tâche au sous-agent `task-implementer` (Sonnet) via `/run-task`, puis contrôle et tient le suivi.
