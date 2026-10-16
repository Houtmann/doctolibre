# RGPD et données de santé

> **Ce document n'est pas un conseil juridique.** Il décrit ce que fait le logiciel et propose un registre à relire, adapter et valider par le praticien (ou son DPO / conseil). Le praticien reste **responsable de traitement**.

## Ce que Doctolibre stocke

| Donnée | Où | Sensibilité |
|---|---|---|
| Nom, prénom, téléphone, email, date de naissance, adresse (visite à domicile), note administrative | `patient` | Donnée personnelle ; un RDV chez un kiné est une donnée de santé **indirecte** |
| Dates, motifs (« séance », « bilan »), statut, motif d'annulation, adresse du RDV | `appointment`, `care_plan` | Idem |
| Contenu des emails et SMS (prénom, date, lien de gestion) | `outbox_message` | Effacé après la durée de conservation (voir ci-dessous) |
| Inscriptions en liste d'attente (fenêtre de dates, jours, plage horaire souhaités, statut) et propositions de créneaux | `waitlist_entry`, `waitlist_offer` | Donnée personnelle ; supprimées avec la fiche patient (suppression dure comme anonymisation), reprises dans l'export, réaffectées lors d'une fusion de fiches ; le lien de proposition est retiré du message dès son envoi |
| Codes de vérification, jetons | `otp_challenge`, `session`, `password_reset` | Seuls des hachés sont stockés ; supprimés une fois expirés |
| Journal d'accès | `audit_log` | Identifiants uniquement (jamais de nom, téléphone, email). L'adresse IP des connexions du personnel y figure |

**Aucune donnée médicale** (pathologie, ordonnance, compte rendu) n'est collectée : le champ « note » est une note administrative, avec avertissement dans l'interface.

## Registre de traitement (modèle)

| Traitement | Finalité | Base légale | Personnes | Données | Destinataires | Durée |
|---|---|---|---|---|---|---|
| Prise de rendez-vous | Planifier, modifier, annuler les soins | Exécution de la demande du patient (art. 6.1.b) | Patients | Identité, contact, RDV | Cabinet ; hébergeur | 3 ans sans RDV, puis anonymisation (réglable) |
| Notifications | Confirmations et rappels par email/SMS | Idem (rappel transactionnel) ; opt-out respecté | Patients | Prénom, date, contact | Fournisseur SMTP / SMS | Contenu effacé après 90 jours (réglable) |
| Vérification à la réservation | Éviter les réservations frauduleuses | Intérêt légitime (art. 6.1.f) | Patients | Haché du contact | — | Supprimé à l'expiration |
| Comptes du personnel et journal d'accès | Sécurité, traçabilité | Intérêt légitime | Personnel | Email, rôle, IP, actions | Cabinet | Journal : 1 an (réglable) |

À compléter : coordonnées du responsable, du DPO s'il y en a un, sous-traitants réels (hébergeur, fournisseur d'emails/SMS) avec leur contrat de sous-traitance (art. 28).

## Ce que fait le logiciel

| Droit / obligation | Fonction | Où |
|---|---|---|
| Information | Page publique `/p/{slug}/privacy`, générée depuis les réglages | `/admin/privacy` pour le contact |
| Accès / portabilité | Export JSON ou CSV d'une fiche : coordonnées, RDV, séries, messages (sans leur texte) | Fiche patient → « Données personnelles » (administrateur, praticien) |
| Effacement | « Supprimer la fiche » : suppression dure s'il n'y a jamais eu de RDV, sinon **anonymisation** (nom « Patient supprimé », coordonnées, note, adresses et motifs d'annulation effacés, lien de gestion coupé, messages effacés). Les RDV restent, anonymes. Les doublons fusionnés dans la fiche sont traités avec elle. Refusé tant qu'il reste des RDV à venir | Fiche patient |
| Minimisation | Purge quotidienne (worker, démarrée au lancement puis toutes les 24 h) | ci-dessous |
| Traçabilité | Consultations de fiches (`patient_view`), exports, suppressions, connexions, modifications de réglages | `/admin/audit` (administrateur) |

### Purge quotidienne

| Donnée | Règle (défaut) |
|---|---|
| Lien de gestion, lien de réinitialisation, code, texte du SMS d'un message **envoyé** | Retirés **immédiatement** après l'envoi |
| Contenu et destinataire des messages envoyés, échoués ou annulés | Effacés après 90 jours (la trace « envoyé le… » reste) |
| Sessions, jetons de réinitialisation, codes de vérification expirés | Supprimés |
| Journal d'audit | Supprimé après 365 jours (lignes sans cabinet : 365 jours) |
| Patients sans activité depuis 3 ans (création, modification, dernier RDV, RDV à venir compris) | **Anonymisés**, seulement si le praticien a activé l'option. `/admin/privacy` affiche d'abord la liste des patients concernés |

Les durées se règlent dans `/admin/privacy` (administrateur).

## Données de santé et hébergement (HDS)

- Si **un tiers héberge** l'application pour le compte du praticien, ce tiers doit être **certifié HDS** (hébergeur de données de santé).
- Si le praticien **s'auto-héberge** (VPS, mini-PC au cabinet), il reste responsable de la sécurité de ses données ; la certification HDS ne s'applique pas à lui, mais les obligations de sécurité demeurent.
- Dans le doute (hébergement mutualisé, prestataire qui administre le serveur), demander conseil.

## Recommandations

- **Chiffrer le disque** du serveur (LUKS ou équivalent) ; la machine doit être sous clé ou dans un local fermé.
- **Sauvegardes chiffrées** (voir [`backup.md`](backup.md)), clé privée hors du serveur ; tester la restauration. Une sauvegarde contient les données avant purge : limiter sa durée de conservation (rétention 7 jours + 4 semaines par défaut).
- **Mises à jour** régulières de l'application, de l'OS et de PostgreSQL ; HTTPS obligatoire (Caddy).
- Mots de passe forts et comptes nominatifs ; supprimer les comptes des personnes parties.
- Ne pas saisir d'information médicale dans la note administrative.
- Violation de données : notifier la CNIL sous 72 h si elle présente un risque (cnil.fr).
- Les journaux applicatifs (stdout) ne contiennent que des identifiants ; limiter leur durée de conservation côté hébergeur.

## Limites connues

- Les messages **en échec ou annulés** gardent leur contenu (renvoi manuel possible) jusqu'à la durée de conservation.
- Les sauvegardes déjà faites ne sont pas modifiées par une anonymisation : elles disparaissent avec leur rotation.
- Le journal d'accès conserve l'adresse IP des connexions du personnel pour la sécurité.
