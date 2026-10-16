# T073 — Statistiques de séances et export pour rétrocession

> UI à faire en daisyUI ; à lancer après T080 (décision du 2026-10-16).

- **Phase** : 5
- **Dépend de** : T072, T059
- **Taille estimée** : M
- **Exécution** : sonnet (`task-implementer`) — revue Opus : renforcée (requête sur `appointment`, export)

## Objectif
L'admin (et chaque praticien pour lui-même) voit, sur une période, le nombre de séances faites, absences et annulations par praticien, par motif et cabinet/domicile, y compris celles réalisées par un remplaçant, et exporte ce tableau en CSV pour calculer les rétrocessions.

## Contexte à lire
T072 (`status_changed_by`, `status_changed_at`), T059 (`practitioner_delegation`), `migrations/` (index existants sur `appointment`), `internal/store/queries/appointment*.sql`, `internal/service/calendar.go` (`Actor`, règles d'accès), `internal/service/audit.go`, `internal/web/privacy.go` (export existant : en-têtes, nom de fichier, audit), `admin_dashboard.html`, `docs/design-system.md` (tableaux v2).

## Spécification
- Route `GET /admin/stats?from=AAAA-MM-JJ&to=AAAA-MM-JJ&practitioner=…` (défaut : mois civil précédent, bornes inclusives en jours **dans le fuseau du cabinet**, période max 366 jours). Admin : tous les praticiens du cabinet ; praticien : lui-même ; secrétariat : 403 (à confirmer avec l'humain, noter dans Suivi).
- Une requête sqlc agrégée (pas de boucle Go sur les RDV) : `GROUP BY practitioner_id, appointment_type_id, at_home, status, réalisé_par` sur les RDV dont le début tombe dans la période, filtrée par `practice_id`. « Réalisé par » = `status_changed_by` pour les `done` ; si cet utilisateur n'est pas le compte du praticien titulaire, la ligne est attribuée au remplaçant (nom de l'utilisateur). RDV `done` antérieurs à T072 (`status_changed_by` NULL) : attribués au titulaire.
- Tableau : par praticien titulaire → lignes par réalisateur (titulaire / remplaçant) × motif, colonnes faites, absents, annulées, dont domicile ; totaux. Pas de montant (les tarifs ne sont pas dans le modèle).
- Export `GET /admin/stats.csv` (mêmes paramètres) : séparateur `;`, UTF-8 avec BOM (Excel FR), une ligne par agrégat, **aucune donnée patient** (ni nom, ni id). Protection contre l'injection de formules (cellule commençant par `=`, `+`, `-`, `@`, tabulation → préfixe `'`). Export journalisé dans `audit_log` (période, praticien).
- Index : `EXPLAIN` de la requête sur un jeu de données de test réaliste (≥ 10 000 RDV) ; ajouter un index seulement si le plan l'exige, et coller le plan dans Suivi.
- Lien vers la page depuis le tableau de bord admin.

## Critères d'acceptation
- [ ] Comptes justes sur un jeu de fixtures (faites, absents, annulées, domicile) ; RDV hors période ou d'un autre cabinet exclus.
- [ ] Bornes de période en `Europe/Paris` : RDV à 23:30 locale le dernier jour inclus, 00:30 le lendemain exclu ; mois d'octobre 2026 (changement d'heure) correct.
- [ ] Séance pointée par un remplaçant attribuée au remplaçant ; séance ancienne sans `status_changed_by` attribuée au titulaire.
- [ ] CSV : en-têtes, BOM, `;`, cellule `=SUM(…)` neutralisée (test), aucun champ patient.
- [ ] Praticien ne voit que ses chiffres ; autre cabinet refusé ; export audité.
- [ ] `EXPLAIN` noté dans Suivi.

## Vérification
```bash
make sqlc
make check
```

## Hors scope
Montants, tarifs, calcul du pourcentage de rétrocession, facturation, graphiques, statistiques patients.

## Suivi

> Rempli par l'agent pendant et à la fin de chaque session sur cette tâche. Le statut officiel est dans `tasks/README.md`.

- **Sessions** : —
- **Fait** : —
- **Reste à faire** : —
- **Notes de reprise** (pièges, état intermédiaire, commandes utiles) : —
- **Écarts avec la spécification** (et pourquoi) : —
