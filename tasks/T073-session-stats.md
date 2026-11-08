# T073 — Statistiques de séances et export pour rétrocession

> UI à faire en daisyUI ; à lancer après T080 (décision du 2026-10-16).

- **Phase** : 5
- **Dépend de** : T072, T059
- **Taille estimée** : M
- **Exécution** : sonnet (`task-implementer`) — revue Opus : renforcée (requête sur `appointment`, export)

## Objectif
L'admin (et chaque praticien pour lui-même) voit, sur une période, le nombre de séances faites, absences et annulations par praticien, par motif et cabinet/domicile, y compris celles réalisées par un remplaçant, et exporte ce tableau en CSV pour calculer les rétrocessions.

## Contexte à lire
T072 (`status_changed_by`, `status_changed_at`), T059 (`practitioner_delegation`), `migrations/` (index existants sur `appointment`), `internal/store/queries/appointment*.sql`, `internal/service/calendar.go` (`Actor`, règles d'accès), `internal/service/audit.go`, `internal/web/privacy.go` (export existant : en-têtes, nom de fichier, audit), `admin_dashboard.html`, `docs/design-system.md` (tableaux daisyUI `table`).

## Spécification
- Route `GET /admin/stats?from=AAAA-MM-JJ&to=AAAA-MM-JJ&practitioner=…` (défaut : mois civil précédent, bornes inclusives en jours **dans le fuseau du cabinet**, période max 366 jours). Admin : tous les praticiens du cabinet ; praticien : lui-même ; secrétariat : 403 (choix par défaut, question laissée à l'humain dans STATUS.md). Remplaçant : seulement ses propres séances, pas les chiffres du titulaire délégué.
- Une requête sqlc agrégée (pas de boucle Go sur les RDV) : `GROUP BY practitioner_id, appointment_type_id, at_home, status, réalisé_par` sur les RDV dont le début tombe dans la période, filtrée par `practice_id`. « Réalisé par » = `status_changed_by` pour les `done` ; si cet utilisateur est lié à une **autre fiche praticien** que le titulaire, la ligne est attribuée à ce remplaçant (nom de sa fiche praticien) ; un pointage par un compte sans fiche praticien (secrétariat, admin non praticien — « Ma journée » est ouverte aux trois rôles, T072) reste attribué au titulaire. Un RDV repassé à `booked` n'est pas compté comme fait (T072 réécrit `status_changed_by` à chaque changement). RDV `done` antérieurs à T072 (`status_changed_by` NULL) : attribués au titulaire.
- Tableau : par praticien titulaire → lignes par réalisateur (titulaire / remplaçant) × motif, colonnes faites, absents, annulées, dont domicile ; totaux. Pas de montant (les tarifs ne sont pas dans le modèle).
- Export `GET /admin/stats.csv` (mêmes paramètres) : séparateur `;`, UTF-8 avec BOM (Excel FR), une ligne par agrégat, **aucune donnée patient** (ni nom, ni id). Protection contre l'injection de formules (cellule commençant par `=`, `+`, `-`, `@`, tabulation → préfixe `'`). Export journalisé dans `audit_log` (période, praticien).
- Index : `EXPLAIN` de la requête sur un jeu de données de test réaliste (≥ 10 000 RDV) ; ajouter un index seulement si le plan l'exige (migration **00027**), et coller le plan dans Suivi.
- Lien vers la page depuis le tableau de bord admin.

## Critères d'acceptation
- [x] Comptes justes sur un jeu de fixtures (faites, absents, annulées, domicile) ; RDV hors période ou d'un autre cabinet exclus.
- [x] Bornes de période en `Europe/Paris` : RDV à 23:30 locale le dernier jour inclus, 00:30 le lendemain exclu ; mois d'octobre 2026 (changement d'heure) correct.
- [x] Séance pointée par un remplaçant attribuée au remplaçant ; séance ancienne sans `status_changed_by` attribuée au titulaire.
- [x] CSV : en-têtes, BOM, `;`, cellule `=SUM(…)` neutralisée (test), aucun champ patient.
- [x] Praticien ne voit que ses chiffres ; autre cabinet refusé ; export audité.
- [x] `EXPLAIN` noté dans Suivi.

## Vérification
```bash
make sqlc
make check
```

## Hors scope
Montants, tarifs, calcul du pourcentage de rétrocession, facturation, graphiques, statistiques patients.

## Suivi

> Rempli par l'agent pendant et à la fin de chaque session sur cette tâche. Le statut officiel est dans `tasks/README.md`.

- **Sessions** : 2026-11-08 (sonnet)
- **Fait** : requête sqlc unique `StatsSessions` (`internal/store/queries/stats.sql`, CTE `base` + agrégat, filtrée par `practice_id`, bornes `[début, fin)` calculées en Go avec `time.ParseInLocation` puis `AddDate(0,0,1)`) ; service `internal/service/stats.go` (`Stats.Report`, `Stats.ExportCSV`, action d'audit `stats.exported`, meta = période + id praticien, aucun nom) ; handlers `internal/web/stats.go` (`GET /admin/stats`, `GET /admin/stats.csv`, RequireUser admin + praticien, secrétariat 403) ; page `admin_stats.html` (daisyUI, formulaire GET sans JS, un tableau par titulaire, totaux) ; lien dans `admin_dashboard.html` (admin et praticien) ; `Deps.Stats` + câblage `serve.go` ; `ui.css` régénéré ; `render_test.go` : donnée vide pour `admin_stats`. Tests : `internal/service/stats_test.go` (comptes/attribution, accès, bornes + DST automne/printemps, validation de période, CSV + audit, neutralisation des formules), `internal/web/stats_test.go` (accès par rôle, page, période invalide, téléchargement CSV).
- **Reste à faire** : passage navigateur (page non vue à l'écran, ni à 360 px, ni en sombre). Question de produit laissée à l'humain : secrétariat en 403 (choix par défaut de la spec).
- **Notes de reprise** (pièges, état intermédiaire, commandes utiles) : sur cette machine `make` et `cc` sont des shims Xcode absents : lancer les commandes avec `CGO_ENABLED=0` (`CGO_ENABLED=0 go tool sqlc generate`, `go test`, `golangci-lint run ./...`) ; `bin/tailwindcss` absent du worktree, utiliser celui du checkout principal. Aucune migration : le plan utilise `appointment_practice_start_idx (practice_id, lower(during), id)` ; `app_user` et `practitioner` sont lus par clé primaire / index `practice_id`.
  EXPLAIN (ANALYZE, BUFFERS) sur 15 000 RDV (3 praticiens x 5 000, dont 5 000 d'un autre cabinet), période d'un mois (1 486 lignes dans l'index, 1 238 retenues) : exécution 2,8 ms, planification 0,7 ms. Plan :
  ```
  GroupAggregate (actual time=2.543..2.782 rows=6)
    -> Sort (rows=1238, quicksort 178kB)
      -> Merge Join (pp.id = performed_by) (rows=1238)
        -> Sort -> Nested Loop Left Join (app_user via app_user_pkey, rows=1238)
             -> Nested Loop (appointment_type via appointment_type_pkey + Memoize, hits 1237)
                -> Hash Join (practitioner pt)
                   -> Bitmap Heap Scan on appointment a (rows=1238, Filter status, 248 removed, 46 blocs)
                        -> Bitmap Index Scan on appointment_practice_start_idx
                           Index Cond: practice_id = $1 AND lower(during) >= ... AND lower(during) < ...
        -> Sort -> Bitmap Heap Scan on practitioner pp (practitioner_practice_id_slug_key)
  Execution Time: 2.826 ms
  ```
  Pas d'index ajouté (00027 reste libre). Réserve : une période de 366 jours sur un gros cabinet lit l'index sur toute la plage, mais reste bornée par le nombre de RDV du cabinet sur un an.
- **Écarts avec la spécification** (et pourquoi) :
  - « Praticien : lui-même uniquement » : un praticien voit les RDV dont il est titulaire (y compris ceux pointés par un remplaçant, lignes « remplaçant ») ET les séances qu'il a lui-même réalisées pour un confrère (il y est le remplaçant). Un remplaçant délégué ne voit jamais les autres lignes du titulaire (testé, délégation incluse).
  - Colonne « Dont à domicile » = séances **faites** à domicile (motif `at_home`), incluse dans « Faites ». Le lieu est une propriété du motif (`appointment_type.at_home`), pas du RDV.
  - Période invalide : page 422 avec le formulaire (HTML) ; l'export CSV répond 400 (page d'erreur HTML, aucun en-tête de téléchargement). Une seule borne fournie est refusée (les deux ou aucune).
  - `statsCSVSafe` n'est pas `csvSafe` (fiches patients) : celui-ci laisse passer `+33…` (numéros), la spec T073 demande de préfixer tout `+`. L'export est construit en mémoire (taille bornée par le nombre d'agrégats) pour ne poser les en-têtes qu'après le contrôle d'accès et l'audit.
  - Le lien du tableau de bord s'affiche aussi si `Deps.Stats` est nil (cas des tests uniquement).
