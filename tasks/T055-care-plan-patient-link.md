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
- [x] `/s/{token}` valide affiche les séances ; jeton falsifié/révoqué → 404.
- [x] Les liens de gestion des séances à venir fonctionnent ; les séances passées/annulées n'en ont pas.
- [x] L'ICS de série contient exactement les séances à venir non annulées.
- [x] La confirmation de série contient le lien ; aucun doublon de message.
- [x] Révocation testée (fusion, effacement) ; rate limiting appliqué.

## Vérification
```bash
make sqlc
make check
```

## Hors scope
Réservation des séances restantes par le patient, PDF, modification de la série par le patient.

## Suivi

> Rempli par l'agent pendant et à la fin de chaque session sur cette tâche. Le statut officiel est dans `tasks/README.md`.

- **Sessions** : 2026-10-17 (sonnet).
- **Fait** : migration 00025 (`care_plan.token_version`, `token_hash UNIQUE`) ; jeton dérivé `HMAC(HMAC(ManageKey, "care-plan-token/v1"), id:version)` via `DeriveManageToken` (`service/care_plan_link.go`) ; `Booking.CarePlanByToken` ; routes `GET /s/{token}` et `/s/{token}/ics` (seau `manage` partagé avec `/r/`, no-store, no-referrer, 404 neutre) ; page `public_series.html` ; lien de série dans la confirmation unique de série (email html/txt, SMS raccourci pour rester en 1 segment), clé `series_url` ajoutée aux secrets effacés par `MarkOutboxSent` ; révocation dans `Patients.Merge` (plans du doublon) et `Erase` (via `AnonymizeCarePlanLabels`) ; tests service, web, golden notify ; `make css` + `make check` verts.
- **Reste à faire** : rien d'obligatoire. Aucun écran du cabinet ne montre/renvoie le lien de série (hors scope) ; passage navigateur de `/s/` non fait.
- **Notes de reprise** : EXPLAIN (base vide, seqscan off) : `GetCarePlanByTokenHash` utilise `care_plan_token_hash_key` ; `ListCarePlanSessions` même forme que `ListCarePlanAppointments` (index `appointment_care_plan_idx`, à confirmer sur données réelles). Génération paresseuse : `ensurePlanToken` n'est appelée qu'à la création de série (aucun autre point de génération n'existe aujourd'hui) ; `export_test.go` l'expose au test.
- **Écarts avec la spécification** : le label `care-plan-token/v1` dérive une sous-clé de `ManageKey` (déjà dérivée de la clé secrète) au lieu de la clé secrète brute, pour ne pas ajouter de câblage ; série d'une seule séance : confirmation ordinaire inchangée (sans lien de série) ; fusion : seuls les liens des séries du doublon sont révoqués.
