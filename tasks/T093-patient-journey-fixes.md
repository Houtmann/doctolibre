# T093 — Correctifs du parcours patient (offres, déplacement, vie privée)

- **Phase** : 5
- **Dépend de** : T088
- **Taille estimée** : M (< 500)
- **Exécution** : sonnet (`task-implementer`) — revue Opus : renforcée (garde-fou T054, no-op de déplacement)

## Objectif
Trois bugs et deux défauts relevés par l'audit UX des parcours patients : offre de liste d'attente envoyée à un patient bloqué, déplacement vers le même créneau qui envoie un faux message « déplacé », page vie privée publique avec consigne interne et sans lien depuis les formulaires.

Origine : audit UX des parcours patients du 2026-10-17.

## Contexte à lire
- `internal/service/waitlist_offers.go` (`offerTo` ~l. 87-190), `internal/service/booking.go` (`onlineBookingBlocked` ~l. 649), `internal/service/waitlist.go` (`Join` ~l. 333, `Claim` ~l. 599)
- `internal/service/manage.go` (`sc.ignore` ~l. 91, `RescheduleByPatient` ~l. 119-145), `internal/service/notifications.go` (`rescheduled` ~l. 290-306)
- `internal/web/public_manage.go` (`rescheduleGrid`, `rescheduleConfirm`, `BackURL` ~l. 209, flash `tooLate` ~l. 54-59), `internal/web/templates/pages/public_confirmed.html` (~l. 34, lien `tel:`)
- `internal/web/templates/pages/public_privacy.html` (l. 4), `public_details.html` (~l. 60), formulaire de liste d'attente, `layout/public.html`, `internal/web/routes.go` (~l. 48 et 99 : route vie privée enregistrée si `Privacy` non nil)

## Spécification
1. **Offre à un patient bloqué** — `offerTo` n'appelle pas `onlineBookingBlocked` (le garde-fou T054 n'existe qu'à `Join` et `Claim`). Après `GetPatient`, si bloqué : `return false, nil` (on passe à l'entrée suivante).
2. **Déplacement vers le même créneau** — `sc.ignore = a.ID` fait apparaître le créneau actuel comme libre ; au POST, `RescheduleByPatient` appelle `MoveAppointment` et `rescheduled` annule les rappels puis envoie un vrai email « déplacé ». Exclure de la grille de déplacement le créneau dont `Start` == début actuel (`rescheduleGrid` / `rescheduleConfirm`) et faire du même créneau un no-op dans `RescheduleByPatient` (retour sans écriture ni message).
3. **Page vie privée publique** — retirer de `public_privacy.html` la consigne interne « Modèle d'information à relire et compléter… » côté public (la garder éventuellement dans `/admin/privacy`). Ajouter un lien vers `/p/{slug}/privacy` dans le texte de la case de consentement (`public_details.html`, idem formulaire de liste d'attente) et dans le pied de `layout/public.html`, **uniquement si la route est active**.
4. **`BackURL` de la confirmation de déplacement** sans `?week=` : propager la semaine.
5. **Délai trop court** — le flash `tooLate` affiche le téléphone en texte brut : rediriger vers `/r/{token}` (qui affiche déjà le lien `tel:`) ou rendre le lien `tel:` cliquable.

## Critères d'acceptation
- [ ] Patient au seuil d'absences : aucune offre ; l'entrée suivante reçoit l'offre (test service DB).
- [ ] Déplacement vers le créneau actuel : absent de la grille ; POST forcé → aucune écriture, aucun message enfilé, rappels conservés (tests service + handler).
- [ ] Page vie privée publique sans consigne interne ; lien présent dans la case de consentement et le pied quand la route est active, absent sinon (tests handler).
- [ ] `BackURL` de la confirmation contient `week=` (test handler).
- [ ] Message de délai dépassé avec numéro cliquable (`href="tel:…"`) (test handler).
- [ ] `make check` vert (dont `css-check` si un template change).

## Vérification
```bash
make css && make check
```

## Hors scope
- Changer la règle de blocage T054 ; refondre la page vie privée (contenu juridique, T043).

## Suivi

> Rempli par l'agent pendant et à la fin de chaque session sur cette tâche. Le statut officiel est dans `tasks/README.md`.

- **Sessions** :
- **Fait** :
- **Reste à faire** :
- **Notes de reprise** :
- **Écarts avec la spécification** :
