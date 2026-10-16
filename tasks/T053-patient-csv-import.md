# T053 — Import CSV des patients

- **Phase** : 5
- **Dépend de** : T017
- **Taille estimée** : M
- **Exécution** : sonnet (`task-implementer`) — revue Opus : renforcée (données)

## Objectif
Un cabinet qui change de logiciel importe sa liste de patients depuis un CSV, avec aperçu, détection des doublons et sans stockage du fichier sur le serveur.

## Contexte à lire
`internal/service/patients.go` (création, `FindPatientDuplicates`, `Merge`), `internal/service/patient.go`, `internal/domain/phone.go` (`NormalizePhone`), `internal/web/patients.go`, `internal/web/templates/pages/admin_patients.html` et `admin_patient_new.html`, `internal/store/queries/` (requêtes patient), `migrations/00002_core.sql` (table `patient`, l. ~110), `internal/service/audit.go`, `internal/auth/policy.go` (`ActionPatientsManage` : tout rôle valide, secrétariat compris).

## Spécification
- Route `GET/POST /admin/patients/import` pour **admin, praticien et secrétariat** (`ActionPatientsManage` les autorise tous). Lien depuis `/admin/patients`. Service `service.PatientImport` (fichier `internal/service/patient_import.go`) ; parseur CSV pur dans un fichier à part, testable sans DB.
- Upload ≤ 2 Mo, ≤ 5 000 lignes (refus clair au-delà). Séparateur `;` ou `,` détecté sur la ligne d'en-tête. Encodage : UTF-8 avec ou sans BOM, sinon Windows-1252 décodé par une **table maison** (pas de `golang.org/x/text`).
- En-tête obligatoire. Colonnes reconnues par noms usuels, insensibles à la casse et aux accents : nom, prénom, téléphone/portable/mobile, email/mail/courriel, date de naissance (`JJ/MM/AAAA` ou `AAAA-MM-JJ`), adresse. Aucun format d'éditeur spécifique garanti (Doctolib, Maiia…). Colonnes inconnues ignorées ; colonnes nom/prénom manquantes → refus du fichier.
- **Aperçu** (étape 1) : lignes valides, lignes invalides avec la raison (nom/prénom vides, ni téléphone ni email — contrainte de la table `patient` —, date illisible), doublons : téléphone normalisé par `domain.NormalizePhone`, email en minuscules, contre la base existante **et** dans le fichier.
- **Confirmation** (étape 2) : le fichier est renvoyé (champ caché ou nouvel upload ; **rien n'est stocké côté serveur**), ré-analysé, puis insertion transactionnelle des seules lignes valides non doublons.
- Consentements de notification `sms_opt_in` / `email_opt_in` à **false** par défaut pour les patients importés, avec une case explicite « Ces patients ont accepté de recevoir des rappels » et un texte d'avertissement (responsabilité du cabinet).
- `audit_log` : compte importateur, nombre de lignes importées/ignorées. Jamais de donnée patient dans les logs ni dans l'audit.
- Pas de SQL concaténé (sqlc) ; si une insertion en masse est nécessaire, `CopyFrom` pgx ou requête par lot dans la transaction.

## Critères d'acceptation
- [ ] Un fichier `;` en cp1252 avec accents est importé correctement.
- [ ] Un fichier avec BOM UTF-8 est importé correctement.
- [ ] Les doublons (base et intra-fichier) et les lignes invalides sont signalés à l'aperçu et non importés.
- [ ] Sans la case de consentement, les patients importés ont les deux opt-in à false.
- [ ] Fichier > 2 Mo ou > 5 000 lignes refusé.
- [ ] Tests : parseur (séparateurs, encodages, en-têtes avec accents), service (aperçu, import, atomicité), web (fragments clés, accès secrétariat).

## Vérification
```bash
make check
```

## Hors scope
Import des RDV, mise à jour des fiches existantes, formats spécifiques d'éditeurs, dépendance `golang.org/x/text`.

## Suivi

> Rempli par l'agent pendant et à la fin de chaque session sur cette tâche. Le statut officiel est dans `tasks/README.md`.

- **Sessions** : —
- **Fait** : —
- **Reste à faire** : —
- **Notes de reprise** (pièges, état intermédiaire, commandes utiles) : —
- **Écarts avec la spécification** (et pourquoi) : —
