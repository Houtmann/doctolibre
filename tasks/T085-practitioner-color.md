# T085 — Choix de la couleur du praticien

> Spécification rédigée le 2026-10-16 par l'orchestrateur, suite à l'écart signalé en T057 (Suivi) : la couleur du praticien est affichée dans l'agenda mais aucune interface ne permet de la choisir (tous les en-têtes sont au défaut `#2563eb`).

- **Phase** : 5
- **Dépend de** : T057
- **Taille estimée** : S
- **Exécution** : sonnet (`task-implementer`) — revue Opus : standard

## Objectif
Un praticien choisit sa couleur d'agenda depuis son profil, et la personne qui gère l'équipe peut régler celle de chaque praticien du cabinet ; la couleur enregistrée apparaît sur l'en-tête de colonne de la vue « Par praticien ».

## Contexte à lire
T057 (Suivi), `internal/domain/color.go` (`ValidColor`, `DefaultColor`), `internal/service/practice.go` (mise à jour du profil : `UpdatePractitioner` reprend `pr.Color` tel quel), `internal/service/appointment_types.go` (validation de couleur d'un motif, erreur de champ `color`, modèle à suivre), `internal/web/templates/partials/types_panel.html` (champ `type="color"` existant, motif de champ daisyUI de T082), le formulaire de profil (page de réglages qui appelle la mise à jour ci-dessus), `internal/web/team.go` + `pages/admin_team.html`, `internal/auth` (`auth.Can`, permissions) et `domain.MayAccessPractitioner`.

## Spécification
- **Service** : une seule voie d'écriture de la couleur, validée par `domain.ValidColor` après `strings.TrimSpace` ; couleur invalide ou vide → `domain.ValidationErrors{{Field: "color", Msg: …}}`, **rien n'est écrit** (ni la couleur ni le reste du formulaire, tout dans la même transaction). Pas de nouvelle regex.
  - Profil : la mise à jour du profil praticien prend la couleur saisie au lieu de `pr.Color`.
  - Équipe : méthode `SetPractitionerColor(ctx, actor, practitionerID, color)` (nom indicatif), qui vérifie dans le service que l'acteur a la permission de gérer l'équipe (`auth.Can`, même permission que les autres actions de `team.go`) **et** que le praticien appartient à son cabinet (`practice_id` dans la requête SQL ; `domain.MayAccessPractitioner` si c'est la règle déjà utilisée pour ce cas). Praticien d'un autre cabinet → `ErrNotFound` ; permission manquante → `ErrForbidden`. Requête sqlc dédiée (`UPDATE practitioner SET color = $3 WHERE id = $1 AND practice_id = $2`) ou réutilisation de `UpdatePractitioner` : au choix, documenté dans Suivi.
- **Profil** (page de réglages) : champ « Couleur dans l'agenda », `<input type="color" name="color">` avec `<label>`, valeur actuelle (`domain.ColorOrDefault`), erreur de champ affichée selon le motif daisyUI de T082 (même rendu que `types_panel.html`).
- **Équipe** (`/admin/team`) : pour chaque membre lié à un praticien, un petit formulaire POST (couleur + bouton « Enregistrer »), CSRF, POST classique + redirect avec flash, htmx en amélioration seulement si trivial. Visible seulement pour qui a la permission de gérer l'équipe.
- Aucun `style=` dans les templates, aucune classe construite dynamiquement, pas d'hex hors des blocs de thème d'`input.css` (la valeur du champ couleur n'est pas un style). `make css` après modification des templates.
- Audit : si les autres actions de `team.go` écrivent dans `audit_log`, faire de même pour le changement de couleur (IDs seulement).

## Critères d'acceptation
- [x] Profil : enregistrer une couleur valide la persiste ; une couleur invalide (`rouge`, `#12345`, `url(x)`, vide) → 422 / erreur de champ `color`, rien d'écrit, y compris les autres champs du formulaire (tests service + handler).
- [x] Équipe : un gestionnaire change la couleur d'un praticien de son cabinet ; un rôle sans permission → 403 ; un praticien d'un autre cabinet → 404 ; couleur invalide → erreur, rien d'écrit (tests).
- [x] Les deux formulaires fonctionnent sans JS (POST + redirect, test handler).
- [ ] Dans le navigateur : la couleur choisie au profil puis à l'équipe apparaît sur l'en-tête de colonne de la vue « Par praticien » (`resourceTimeGridDay`), clair et sombre ; aucune violation CSP.
- [x] Aucun `style=` dans les templates ; `make css` à jour ; `make check` vert.

## Vérification
```bash
make css && make check
```

## Hors scope
Palette imposée ou suggestions de couleurs, couleur de fond des RDV, couleur dans le repli sans JS, séparation profil / cabinet de la page de réglages (T056 : si T056 avance en parallèle, conflit probable sur le formulaire de profil, ne pas les lancer ensemble).

## Suivi

> Rempli par l'agent pendant et à la fin de chaque session sur cette tâche. Le statut officiel est dans `tasks/README.md`.

- **Sessions** : 2026-10-16 (sonnet)
- **Fait** : profil (`SettingsInput.Color`, validé par `domain.ValidColor` avant la transaction, donc rien d'écrit si invalide ; vide refusé), champ `type="color"` dans `admin_settings.html`. Équipe : `auth.Service.SetPractitionerColor` (la gestion d'équipe vit dans `auth`, pas dans `service` : `requireTeamAdmin`/`ActionTeamManage`), requête sqlc dédiée `SetPractitionerColor :execrows` (filtre `practice_id`, 0 ligne -> ErrNotFound), audit `practitioner_color_changed` (entité practitioner, sans meta). Route `POST /admin/team/practitioners/{id}/color` (id = praticien), colonne « Couleur » dans `admin_team.html`, `ListTeamUsers` renvoie la couleur. Tests : service, handler profil, handler équipe (303/422/403/404). `make css` et `make check` verts.
- **Reste à faire** : vérification navigateur (en-tête de colonne « Par praticien », clair/sombre, CSP).
- **Notes de reprise** : `Non vu` : le critère navigateur n'a pas été vérifié, ni la CSP. Les erreurs de champ du profil s'affichent via le récapitulatif `form_errors` (comme `types_panel`), pas de marquage par champ.
- **Écarts avec la spécification** : `SetPractitionerColor` est dans `auth` (là où vit la logique d'équipe) et non dans `service`. Les tests existants envoyant le formulaire de profil ont dû ajouter `color` (champ désormais obligatoire).
