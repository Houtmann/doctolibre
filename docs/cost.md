# Coût mensuel estimé

> Relevé du 2026-10-16, ordres de grandeur en euros HT, France. Les tarifs bougent : vérifiez chez le fournisseur avant de décider. Les prix SMS ne sont **pas** redétaillés ici : voir [`sms.md`](sms.md) (stratégie d'envoi et grille par fournisseur).

## Postes de dépense

| Poste | Montant indicatif | Remarque |
|---|---|---|
| Hébergement VPS (Hetzner, OVH, Scaleway) | 4 à 6 €/mois | 1 vCPU, 1 à 2 Go de RAM suffisent |
| Nom de domaine | 8 à 15 €/an (≈ 1 €/mois) | Renouvellement annuel |
| Email d'envoi | 0 à quelques €/mois | Offre gratuite d'un fournisseur SMTP ou boîte email du domaine, selon le volume |
| Sauvegardes hors serveur (S3-compatible) | < 1 €/mois | Quelques centimes de stockage pour une base de cette taille |
| SMS | selon profil | [`sms.md`](sms.md) |
| Temps d'administration | 1 h/trimestre | Mises à jour + test de restauration |

## Trois profils

| Profil | Hébergement | Domaine | Sauvegardes | SMS (rappel J-1 seul) | **Total mensuel** |
|---|---|---|---|---|---|
| **Solo, 100 RDV/semaine** (≈ 430 RDV/mois), VPS | ≈ 5 € | ≈ 1 € | ≈ 1 € | ≈ 25 € HT (OVH), ou 2 à 5 € en passerelle Android | **≈ 32 €** avec OVH, **≈ 10 à 12 €** avec passerelle Android |
| **Cabinet de 3 kinés**, VPS | ≈ 6 € | ≈ 1 € | ≈ 1 € | ≈ 75 € HT (OVH, 3 × le profil solo) | **≈ 83 €** (le SMS fait presque toute la facture) |
| **Raspberry Pi au cabinet** (solo), SMS par passerelle Android | 0 € (≈ 80 € de matériel une fois ; ≈ 1 €/mois d'électricité) | ≈ 1 € | ≈ 1 € | 2 à 5 € | **≈ 5 à 8 €** + matériel amorti |

Coût annuel : multiplier par 12 (le domaine est facturé à l'année). Par exemple, le profil solo avec OVH revient à environ 380 €/an, dont 300 € de SMS.

## Pour baisser la facture

- Confirmation et code de vérification par **email**, SMS réservé au rappel de la veille.
- Désactiver le rappel pour les séances d'une série déjà en cours (réglage dans `/admin/settings/notifications`) : voir `sms.md`.
- Passerelle Android : très économique, mais un téléphone dédié doit rester allumé, et les conditions de l'opérateur sont à vérifier.

Procédure d'installation : [`install.md`](install.md).
