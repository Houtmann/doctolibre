# T055 — Lien de série pour le patient

- **Phase** : 5
- **Dépend de** : T050
- **Taille estimée** : M
- **Exécution** : sonnet (`task-implementer`) — revue Opus : renforcée

## Objectif
Le patient d'une série reçoit un lien unique `/s/{token}` pour voir toutes ses séances, gérer chacune et importer les séances à venir dans son agenda.

## Contexte à lire
T050 (dérivation du jeton), `migrations/00013_care_plan.sql`, `internal/service/care_plans.go` (`Create`, événements émis après commit), `internal/service/notifications.go` (message de confirmation des séances de série, `notifications_series_test.go`), `internal/service/manage.go`, `internal/web/public.go` (routes `/r/{token}`, middleware `manage` de rate limiting, `calendarFile`), `internal/web/public_manage.go`, `internal/ics/ics.go` (`Feed`), `internal/service/patients.go` (`Merge`), `internal/service/privacy.go`, `internal/notify/templates/`.

## Spécification
- Jeton de série dérivé comme T050 : `HMAC(k, care_plan_id || ":" || version)` avec le label `"care-plan-token/v1"`. Migration : `care_plan.token_version int NOT NULL DEFAULT 1` et `care_plan.token_hash text UNIQUE` = sha256(jeton dérivé), recherche par hash comme `/r/` (même schéma que T050, aucun identifiant dans l'URL). Séries existantes : hash rempli à la volée à la première génération du lien.
- Route `GET /s/{token}` (public, même rate limiting que `/r/`, `Cache-Control: no-store`, `Referrer-Policy: no-referrer`) : liste des séances passées/à venir avec statut ; pour chaque séance à venir, le lien de gestion `/r/{token}` de T050 ; bouton de téléchargement d'un ICS de **toutes les séances à venir** (`GET /s/{token}/ics`, via `ics.Feed`).
- Le lien de série est inclus dans le message de **confirmation de création de série** (vérifier quel message part aujourd'hui pour les séances d'une série : un par séance ou un récapitulatif ; adapter les templates email/SMS en conséquence, sans doubler les messages).
- Le patient ne modifie pas la série au-delà de ce que permet déjà `/r/{token}` par séance.
- Révocation (incrément de version) à la fusion de fiches patients et à l'effacement RGPD ; jeton invalide → 404 neutre.
- Aucune donnée médicale affichée (libellé de série = texte du cabinet : l'afficher tel quel est acceptable seulement s'il existe déjà côté patient, sinon le motif du RDV).

## Critères d'acceptation
- [ ] `/s/{token}` valide affiche les séances ; jeton falsifié/révoqué → 404.
- [ ] Les liens de gestion des séances à venir fonctionnent ; les séances passées/annulées n'en ont pas.
- [ ] L'ICS de série contient exactement les séances à venir non annulées.
- [ ] La confirmation de série contient le lien ; aucun doublon de message.
- [ ] Révocation testée (fusion, effacement) ; rate limiting appliqué.

## Vérification
```bash
make sqlc
make check
```

## Hors scope
Réservation des séances restantes par le patient, PDF, modification de la série par le patient.

## Suivi

> Rempli par l'agent pendant et à la fin de chaque session sur cette tâche. Le statut officiel est dans `tasks/README.md`.

- **Sessions** : —
- **Fait** : —
- **Reste à faire** : —
- **Notes de reprise** (pièges, état intermédiaire, commandes utiles) : —
- **Écarts avec la spécification** (et pourquoi) : —
