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
- **Taille des requêtes (précisé par l'orchestrateur)** : le middleware `httpx.CSRF` lit `csrf_token` via `PostFormValue` derrière un `MaxBytesReader` de 1 Mio (`maxCSRFFormLen`), ce qui casserait un upload de 2 Mo. Ajouter dans `httpx` une limite distincte pour `multipart/form-data` (constante ~4 Mio : fichier 2 Mo + renvoi en base64 à l'étape 2 + marge), sans changer la limite des formulaires urlencoded. Les deux formulaires (aperçu et confirmation) sont donc en `enctype="multipart/form-data"` ; à l'étape 2 le contenu est renvoyé en base64 dans un champ caché. Le handler applique en plus sa propre borne de 2 Mo sur le fichier décodé. Test : un upload multipart de ~1,5 Mo passe le CSRF, une requête urlencoded > 1 Mio est toujours refusée.
- Pas de SQL concaténé (sqlc) ; si une insertion en masse est nécessaire, `CopyFrom` pgx ou requête par lot dans la transaction.

## Critères d'acceptation
- [x] Un fichier `;` en cp1252 avec accents est importé correctement.
- [x] Un fichier avec BOM UTF-8 est importé correctement.
- [x] Les doublons (base et intra-fichier) et les lignes invalides sont signalés à l'aperçu et non importés.
- [x] Sans la case de consentement, les patients importés ont les deux opt-in à false.
- [x] Fichier > 2 Mo ou > 5 000 lignes refusé.
- [x] Tests : parseur (séparateurs, encodages, en-têtes avec accents), service (aperçu, import, atomicité), web (fragments clés, accès secrétariat).

## Vérification
```bash
make check
```

## Hors scope
Import des RDV, mise à jour des fiches existantes, formats spécifiques d'éditeurs, dépendance `golang.org/x/text`.

## Suivi

> Rempli par l'agent pendant et à la fin de chaque session sur cette tâche. Le statut officiel est dans `tasks/README.md`.

- **Sessions** : 2026-10-16 (sonnet).
- **Fait** : middleware CSRF (`httpx.MaxMultipartLen` = 4 Mio pour multipart, urlencoded inchangé à 1 Mio) ; parseur pur `service.ParseImportCSV` (`patient_import_csv.go` : BOM, UTF-8 sinon cp1252 par table maison, séparateur détecté, en-têtes sans accents/casse, portable prioritaire sur le fixe, dates JJ/MM/AAAA ou AAAA-MM-JJ) ; service `PatientImport` (`Preview`, `Import`) avec détection de doublons en lot (`FindImportDuplicates`) puis intra-fichier, insertion en une requête (`CreatePatientsBulk`, JSON) dans une seule `store.WithTx` qui ré-analyse les doublons, audit `patient.imported` (compteurs + importateur) ; handler/route `GET/POST /admin/patients/import` (tous rôles), page `admin_patient_import`, lien dans `/admin/patients` ; `make css` fait ; câblage `Deps.PatientImport` + `serve.go`. Tests : parseur, service (aperçu, opt-in, atomicité, re-vérification à la confirmation, audit sans donnée), httpx (limites), web (flux 3 rôles, limites, accès).
- **Reste à faire** : rien de fonctionnel ; passage navigateur de la page non fait.
- **Notes de reprise** : aucune migration. EXPLAIN de `FindImportDuplicates` (200 000 patients, 5 cabinets, 5 000 téléphones + 5 000 emails dans le lot) : BitmapOr sur `patient_practice_phone_idx` et `patient_practice_email_idx`, ~65 ms.
- **Écarts avec la spécification** : `sqlc` ne gère pas `unnest` multi-tableaux : insertion en lot via `jsonb_array_elements` (un seul INSERT … SELECT). Pas de verrou consultatif : deux imports simultanés du même fichier pourraient créer des doublons (re-vérification dans la transaction seulement). Un fichier de plus de 4 Mio est refusé par le CSRF (403 « jeton CSRF invalide ») avant que le handler puisse afficher « dépasse 2 Mo » (affiché entre 2 et ~4 Mio).
