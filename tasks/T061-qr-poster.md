# T061 — Affiche imprimable avec QR code

> UI à faire en daisyUI ; à lancer après T080 (décision du 2026-10-16).

- **Phase** : 5
- **Dépend de** : T014
- **Taille estimée** : S
- **Exécution** : sonnet (`task-implementer`) — revue Opus : standard

## Objectif
Le cabinet imprime une affiche A4 avec l'adresse de réservation en ligne et un QR code.

## Contexte à lire
`internal/web/public.go` (pages `/p/{practice}`), `internal/web/admin.go` (tableau de bord : adresse de la page de réservation), `internal/web/templates/layout/`, `internal/web/input.css` (Tailwind/daisyUI ; `app.css` n'existe plus depuis T084), `internal/config/config.go` (`BaseURL`), `go.mod`, `docs/decisions.md`, `AGENTS.md` (dépendances autorisées).

## Spécification
- Page `GET /admin/poster` (authentifiée) : A4 imprimable (variantes `print:` Tailwind ou `@media print` dans `input.css`, navigation masquée ; `make css` ensuite) avec nom du cabinet, praticiens, URL publique (`BaseURL` + `/p/{practice}`) et QR code **SVG généré côté serveur**. Pas de CDN, pas de style inline.
- **Choix du QR à faire par l'humain avant implémentation** : (1) dépendance `rsc.io/qr` (pure Go, petite), à justifier dans `docs/decisions.md` (aucune lib QR dans `go.mod` aujourd'hui) ; ou (2) encodeur maison limité (version fixe, niveau de correction M, mode octet). Ne rien implémenter tant que ce choix n'est pas écrit dans « Notes de reprise ».
- Test : le SVG est généré de façon déterministe pour une URL donnée (golden), la page contient l'URL et le nom du cabinet ; `BaseURL` vide → message explicite plutôt qu'un QR vide.

## Critères d'acceptation
- [x] Choix du QR validé par l'humain et consigné.
- [ ] `/admin/poster` s'imprime proprement sur A4 (vérifier en aperçu d'impression).
- [ ] Le QR décode vers l'URL publique (vérifier avec un lecteur).
- [x] Tests ajoutés.

## Vérification
```bash
make check
```

## Hors scope
Affiches personnalisables, QR par praticien ou par motif.

## Suivi

> Rempli par l'agent pendant et à la fin de chaque session sur cette tâche. Le statut officiel est dans `tasks/README.md`.

- **Sessions** : 2026-11-09 (sonnet).
- **Fait** : `internal/web/poster.go` (`qrSVG` : `rsc.io/qr` niveau M, zone de silence 4 modules, fond blanc + un seul `<path>` noir en attributs de présentation, rendu inline ; route `GET /admin/poster` pour tous les rôles authentifiés, comme le tableau de bord), template `admin_poster.html`, bouton « Imprimer » (`data-print` dans `app.js`), `@media print` A4 dans `input.css` (navigation masquée), `ui.css` régénéré, lien depuis le tableau de bord, `Dashboard.PracticeName` ajouté au service. Tests : golden `internal/web/testdata/poster_qr.svg`, page (nom, URL, SVG, secrétariat inclus, lien du tableau de bord), `BaseURL` vide → message `DOCTOLIBRE_BASE_URL` sans QR. `go.mod` : seul `rsc.io/qr v0.2.0` ajouté (direct).
- **Reste à faire** : aperçu d'impression A4 et lecture du QR avec un vrai lecteur (non faits, d'où critères non cochés).
- **Notes de reprise** (pièges, état intermédiaire, commandes utiles) : Choix de l'humain (2026-11-09) : **dépendance `rsc.io/qr`** (consignée dans `docs/decisions.md`). Spec corrigée par l'orchestrateur : `app.css` → `input.css`.
- **Écarts avec la spécification** (et pourquoi) : `BaseURL` vide → pas de repli sur l'hôte de la requête (contrairement au tableau de bord) : message explicite, conformément à la spec ; l'affiche n'est pas dans le menu latéral (lien depuis le tableau de bord).
