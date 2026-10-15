# T044 — Documentation d'installation

- **Phase** : 4
- **Dépend de** : T040, T041
- **Taille estimée** : S
- **Exécution** : sonnet (`task-implementer`) — revue Opus : standard

## Objectif
Une personne non-développeuse mais à l'aise avec un tutoriel installe une instance en 30 min.

## Contexte à lire
`deploy/`, `docs/email.md`, `docs/sms.md`, `docs/rgpd.md`.

## Spécification
`docs/install.md` :
1. Choisir un hébergement (tableau : VPS Hetzner/OVH/Scaleway ~4–6 €/mois, Raspberry Pi au cabinet + nom de domaine, coûts annuels totaux).
2. Nom de domaine + DNS (A/AAAA, SPF/DKIM pour l'email).
3. Installer Docker, cloner, remplir `.env`, `docker compose up -d`.
4. Créer le compte admin, configurer le cabinet (captures d'écran).
5. Configurer email et SMS (renvois vers `email.md` / `sms.md`).
6. Sauvegardes et test de restauration.
7. Mettre à jour (`docker compose pull && up -d`), lire le changelog.
8. Dépannage (FAQ).

+ `docs/cost.md` : estimation du coût mensuel total (hébergement + domaine + SMS + sauvegardes) pour 3 profils (solo 100 RDV/sem, cabinet 3 kinés, Raspberry Pi). Reprendre les chiffres SMS de `docs/sms.md` (ne pas les dupliquer : lien + total).

## Critères d'acceptation
- [ ] Suivi à la lettre sur une VM neuve par quelqu'un d'autre (ou un agent) sans blocage.

## Vérification
Relecture + installation réelle.

## Hors scope
Offre d'hébergement géré.

## Suivi

> Rempli par l'agent pendant et à la fin de chaque session sur cette tâche. Le statut officiel est dans `tasks/README.md`.

- **Sessions** : —
- **Fait** : —
- **Reste à faire** : —
- **Notes de reprise** (pièges, état intermédiaire, commandes utiles) : —
- **Écarts avec la spécification** (et pourquoi) : —
