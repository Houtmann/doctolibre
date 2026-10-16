# T065 — Design v2 : formulaires et tableaux

- **Phase** : 5
- **Dépend de** : T064
- **Taille estimée** : M
- **Exécution** : sonnet (`task-implementer`) — revue Opus : standard (+ captures avant/après clair/sombre, 360 et 1280 px)

## Objectif
Tous les formulaires (connexion, mot de passe, réglages, motifs, disponibilités, absences, équipe, patient, nouveau RDV, séries) et toutes les listes tabulaires utilisent `.dl-field`/`.dl-input`/`.dl-select`/`.dl-textarea`/`.dl-check`/`.dl-fieldset` et `.dl-table`.

## Contexte à lire
`design/README.md`, `design/MIGRATION.md`, la ou les cartes `design/previews/` citées ci-dessous, `/dev/styleguide/v2` (T063), `partials/{patient_fields,appointment_form,availability_panel,absences_panel,types_panel,patient_results}.html`, pages `login`, `password_*`, `invitation`, `admin_{settings,team,types,availability,absences,patient,patient_new,patients,appointment_new,care_plan_new,messages,audit,notifications,waitlist,privacy,status}.html`. Cartes : `forms`, `table`. Point connu (STATUS.md) : la CSP bloque les icônes SVG `data:` de Pico (case à cocher, date) — les contrôles v2 ne doivent pas en dépendre.

## Spécification
- Chaque champ : `<div class="dl-field">` + `<label class="dl-label">` + contrôle + `.dl-help` éventuel ; en erreur, `.is-invalid` sur `.dl-field` et `aria-invalid="true"` + `aria-describedby` vers le message.
- Champ libre « note administrative » : `.dl-note-warning` (avertissement « pas de donnée médicale »), à la place de l'actuel.
- Listes (patients, messages, audit, équipe, liste d'attente, statut) → `.dl-table-wrap` > `table.dl-table` ; nombres et heures en `.dl-num`. Corrige au passage la puce carrée et le contact à la ligne de `patient_results` (défaut connu).
- Règles communes : remplacer les classes v1 par les classes v2 selon `design/MIGRATION.md` ; les tests `httptest` qui cherchent d'anciennes classes sont mis à jour (fragments v2) ; aucun `style=` ni script inline ; les formulaires marchent toujours sans JS ; les variantes htmx des partials gardent les mêmes `id`/`hx-target`. Si une classe v1 n'est plus utilisée nulle part après la tâche, la retirer d'`app.css` ; sinon la laisser (T068 nettoie).
- Composant manquant ou inadapté : l'ajouter dans `internal/web/static/components.css` **et** `design/components.css` (jetons sémantiques uniquement), et le signaler dans Suivi.

## Critères d'acceptation
- [ ] Aucun `<input>`/`<select>`/`<textarea>` visible sans classe `.dl-*` dans les templates listés (`grep` dans Suivi).
- [ ] Un formulaire en erreur rend `dl-field is-invalid` et `aria-invalid="true"` (test httptest, ex. connexion ou nouveau patient).
- [ ] `make check` vert, aucun `style=` ajouté.
- [ ] Vérifié dans le navigateur à 360 et 1280 px, clair et sombre, et au clavier (focus visible).

## Vérification
```bash
make check
grep -rnE '<(input|select|textarea)\b' internal/web/templates | grep -v 'dl-\|type="hidden"'
```

## Hors scope
Parcours patient (T066), agenda et dialogue (T067), retrait de Pico (T068).

## Suivi

> Rempli par l'agent pendant et à la fin de chaque session sur cette tâche. Le statut officiel est dans `tasks/README.md`.

- **Sessions** : —
- **Fait** : —
- **Reste à faire** : —
- **Notes de reprise** (pièges, état intermédiaire, commandes utiles) : —
- **Écarts avec la spécification** (et pourquoi) : —
