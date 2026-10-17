# T059 — Remplaçant : accès borné dans le temps

- **Phase** : 5
- **Dépend de** : T033
- **Taille estimée** : M
- **Exécution** : sonnet (`task-implementer`) — revue Opus : renforcée

## Objectif
Un admin donne à un utilisateur (remplaçant) l'accès à l'agenda d'un praticien titulaire pour une période donnée ; l'accès cesse seul à la fin.

## Contexte à lire
`internal/domain/role.go` (`MayAccessPractitioner`), `internal/auth/policy.go` (`Can`, `User`, `Resource`), `internal/auth/team.go`, `internal/service/practice.go` (`Actor`, `mayAccessPractitioner`, l. ~36-55), `internal/web/access.go` (construction de l'`Actor`), `internal/web/team.go` + `admin_team.html`, `migrations/00015_team.sql`, `internal/service/audit.go`, appelants de `MayAccessPractitioner` / `mayAccessPractitioner` (à lister avec LSP `findReferences`).

## Spécification
- Migration : `practitioner_delegation(id uuid PK, practice_id, user_id, practitioner_id, valid daterange NOT NULL CHECK (NOT isempty(valid)), created_by, created_at)` ; index `(user_id)` ; `EXCLUDE` optionnel pour éviter deux délégations identiques qui se chevauchent.
- `valid` est une plage de **jours dans le fuseau du cabinet** (`practice.timezone`, `Clock` injecté) : le jour courant local est calculé à chaque requête. Aucune tâche planifiée : l'expiration est un simple filtre sur la date.
- L'`Actor` / `auth.User` porte les praticiens délégués **actifs aujourd'hui** ; `MayAccessPractitioner` (ou son wrapper) en tient compte. **Chaque requête réévalue** (pas seulement au login) : charger les délégations actives avec la session de la requête, pas dans le cookie de session. Vérifier tous les appelants de `MayAccessPractitioner` (policy et couche service) et leur passer l'information, sans dupliquer la règle.
- Le remplaçant garde ses droits propres (rôle praticien ou secrétariat) ; la délégation n'ajoute que l'accès aux agendas des praticiens visés (agenda, RDV, séries, absences ; pas les réglages du cabinet).
- Admin : création/révocation depuis la page équipe (`admin_team.html`), choix de l'utilisateur, du praticien, des dates début/fin (inclusives) ; possible pour une invitation en attente (la délégation s'attache à l'utilisateur à l'acceptation, ou nécessite un compte existant — choisir la solution la plus simple et la documenter dans Suivi). Mêmes garde-fous que `team.go` (dernier admin, autre cabinet).
- `audit_log` : les actions du remplaçant gardent son `user_id` ; création/révocation de délégation auditées.

## Critères d'acceptation
- [x] Avant la période : 403 ; pendant : accès ; après : 403 (horloge fixe).
- [x] Frontière de jour en `Europe/Paris` (23:59 / 00:00 locaux, jour de changement d'heure) testée.
- [x] Révocation immédiate : l'accès tombe à la requête suivante.
- [x] Un praticien non admin ne peut pas créer de délégation ; autre cabinet refusé.
- [x] Audit : action du remplaçant = son user_id.

## Vérification
```bash
make sqlc
make check
```

## Hors scope
Facturation, rétrocession, délégation des réglages du cabinet.

## Suivi

> Rempli par l'agent pendant et à la fin de chaque session sur cette tâche. Le statut officiel est dans `tasks/README.md`.

- **Sessions** : 2026-10-17 (sonnet)
- **Fait** : migration `00021_practitioner_delegation.sql` (daterange + CHECK + EXCLUDE gist, index user) ; `queries/delegation.sql` (jour local calculé en SQL : `timezone(practice.timezone, now)::date`, `now` = `Clock` injecté) ; `auth.User.Delegated` et `service.Actor.Delegated`, rechargés par `Authenticate` à chaque requête (rien dans la session/cookie) ; `domain.MayAccessPractitioner(role, own, delegated, target)` reste la règle unique (appelée par `auth.Can` et `Actor.mayAccessPractitioner`, donc agenda, JSON, détail/déplacement/annulation/statut de RDV, absences, horaires) ; `auth/delegation.go` (Delegations, CreateDelegation, RevokeDelegation, audit `delegation_created` / `delegation_revoked`, meta = IDs et dates seulement) ; routes `POST /admin/team/delegations` et `/{id}/revoke` + section « Remplaçants » dans `admin_team.html` ; `make css` fait. Tests : `auth/delegation_test.go`, `auth/policy_test.go`, `web/delegation_test.go` (champ `clock` ajouté à `groupEnv`).
- **Reste à faire** : vérification visuelle de la section « Remplaçants » au navigateur (non faite).
- **Notes de reprise** : appelants de `MayAccessPractitioner` : `auth.Can` (2 sites), `Actor.mayAccessPractitioner` (TeamPractitioners, selectPractitioners, Move, Cancel, SetStatus, DeleteRule/absence l. ~627, targetPractitioner). L'ICS n'est pas concerné : le flux est celui du compte (`GetFeedTokenByUser`), jamais d'un autre praticien, et ne passe pas par `Target`. Un remplaçant ne reçoit donc pas l'agenda délégué dans son flux ICS.
- **Écarts avec la spécification** : (1) délégation réservée à un compte existant, actif, de rôle praticien (admin et secrétariat voient déjà tous les agendas, refusé avec message) ; pas de délégation sur invitation en attente (l'invité active d'abord son compte). (2) Durée limitée à 366 jours, fin déjà passée refusée, début dans le passé accepté. (3) Révocation = suppression de la ligne (l'historique est dans `audit_log`). (4) Un praticien désactivé n'est plus délégué (jointure `active`).
