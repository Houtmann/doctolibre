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
- [x] Aucun `<input>/`<select>`/`<textarea>` visible sans classe `.dl-*` dans les templates listés (`grep` dans Suivi).
- [x] Un formulaire en erreur rend `dl-field is-invalid` et `aria-invalid="true"` (test httptest, ex. connexion ou nouveau patient).
- [x] `make check` vert, aucun `style=` ajouté.
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

- **Sessions** : 2026-10-16 (sonnet)
- **Fait** :
  - Tous les champs des templates listés en `.dl-field` + `.dl-label` + `.dl-input/.dl-select/.dl-textarea`, cases en `label.dl-check`, groupes en `.dl-fieldset` (+ `.dl-checks`), aide en `p.dl-help`, avertissement note administrative en `.dl-note-warning` (patient_fields, care plan). Formulaires de saisie : `class="dl-form"` (max 40 rem). Rangées de champs (ex-`.grid` Pico) : `.dl-field-row`. Recherches : `.dl-search`.
  - Tableaux (équipe, motifs, liste d'attente, messages, journal, statut, RGPD, patients, lignes de série) : `.dl-table-wrap > table.dl-table`, heures/dates/nombres en `.dl-num`, actions de ligne dans `div.dl-cluster` ; états vides en `.dl-empty`. `patient_results` : `ul.dl-choice-list` + `label.dl-check` (puce ronde native, contact sur la même ligne).
  - Erreurs par champ : `PatientForm.Invalid map[string]bool` (helper `invalidFields(err)` dans admin_config.go, rempli à la création et à l'édition d'un patient) → `dl-field is-invalid` + `aria-invalid="true"` + `aria-describedby` (aide et `#form-errors`). Connexion : `LoginData.Failed` marque email et mot de passe. Lignes de série en conflit : `is-invalid` sur le champ.
  - Pages invalides (lien expiré) et doublons de patient : `.dl-alert`.
  - Tests : assertions ajoutées dans `TestLoginFailureIsGeneric`, `TestPatientEditAndNoMedicalWarning`, nouveau `TestPatientNewInvalidMarksFields`.
  - CSS : section `/* ── T065 ── */` en fin de `design/components.css` et `static/components.css` (identiques) : `.dl-form`, `.dl-field-row`, `.dl-checks`, `.dl-choice-list`, `.dl-details-form`, `.dl-search`, règles de tableau (th de ligne, cellules d'actions). `app.css` : retrait de `.patient-results` uniquement.
- **Reste à faire** : vérification navigateur des pages authentifiées (360/1280 px, clair/sombre, clavier) : seule `/dev/styleguide` (patient_fields, patient_results) a été vue (≈520 px, clair, pas de débordement). Critère 4 non coché.
- **Notes de reprise** : `appointment_form` (dialogue) est mis en page en `.dl-form` ; T067 peut avoir à ajuster dans le dialogue. Les champs en erreur de la création rapide d'un patient dans le formulaire de RDV et ceux des autres formulaires (réglages, motifs…) n'ont que le message dans l'alerte (les handlers ne remontent pas les champs) ; seuls patient (création/édition) et connexion marquent les champs. `.week-grid`/`.week-day` (horaires) et `.is-inactive` restent dans `app.css` (utilisés, T068). `dev_styleguide.html` v1 hérite du nouveau `patient_fields`.
- **Écarts avec la spécification** : `admin_dashboard.html` (champ « adresse à copier ») converti aussi (hors liste, mais couvert par le grep). Portée « is-invalid » limitée aux formulaires où le champ est identifiable (patient, connexion, série).
- **Contrôle orchestrateur (2026-10-16)** : `make check` ✅ après fusion des trois branches ; grep des champs sans classe : seuls restent le honeypot (`.dl-hp`) et une case dans `label.dl-check` (agenda). Navigateur : `patient_fields` et `patient_results` (styleguide) à 1280 px clair ; la connexion à 1280 px. Pages admin authentifiées non revues : session de démo expirée (l'orchestrateur ne saisit pas de mot de passe) → à regarder par l'humain. Question ouverte : étendre `is-invalid` aux autres formulaires admin (tâche à part).
- **Clôture (2026-10-16)** : `done` — remplacée par T080–T084 (migration daisyUI, voir `docs/decisions.md`). Le travail est mergé mais son CSS `dl-*` est supprimé en T084.
