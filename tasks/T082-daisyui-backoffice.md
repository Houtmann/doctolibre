# T082 — Back-office : formulaires et tableaux en daisyUI

- **Phase** : 5
- **Dépend de** : T080
- **Taille estimée** : L
- **Exécution** : sonnet (`task-implementer`) — revue Opus : standard (+ captures 360 et 1280 px, clair et sombre)

## Objectif
Toutes les pages de gestion du cabinet (hors agenda, tableau de bord et connexion) utilisent les composants daisyUI : formulaires, tableaux, pagination, états vides, alertes.

## Contexte à lire
T080 (Suivi, `/dev/styleguide`, `docs/design-system.md`), `AGENTS.md` section Front, `poc/daisyui/` (tableaux et formulaires de référence), `internal/web/render_test.go`, tests handlers de ces pages. Rappel T065 (Suivi) : `PatientForm.Invalid`, `invalidFields(err)` ; conserver `aria-invalid` et `aria-describedby`.

## Spécification
Templates **de cette tâche** (et eux seuls) :
- Pages : `pages/admin_patients.html`, `admin_patient.html`, `admin_patient_new.html`, `admin_care_plan_new.html`, `admin_types.html`, `admin_availability.html`, `admin_absences.html`, `admin_settings.html`, `admin_team.html`, `admin_waitlist.html`, `admin_privacy.html`, `admin_audit.html`, `admin_messages.html`, `admin_notifications.html`.
- Partials : `partials/patient_fields.html`, `patient_results.html`, `types_panel.html`, `availability_panel.html`, `absences_panel.html`.

Contenu : `table` daisyUI (conteneur à défilement horizontal sur mobile, colonnes secondaires masquées en petit écran si nécessaire), pagination (`join`), formulaires `fieldset`/`label`/`input`/`select`/`textarea`/`checkbox`, avertissement « note administrative : pas de donnée médicale » en `alert`, erreurs de champ (`input-error` + texte d'aide, `aria-invalid`, `aria-describedby`), états vides, boutons d'action de ligne. Aucune logique ni donnée nouvelle ; `id`/`hx-target` des partials conservés ; formulaires fonctionnels sans JS.

Règles communes : `ui.css` est **généré** : `make css` après toute modif de template ; en cas de conflit au merge, **relancer `make css`** plutôt que résoudre à la main. Un seul agent à la fois sur les templates de layout (non modifiés ici). Pas de classe construite dynamiquement. Tests adaptés (fragments clés).

## Critères d'acceptation
- [x] `grep -rn 'dl-'` sur les templates listés : vide.
- [x] `grep -rnE '<(input|select|textarea|button|table)\b'` sur ces templates : chaque élément porte une classe daisyUI (hors `type="hidden"`).
- [x] Un formulaire en erreur rend `input-error` (ou équivalent) et `aria-invalid="true"` (test `httptest` existant adapté).
- [ ] Chaque page vérifiée dans le navigateur à 360 et 1280 px, clair et sombre, focus visible au clavier, sans débordement horizontal.
- [x] `make css` ne produit aucun diff après commit ; `make check` vert, aucun `style=` ajouté.

## Vérification
```bash
make css && git diff --exit-code internal/web/static/ui.css
make check
```

## Hors scope
Layouts (T080), parcours patient (T081), agenda, dialogue de RDV et leurs partials (T083), connexion, tableau de bord, statut (T084), nouvelles fonctionnalités.

## Suivi

> Rempli par l'agent pendant et à la fin de chaque session sur cette tâche. Le statut officiel est dans `tasks/README.md`.

- **Sessions** : 2026-10-16 (sonnet)
- **Fait** : les 14 pages et 5 partials de la spec migrés (script perl de remplacement des classes `dl-*` puis retouches à la main) : champs `div.space-y-1` + `label.block.text-sm.font-medium` + `input/select/textarea w-full`, cases `checkbox checkbox-sm` + `text-sm`, `fieldset` daisyUI (`fieldset-legend`), `table` dans `overflow-x-auto` avec colonnes secondaires `hidden sm:/md:table-cell` (patients, équipe, liste d'attente, messages, motifs), pagination en `join`, états vides en `alert alert-info alert-soft`, note administrative en `alert alert-warning alert-soft`, semaine de disponibilités en cartes. `PatientForm.Invalid` : `input-error`/`textarea-error` + `aria-invalid` + `aria-describedby` conservés ; ligne en conflit de la série : `input-error` + `aria-invalid`. `id`, `name`, `hx-*`, `action`, CSRF inchangés. `patients_test.go` adapté (`input-error`). `make css`, `make check` verts, `css-check` sans diff.
- **Reste à faire** : contrôle au clavier (focus visible) non fait ; clair/sombre et 1280 px vus seulement sur un échantillon (voir ci-dessous).
- **Notes de reprise** : vérifié au navigateur (base `doctolibre_demo`, aucune écriture) : 360 px clair, `scrollWidth` = 360 sur toutes les pages (patients, fiche, nouveau, série + proposition, motifs, horaires, absences, paramètres, notifications, équipe, liste d'attente, confidentialité, journal, messages) ; 1280 px sombre sur patients, horaires, motifs, journal (pas de débordement). Non vus : 1280 clair, 360 sombre, états avec données (liste d'attente remplie, absences, invitations, doublons, erreurs de champ à l'écran, flux ICS), `patient_results` (dans le dialogue RDV de T083), pagination affichée.
- **Correctif revue Opus** : équipe : select de rôle `w-full min-w-40`, form en colonne sous `lg` ; journal : Objet/Détails masqués sous `md`, Utilisateur sous `sm`, date en nowrap. Vérifié à 375 px sombre et 1280 px clair : scrollWidth = largeur, lignes équipe 96 px, journal ~50 px (à 1280 : 66 px).
- **Écarts avec la spécification** : `patient_fields` : les 4 champs identité/contact sont dans une seule grille (l'aide « téléphone ou email » est après la grille). Colonnes masquées en mobile : email (patients), dernière connexion (équipe), jours/heures/origine (liste d'attente), tentatives (messages), marges/options (motifs). `admin_test.go` et `styleguide_test.go` cherchent encore `dl-field is-invalid` (login T084, styleguide) : non touchés.
