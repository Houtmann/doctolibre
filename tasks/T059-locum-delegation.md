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
- [ ] Avant la période : 403 ; pendant : accès ; après : 403 (horloge fixe).
- [ ] Frontière de jour en `Europe/Paris` (23:59 / 00:00 locaux, jour de changement d'heure) testée.
- [ ] Révocation immédiate : l'accès tombe à la requête suivante.
- [ ] Un praticien non admin ne peut pas créer de délégation ; autre cabinet refusé.
- [ ] Audit : action du remplaçant = son user_id.

## Vérification
```bash
make sqlc
make check
```

## Hors scope
Facturation, rétrocession, délégation des réglages du cabinet.

## Suivi

> Rempli par l'agent pendant et à la fin de chaque session sur cette tâche. Le statut officiel est dans `tasks/README.md`.

- **Sessions** : —
- **Fait** : —
- **Reste à faire** : —
- **Notes de reprise** (pièges, état intermédiaire, commandes utiles) : —
- **Écarts avec la spécification** (et pourquoi) : —
