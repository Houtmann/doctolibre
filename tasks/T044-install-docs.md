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

- **Sessions** : 2026-10-16 (sonnet)
- **Fait** : `docs/install.md` (8 sections demandées + variante systemd + aide-mémoire des sous-commandes), `docs/cost.md` (3 profils, SMS renvoyés vers `sms.md`), lien dans `docs/README.md`. Contenu relu contre deploy/, config, backup.md, sms.md, email.md, routes admin.
- **Reste à faire** : installation réelle sur VM neuve (critère non vérifiable ici) ; captures d'écran ; URL officielle du dépôt ; `docs/rgpd.md` (T043) à lier quand il existera.
- **Notes de reprise** : prix hébergeurs et matériel indicatifs, non vérifiés en ligne (datés 2026-10-16).
- **Écarts avec la spécification** : pas de captures d'écran (UI décrite en texte) ; pas de lien vers `rgpd.md` (inexistant) ; pas de CHANGELOG dans le dépôt (renvoi aux releases).
