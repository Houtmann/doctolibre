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
- [ ] Après suppression, aucune donnée identifiante du patient en base (test qui grep la base dumpée).
- [ ] Purge testée avec une horloge fixe.

## Vérification
```bash
make check
```

## Hors scope
Conseil juridique : la doc le dit explicitement.

## Suivi

> Rempli par l'agent pendant et à la fin de chaque session sur cette tâche. Le statut officiel est dans `tasks/README.md`.

- **Sessions** : —
- **Fait** : —
- **Reste à faire** : —
- **Notes de reprise** (pièges, état intermédiaire, commandes utiles) : —
- **Écarts avec la spécification** (et pourquoi) : —
