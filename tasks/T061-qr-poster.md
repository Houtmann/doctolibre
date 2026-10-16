# T061 — Affiche imprimable avec QR code

- **Phase** : 5
- **Dépend de** : T014
- **Taille estimée** : S
- **Exécution** : sonnet (`task-implementer`) — revue Opus : standard

## Objectif
Le cabinet imprime une affiche A4 avec l'adresse de réservation en ligne et un QR code.

## Contexte à lire
`internal/web/public.go` (pages `/p/{practice}`), `internal/web/admin.go` (tableau de bord : adresse de la page de réservation), `internal/web/templates/layout/`, `internal/web/static/app.css`, `internal/config/config.go` (`BaseURL`), `go.mod`, `docs/decisions.md`, `AGENTS.md` (dépendances autorisées).

## Spécification
- Page `GET /admin/poster` (authentifiée) : A4 imprimable (`@media print` dans `app.css`, navigation masquée) avec nom du cabinet, praticiens, URL publique (`BaseURL` + `/p/{practice}`) et QR code **SVG généré côté serveur**. Pas de CDN, pas de style inline.
- **Choix du QR à faire par l'humain avant implémentation** : (1) dépendance `rsc.io/qr` (pure Go, petite), à justifier dans `docs/decisions.md` (aucune lib QR dans `go.mod` aujourd'hui) ; ou (2) encodeur maison limité (version fixe, niveau de correction M, mode octet). Ne rien implémenter tant que ce choix n'est pas écrit dans « Notes de reprise ».
- Test : le SVG est généré de façon déterministe pour une URL donnée (golden), la page contient l'URL et le nom du cabinet ; `BaseURL` vide → message explicite plutôt qu'un QR vide.

## Critères d'acceptation
- [ ] Choix du QR validé par l'humain et consigné.
- [ ] `/admin/poster` s'imprime proprement sur A4 (vérifier en aperçu d'impression).
- [ ] Le QR décode vers l'URL publique (vérifier avec un lecteur).
- [ ] Tests ajoutés.

## Vérification
```bash
make check
```

## Hors scope
Affiches personnalisables, QR par praticien ou par motif.

## Suivi

> Rempli par l'agent pendant et à la fin de chaque session sur cette tâche. Le statut officiel est dans `tasks/README.md`.

- **Sessions** : —
- **Fait** : —
- **Reste à faire** : —
- **Notes de reprise** (pièges, état intermédiaire, commandes utiles) : **BLOQUÉE tant que la décision dépendance (`rsc.io/qr` ou encodeur maison) n'est pas prise par l'humain.**
- **Écarts avec la spécification** (et pourquoi) : —
