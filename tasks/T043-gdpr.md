# T043 — RGPD : export, purge, audit

- **Phase** : 4
- **Dépend de** : T017
- **Taille estimée** : M
- **Exécution** : sonnet (`task-implementer`) — revue Opus : renforcée

## Objectif
Fournir au praticien (responsable de traitement) les outils pour respecter le RGPD sans effort.

## Contexte à lire
`PLAN.md` §4, tables `patient`, `appointment`, `audit_log`, `outbox_message`, `session`.

## Spécification
- Fiche patient : **Exporter** (JSON + CSV des coordonnées et RDV) et **Supprimer** (anonymisation : nom → « Patient supprimé », coordonnées nulles, RDV conservés anonymes pour les statistiques ; ou suppression dure si aucun RDV).
- Job quotidien de purge (dans le worker) avec durées configurables :
  - `outbox_message` envoyés > 90 j : suppression du contenu (`payload`, `recipient`) ;
  - `otp_challenge`, sessions expirées : suppression ;
  - patients sans RDV depuis N ans (défaut 3) : anonymisation, avec liste de prévisualisation dans l'admin avant activation ;
  - `audit_log` > 1 an : suppression.
- Audit : consultations de fiche patient (`patient_view`), exports, suppressions, connexions → `audit_log`. Écran `/admin/audit` (admin).
- Page publique `/p/{slug}/privacy` : modèle de mentions d'information patient (à compléter par le praticien), générée depuis les réglages.
- `docs/rgpd.md` : registre de traitement prérempli, point HDS (hébergement par un tiers = HDS requis ; auto-hébergement = responsabilité du praticien), recommandations (chiffrement disque, sauvegardes chiffrées, mises à jour).

## Critères d'acceptation
- [x] Après suppression, aucune donnée identifiante du patient en base (test qui grep la base dumpée).
- [x] Purge testée avec une horloge fixe.

## Vérification
```bash
make check
```

## Hors scope
Conseil juridique : la doc le dit explicitement.

## Suivi

> Rempli par l'agent pendant et à la fin de chaque session sur cette tâche. Le statut officiel est dans `tasks/README.md`.

- **Sessions** : 2026-10-16 (sonnet).
- **Fait** : migration 00016 (réglages de conservation par cabinet, `patient.anonymized_at` + CHECK contact assoupli, `outbox_message.scrubbed_at` + index, `audit_log.practice_id` + index) ; `MarkOutboxSent` retire tout de suite `manage_url`/`reset_url`/`code`/`body` du payload ; service `Privacy` (réglages, prévisualisation, purge quotidienne `RunOnce`/`Run` branchée dans `startWorker`, page d'information publique) ; `Patients.Erase` (suppression dure sans RDV, sinon anonymisation, cascade sur les doublons fusionnés, messages effacés, refus s'il reste des RDV à venir) ; `Patients.Export` (JSON + CSV anti-formules) ; `patient_view` à chaque consultation ; service `Audit` + `/admin/audit` (admin) ; `/admin/privacy` (admin) ; `/p/{slug}/privacy` ; `docs/rgpd.md`. Tests : `internal/service/privacy_test.go`, `internal/web/privacy_test.go` (dump de toutes les tables sans marqueur après suppression ; purge avec horloge fixe).
- **Reste à faire** : rien d'obligatoire. Rendu visuel des pages non vérifié ; EXPLAIN sur gros volume non fait (index prévus : `outbox_message_scrub_idx`, `audit_log_practice_id_idx`, `audit_log_practice_at_idx` ; `ListInactivePatients` parcourt les patients du cabinet avec `appointment_patient_idx`).
- **Notes de reprise** : la purge des patients est désactivée par défaut (`practice.patient_purge_enabled`). `ScrubOutboxForPatients` retrouve aussi les messages sans RDV (codes) par destinataire. `TestMigrateUpDownUp` : boucle de descente portée de 10 à 40 (16 migrations).
- **Écarts avec la spécification** : durées stockées dans `practice` (réglables dans `/admin/privacy`) et non en variables d'environnement ; prévisualisation + activation sur `/admin/privacy` ; l'IP des connexions du personnel reste dans `audit_log.meta` (comportement existant, documenté dans `docs/rgpd.md`) ; export/suppression réservés aux rôles admin et praticien ; l'export contient les messages (type, statut, destinataire) mais pas leur texte.
