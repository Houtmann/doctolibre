# T070 — Consignes par motif et prérequis à la réservation

> UI à faire en daisyUI ; à lancer après T080 (décision du 2026-10-16).

- **Phase** : 5
- **Dépend de** : T014, T023, T058
- **Taille estimée** : S
- **Exécution** : sonnet (`task-implementer`) — revue Opus : standard

## Objectif
Chaque motif peut porter des consignes pratiques pour le patient (« apporter l'ordonnance, une serviette, une tenue souple »), affichées avant la réservation, sur la confirmation, dans les emails et dans l'ICS ; le cabinet peut exiger que le patient les ait lues avant de réserver.

## Contexte à lire
`internal/store/queries/` (requêtes `appointment_type`), `internal/service/` (motifs : `admin_types`, lecture du motif dans la réservation `booking.go`), `internal/web/templates/pages/admin_types.html`, `public_book.html`, `public_details.html`, `public_confirmed.html`, `internal/notify/templates/email/{confirmation,reminder,reschedule,series}.*.tmpl`, `internal/ics/ics.go` (champ `Description`, alarmes T058), T032 Suivi (pourquoi les requêtes `Create/UpdateAppointmentType` n'ont pas été modifiées : setter dédié).

## Spécification
- Migration (prochain numéro libre, ≥ 00020) :
  - `appointment_type.patient_instructions text NOT NULL DEFAULT '' CHECK (char_length(patient_instructions) <= 1000)` ;
  - `appointment_type.instructions_ack_required boolean NOT NULL DEFAULT false`.
- Texte brut uniquement : jamais interprété comme HTML (échappement `html/template` par défaut), sauts de ligne conservés (CSS `white-space: pre-line` via une classe du design system, pas de style inline). Espaces de bord retirés à l'enregistrement.
- Admin (`admin_types.html`) : champ `<textarea>` « Consignes pour le patient » + case « Le patient doit confirmer avoir lu les consignes » ; avertissement visible : **pas de donnée médicale** (même formule que la note administrative). Setter dédié (`SetAppointmentTypeInstructions`) si modifier les requêtes de création casse trop de fixtures.
- Réservation en ligne : consignes affichées dès que le motif est choisi (étape coordonnées `public_details.html`, avant l'OTP). Si `instructions_ack_required` : case à cocher obligatoire « J'ai lu les consignes » ; vérifiée **côté serveur** (`ErrValidation{Field: "instructions_ack"}`), pas seulement `required` HTML. La case n'est pas stockée (pas de nouvelle donnée patient).
- Saisie par le cabinet : pas de case à cocher ; les consignes partent quand même dans les messages.
- Consignes reprises dans :
  - la page de confirmation `public_confirmed.html` et la page de gestion `/r/{token}` ;
  - les emails confirmation, rappel, report et récapitulatif de série (bloc « À prévoir », omis si vide) ;
  - l'ICS patient (`DESCRIPTION`, après le texte existant) ; **pas** le flux ICS du cabinet (T042), inutile.
  - **Pas dans les SMS** (coût : un SMS de plus par message ; le lien de gestion suffit).
- Les consignes sont lues au moment de l'envoi (pas copiées dans le RDV) : une correction du cabinet profite aux rappels suivants. Le documenter dans l'aide du champ.
- Golden files emails / ICS mis à jour (`-update`).

## Critères d'acceptation
- [ ] Consignes saisies dans l'admin, affichées à la réservation, sur la confirmation, sur `/r/{token}`.
- [ ] Case obligatoire : réservation refusée sans la case (test handler, requête POST sans le champ), acceptée avec.
- [ ] Emails (confirmation, rappel, report, série) et ICS patient contiennent les consignes ; rien quand le champ est vide (golden files).
- [ ] SMS inchangés (test).
- [ ] `<script>` saisi dans les consignes rendu échappé (test).
- [ ] 1001 caractères refusés avec un message sur le champ.

## Vérification
```bash
make sqlc
make check
```

## Hors scope
Consignes par praticien, mise en forme riche (Markdown), pièces jointes, questionnaire préalable, consignes dans les SMS.

## Suivi

> Rempli par l'agent pendant et à la fin de chaque session sur cette tâche. Le statut officiel est dans `tasks/README.md`.

- **Sessions** : —
- **Fait** : —
- **Reste à faire** : —
- **Notes de reprise** (pièges, état intermédiaire, commandes utiles) : —
- **Écarts avec la spécification** (et pourquoi) : —
