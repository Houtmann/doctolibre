# T103 — Téléphone formaté dans l'aperçu d'import CSV

- **Phase** : 5
- **Dépend de** : T100
- **Taille estimée** : S (< 200 lignes)
- **Exécution** : sonnet (`task-implementer`) — revue Opus : standard

## Objectif
L'aperçu d'import de patients (`/admin/patients/import`, après envoi du fichier) affiche le téléphone comme le reste de l'interface depuis T100 (`06 12 34 56 78`), et non plus en E.164 brut (`+33612345678`).

## Contexte à lire
- `internal/web/templates/pages/admin_patient_import.html` (tableau d'aperçu, colonne « Téléphone », vers la l. 35)
- `internal/web/render.go` (fonction de template `formatPhone`, l. ~499) et `domain.FormatPhone`
- `internal/web/patient_import_test.go` (`TestPatientImportFlowForEveryRole`, assertion `33612345678` sur l'aperçu)
- Décision du 2026-11-19 sur `formatPhone` dans `docs/decisions.md` (affichage seulement)

## Spécification
1. Dans le tableau d'aperçu, afficher `{{formatPhone .Phone}}` au lieu de `{{.Phone}}`. Une ligne sans téléphone reste vide (vérifier le comportement de `FormatPhone("")`).
2. **Ne pas changer** le `payload` caché (JSON des lignes validées) ni ce qui est enregistré : le stockage reste en E.164.
3. Test : adapter `TestPatientImportFlowForEveryRole` pour vérifier que l'aperçu contient `06 12 34 56 78` dans une cellule du tableau, et que l'import final enregistre toujours `+33612345678` (si le test le vérifie déjà, garder). Attention : `33612345678` peut aussi apparaître dans le `payload` (attribut `value`, échappé) ; l'assertion sur l'affichage doit viser la cellule formatée, pas cette chaîne.
4. Pas de nouvelle classe CSS : `make css` ne doit rien changer (le vérifier).

## Critères d'acceptation
- [x] L'aperçu affiche `06 12 34 56 78` pour `06 12 34 56 78` saisi dans le CSV.
- [x] Le patient importé est stocké en `+33612345678`.
- [x] Test adapté, `make check` vert, 0 test DB sauté.

## Vérification
```bash
make check
```

## Hors scope
- Changer la normalisation des numéros à l'import ou le format de stockage.
- Toute autre page.

## Suivi

> Rempli par l'agent pendant et à la fin de chaque session sur cette tâche. Le statut officiel est dans `tasks/README.md`.

- **Sessions** : 2026-11-22 (sonnet)
- **Fait** : template `admin_patient_import.html` l. 35 : `{{formatPhone .Phone}}` (payload caché inchangé ; `FormatPhone("")` renvoie "" donc cellule vide). Test `TestPatientImportFlowForEveryRole` : l'aperçu doit contenir `<td class="tabular-nums">06 12 34 56 78</td>` ; après confirmation, requête SQL vérifiant `phone = '+33612345678'`. `make check` compile/lint/css-check OK.
- **Reste à faire** : rien. Vérifié sur postgres:17-alpine jetable : `make check` vert, 0 SKIP dans internal/web.
- **Notes de reprise** : le `payload` est du base64 du CSV, la chaîne `33612345678` n'y apparaissait pas ; l'ancienne assertion visait bien la cellule.
- **Écarts avec la spécification** : aucun.
