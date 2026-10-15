# SMS : fournisseurs et coûts

> Relevé du 2026-10-15, prix vers la France. Les tarifs bougent : vérifier sur le site du fournisseur avant de chiffrer.
> Les sections « Configuration » seront complétées par la tâche [T022](../tasks/T022-sms-providers.md).

## Prix unitaire

| Fournisseur | Prix / SMS (France) | Modèle | Driver Doctolibre |
|---|---|---|---|
| **OVH SMS Pro** | 0,060 € HT (pack 100) → 0,058 € (1 000) → 0,054 € (10 000) | Packs prépayés, sans abonnement. Validité des crédits annoncée à 24 mois par un concurrent (à vérifier dans les CGV OVH). | `ovh` |
| **Twilio** | 0,0798 $ ≈ 0,07 € | À l'usage, sans abonnement, facturé en USD | `http` (webhook générique) |
| Plivo (référence) | 0,0664 $ ≈ 0,06 € | À l'usage, facturé en USD | `http` |
| **Passerelle Android** | ≈ 0 € (forfait SMS illimité ≈ 2–5 €/mois) | Un téléphone + une SIM dédiés, app open source « SMS Gateway for Android » | `android` |

Note : un comparatif tiers annonce OVH à 0,045 €/SMS, ce qui ne correspond pas à la grille officielle — non retenu.

## Coût mensuel estimé — kiné solo (100 RDV/semaine ≈ 430 RDV/mois)

| Stratégie | SMS / mois | OVH | Twilio |
|---|---|---|---|
| Tout par SMS (confirmation + rappel + OTP) | ~1 000 | ~58 € HT | ~70 € |
| **Rappel J-1 seul par SMS**, confirmation et OTP par email quand possible | ~430 | **~25 € HT** | ~30 € |
| Idem, sans rappel pour les séances d'une série déjà en cours | ~150–200 | **~10 € HT** | ~13 € |
| Passerelle Android | illimité | **~2–5 € tout compris** | — |

Multiplier à peu près par le nombre de praticiens pour un cabinet de groupe.

**Conclusion : la stratégie d'envoi pèse bien plus que le choix du fournisseur.**

## Réglages par défaut retenus

- Confirmation : **email** (SMS seulement si le patient n'a pas d'email).
- Rappel J-1 : **SMS** (c'est lui qui réduit les absences).
- Rappel H-2 : désactivé par défaut.
- Séances d'une série en cours (T030) : rappel SMS **désactivable** (réglage cabinet).
- OTP : email si une adresse est fournie, sinon SMS.

## Pièges de facturation

- **Segments** : 160 caractères en GSM-7, mais **70 seulement en UCS-2**. Un seul caractère hors GSM-7 suffit à basculer tout le message en UCS-2.
  - Dans GSM-7 : `é è à ù ì ò Ç`.
  - **Hors GSM-7** : `ê â î ô û ë ï ç œ` et les emojis.
  - Doctolibre translittère ces caractères par défaut (`ê`→`e`, `œ`→`oe`…) pour rester à 1 segment (T022).
- **Lien de gestion** : utiliser une URL courte (`/r/{token}` sur le domaine du cabinet), pas de raccourcisseur tiers.
- **Nom d'expéditeur** (alphanumérique, ex. `KINE-DUPONT`) : géré par OVH ; chez Twilio, vérifier les conditions d'enregistrement pour la France.
- **Plage horaire** : pas d'envoi entre 20 h et 8 h (hors OTP), géré par l'outbox.

## Recommandation

| Profil | Choix |
|---|---|
| Kiné solo, budget minimal, à l'aise techniquement | **Passerelle Android**. Limites : téléphone allumé et connecté en permanence ; les forfaits grand public interdisent souvent l'usage automatisé (vérifier les CGV de l'opérateur). |
| Cas général, cabinet de groupe | **OVH** : le moins cher des fournisseurs « pros », facturé en euros, hébergé en France. |
| Déjà client Twilio / autre | Driver `http` générique, sans code spécifique. |

## Sources

- [OVHcloud SMS Pro (officiel)](https://www.ovhcloud.com/fr/sms/)
- [Twilio — SMS pricing France (officiel)](https://www.twilio.com/en-us/sms/pricing/fr)
- [Plivo — SMS pricing France](https://www.plivo.com/sms/pricing/fr/)
- [Conexteo — comparatif plateformes SMS 2026](https://conexteo.com/comparatif-plateforme-sms-pro/)
- [SMS Proxima — OVH vs SMS Proxima](https://sms-proxima.com/sms-ovh-vs-sms-proxima)
- [textbee — Twilio France](https://textbee.dev/sms-pricing/twilio/fr)
