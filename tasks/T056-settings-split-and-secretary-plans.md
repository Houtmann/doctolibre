# T056 — Réglages : profil vs cabinet, séries au secrétariat

- **Phase** : 5
- **Dépend de** : T033, T030
- **Taille estimée** : S
- **Exécution** : sonnet (`task-implementer`) — revue Opus : standard

## Objectif
Séparer ce qu'un praticien règle pour lui-même de ce qui engage le cabinet, et laisser le secrétariat gérer les séries de séances.

## Contexte à lire
`internal/web/admin_config.go` (`configRoutes`, `settingsForm`, `settingsSubmit`, `notifyForm`), `internal/service/practice.go` (`SettingsInput` : champs cabinet `Name, Address, Phone, BookingNoticeHours, CancelNoticeHours, HorizonDays, HomeVisit*` ; champs praticien `FirstName, LastName, Title, Bio, Slug`), `internal/web/templates/pages/admin_settings.html`, `internal/auth/policy.go` (`ActionPracticeConfig`, `ActionCarePlanManage`), `internal/auth/policy_test.go`, `internal/web/care_plans.go` (routes et `require`), `internal/domain/role.go`, `docs/` (doc des rôles, p. ex. `docs/install.md`/`README`).

## Spécification
- **(a) Scission de `/admin/settings`** : « Mon profil » (prénom, nom, titre, bio, slug, couleur, flux ICS…) accessible à tout praticien sur lui-même ; « Réglages du cabinet » (nom, adresse, téléphone, délais, fenêtre de réservation, domicile, notifications, fuseau…) réservé à l'admin. Ajouter une action `ActionPracticeSettings` (ou renommer l'usage de `ActionPracticeConfig`) dans `auth.Can` si nécessaire. Répartir précisément les champs d'après le formulaire actuel et **lister la répartition dans Suivi**. Un praticien non admin qui poste un champ cabinet reçoit 403 (pas d'ignorance silencieuse). Les liens `/admin/settings` existants doivent continuer à fonctionner (redirection ou page profil).
- **(b)** Ouvrir `ActionCarePlanManage` au rôle `secretary` (aujourd'admin + praticien, même condition que `ActionPracticeConfig` dans `Can`) ; adapter les routes de `care_plans.go` (le secrétariat choisit le praticien visé, vérifier `Actor.Target`/`MayAccessPractitioner`) et retirer la mention « 403 secrétariat » des tests de `care_plans_test.go`.
- Mettre à jour les tests de matrice (`policy_test.go`) et la doc des rôles.
- Retirer les deux questions correspondantes de `STATUS.md` ne fait **pas** partie de la tâche (orchestrateur).

## Critères d'acceptation
- [ ] Un praticien non admin modifie son profil, pas les réglages du cabinet (403 testé).
- [ ] L'admin modifie les deux.
- [ ] Le secrétariat crée, ajuste et change le statut d'une série ; autres rôles inchangés.
- [ ] Matrice `auth.Can` testée pour les nouvelles combinaisons ; doc des rôles à jour.

## Vérification
```bash
make check
```

## Hors scope
Nouveaux rôles, édition des horaires par le secrétariat, mise à jour de `STATUS.md`.

## Suivi

> Rempli par l'agent pendant et à la fin de chaque session sur cette tâche. Le statut officiel est dans `tasks/README.md`.

- **Sessions** : —
- **Fait** : —
- **Reste à faire** : —
- **Notes de reprise** (pièges, état intermédiaire, commandes utiles) : —
- **Écarts avec la spécification** (et pourquoi) : —
