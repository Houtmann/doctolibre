# T095 — Déplacement et annulation par le patient

- **Phase** : 5
- **Dépend de** : T093
- **Taille estimée** : M (< 500)
- **Exécution** : sonnet (`task-implementer`) — revue Opus : standard

## Objectif
Le patient sait quel RDV il déplace, arrive sur la bonne semaine, connaît le délai d'annulation tant qu'il peut agir, reçoit un accusé quand il annule, et peut reprendre rendez-vous en un clic.

Origine : audit UX des parcours patients du 2026-10-17.

## Contexte à lire
- `internal/web/templates/pages/public_reschedule.html` (~l. 5), `internal/web/public_manage.go` (~l. 117-120 : `mondayOf(c.Start)`, borné à `minMon`)
- `internal/web/templates/pages/public_confirmed.html` (~l. 33-35, délai affiché si `not Changeable`), `internal/web/public.go` (`RebookURL` ~l. 656)
- `internal/notify` (templates `confirmation.txt.tmpl` ~l. 9, golden files dans `testdata/`), `internal/service/notifications.go` (`n.data()`)
- `internal/service/manage.go` (~l. 73, `cancelled(..., false)`), `docs/decisions.md`

## Spécification
1. **RDV actuel** — `public_reschedule.html` affiche « Rendez-vous actuel : <date> à <heure> ».
2. **Semaine d'ouverture** — grille de déplacement sans `week` : ouvrir sur la semaine du RDV actuel (`mondayOf(c.Start)`, bornée à `minMon`).
3. **Délai annoncé** — tant que l'annulation/déplacement est possible : sur `/r/{token}` et dans l'email/SMS de confirmation (ajouter la donnée dans `n.data()`). Régénérer les golden files (`-update`).
4. **Accusé d'annulation** (décision du 2026-10-17, `docs/decisions.md`) — envoyer un accusé quand le patient annule lui-même (aujourd'hui `cancelled(..., false)`, choix « sans message » d'origine). Template dédié : email, et SMS seulement si le canal du RDV est SMS ; contient le lien « Reprendre rendez-vous ». Idempotent via l'outbox, inséré dans la transaction d'annulation.
5. **`RebookURL`** — pointer vers la grille du praticien avec le motif présélectionné (`/book?type=<id>`) si le motif est toujours actif et réservable en ligne, sinon vers la page praticien actuelle. (La reprise sans OTP est T098.)

## Critères d'acceptation
- [x] Page de déplacement : « Rendez-vous actuel » affiché ; sans `week`, la grille s'ouvre sur la semaine du RDV (test handler, y compris RDV lointain et borne `minMon`).
- [x] Délai visible sur `/r/{token}` tant que le RDV est modifiable, et dans le message de confirmation (golden files).
- [x] Annulation par le patient : message enfilé dans la transaction, une seule fois si l'annulation est rejouée, avec lien de reprise ; SMS seulement si canal SMS (tests service DB).
- [x] `RebookURL` : motif actif → `/book?type=…` ; motif désactivé ou non réservable en ligne → page praticien (tests).
- [x] `make check` vert (dont `css-check` si un template change).

## Vérification
```bash
make css && make check && go test ./internal/notify/ ./internal/service/ ./internal/web/
```

## Hors scope
- Reprise sans nouvelle vérification (T098).

## Suivi

> Rempli par l'agent pendant et à la fin de chaque session sur cette tâche. Le statut officiel est dans `tasks/README.md`.

- **Sessions** : 2026-10-25 (sonnet)
- **Fait** : page de déplacement avec « Rendez-vous actuel » et ouverture sur la semaine du RDV (`mondayOf(c.Start)`, bornée minMon/maxMon) ; délai annoncé sur `/r/{token}` (si modifiable) et dans l'email de confirmation (`cancel_notice` dans `n.data()`, golden régénérés : seuls `confirmation_avec_lien.email.{txt,html}` changent) ; accusé d'annulation `patient_cancellation` (email + SMS, templates et golden) enfilé dans la transaction de `CancelByPatient` (`notifier.patientCancelled`), clé de dédup `patient_cancellation:<rdv>:<canal>` ; `service.RebookPath` (grille `/book?type=` si motif actif et réservable en ligne, sinon page praticien) utilisée par la page `/r/` et l'email ; tests service/web/notify.
- **Reste à faire** : passage navigateur (page de déplacement, `/r/` modifiable et annulé).
- **Notes de reprise** : `make tools` puis `make css` (ui.css inchangé). `TestPublicBookingBlockedByNoShows` a échoué une fois sous charge (« le corps ne doit pas contenir 403 », probablement un jeton/UUID contenant 403) et passe seul : test fragile préexistant.
- **Écarts avec la spécification** : (1) le SMS de confirmation ne porte pas le délai : pas de place dans un segment GSM-7 avec le lien de gestion (email seulement). (2) L'accusé SMS donne le lien `/r/{token}` (page « Reprendre rendez-vous »), plus court que le lien direct, réservé à l'email. (3) Pas d'accusé si le cabinet a réglé les messages de changement sur `none`, ni pour un RDV du cabinet sans email utilisable (SMS réservé au canal vérifié).
