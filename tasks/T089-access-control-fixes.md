# T089 — Contrôle d'accès : séries d'un confrère, praticien implicite, reset d'un compte désactivé

- **Phase** : 5
- **Dépend de** : T056, T059
- **Taille estimée** : S (< 200)
- **Exécution** : sonnet (`task-implementer`) — revue Opus : renforcée (sécurité)

## Objectif
Un praticien (ou un remplaçant) n'agit que sur les séries des praticiens auxquels il a accès ; la création d'un RDV sans praticien désigné ne crée jamais de fiche praticien parasite ni de 403 incompréhensible ; un compte désactivé ne garde aucun jeton de réinitialisation.

## Contexte à lire
- `internal/service/care_plans.go` (`AddSession` ~l. 721, `SetStatus` ~l. 786, autres méthodes pour le modèle), `internal/service/practice.go` (`mayAccessPractitioner`, `Practitioner`, `targetPractitioner`, `ensurePractitioner`)
- `internal/web/care_plans.go` (routes `status`, `replace`, sessions), `internal/web/calendar.go` (`formPage`, `create`), `internal/web/templates/partials/appointment_form.html`
- `internal/auth/team.go` (`SetActive`), `internal/auth/passwordreset.go`, `internal/store/queries/password_reset.sql` (`DeletePasswordResetsByUser` existe)
- Suivi de T056 (il note que `status`/`replace` ne vérifient pas le praticien de la série)

## Spécification
1. **Séries d'un confrère (critique, confirmé)** — `AddSession` et `SetStatus` ne vérifient pas `mayAccessPractitioner(cp.PractitionerID)` et la route web n'a pas de `withTarget` : tout praticien du cabinet (ou un remplaçant délégué pour A seulement) peut planifier une séance dans l'agenda de B (avec notification au patient) ou clore la série de B. Dans les deux méthodes, lire la série dans la transaction puis `if !a.mayAccessPractitioner(cp.PractitionerID) { return domain.ErrNotFound }` (ne pas révéler l'existence). Passer en revue **toutes** les méthodes de `CarePlans` qui prennent un ID de série (dont `replace`) et appliquer la même règle. Tests service + handler : praticien B → 404 sur la série de A ; remplaçant délégué pour A → OK sur A, 404 sur B ; secrétariat (ouvert par T056) inchangé ; admin OK.
2. **Praticien implicite à la création d'un RDV** — si `/admin/appointments/new` est ouvert sans `?practitioner=` (accès direct), `a.Target == uuid.Nil` → `ensurePractitioner` : 403 pour la secrétaire, fiche praticien parasite pour un admin non praticien. Dans `formPage`, si un seul praticien est visible et `Form.Practitioner` vide, le présélectionner (champ caché). Dans le service, n'appeler `ensurePractitioner` que pour un acteur de rôle praticien ; sinon, `Target` nul → `ErrValidation{Field: "practitioner", Msg: "Choisissez le praticien."}`. Tests : secrétaire, cabinet mono-praticien, formulaire sans paramètre → champ présent et création OK ; admin non praticien sans cible → erreur de validation, aucune ligne `practitioner` créée.
3. **Compte désactivé** — `SetActive(false)` ajoute `DeletePasswordResetsByUser` dans la même transaction ; `ResetPassword` refuse un compte inactif. Test : jeton émis, compte désactivé → reset refusé.

## Critères d'acceptation
- [ ] Aucune méthode de `CarePlans` n'agit sur la série d'un praticien inaccessible (tests cross-praticien et délégation).
- [ ] Secrétaire en cabinet mono-praticien crée un RDV même sans paramètre d'URL ; plus de fiche praticien parasite.
- [ ] Désactivation = sessions et jetons de reset supprimés (test).
- [ ] `make check` vert.

## Vérification
```bash
make check
go test ./internal/service/ ./internal/web/ ./internal/auth/ -run 'CarePlan|Appointment|SetActive|Reset' -count=1
```

## Hors scope
Horaires hebdomadaires du titulaire modifiables par le remplaçant (question produit ouverte dans STATUS.md, voulu par `TestCanWithDelegation`) ; refus du rôle vide dans `Actor` (convention « appel interne », à traiter à part si voulu).

## Suivi

> Rempli par l'agent pendant et à la fin de chaque session sur cette tâche. Le statut officiel est dans `tasks/README.md`.

- **Sessions** : —
- **Fait** : —
- **Reste à faire** : —
- **Notes de reprise** (pièges, état intermédiaire, commandes utiles) : —
- **Écarts avec la spécification** (et pourquoi) : —
