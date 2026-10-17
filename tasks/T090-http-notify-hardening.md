# T090 — Durcissement HTTP et notifications (corps, XFF, logs, SSRF, ICS)

- **Phase** : 5
- **Dépend de** : T087, T086
- **Taille estimée** : S (< 200)
- **Exécution** : sonnet (`task-implementer`) — revue Opus : renforcée (sécurité)

## Objectif
Le corps des requêtes non sûres est toujours borné, la résolution d'IP derrière proxy ne retombe jamais sur l'IP du proxy, aucun email/téléphone ne fuit dans les logs d'un envoi de test, et le garde-fou SSRF couvre les préfixes IPv6 manquants.

## Contexte à lire
- `internal/httpx/middleware.go` (middleware CSRF ~l. 231, `resolveClientIP` ~l. 299) et `middleware_test.go` — **après** T087, qui modifie ces fichiers
- `internal/service/notify_transport.go` (`testFailure` ~l. 480), `internal/outbox/worker.go` (~l. 317 : modèle « ne pas logger le texte d'erreur »)
- `internal/notify/guard.go` (`forbiddenPrefixes`) et `guard_internal_test.go`
- `internal/ics/ics.go` (`Event.Status`), `internal/config/config.go` (`NotifyAllowPrivateHosts`), `cmd/doctolibre/serve.go`

## Spécification
1. **Corps non borné avec `X-CSRF-Token`** — `MaxBytesReader` n'est posé que si l'en-tête est absent (htmx l'envoie toujours) : le plafond retombe à 10 Mio implicites de `net/http`. Poser `MaxBytesReader` (multipart → `MaxMultipartLen`, sinon `maxCSRFFormLen`) pour toute méthode non sûre, avant le test de l'en-tête. Test : POST avec en-tête et corps > limite → 413 (ou rejet), POST htmx normal OK.
2. **XFF invalide** — un élément non parsable de `X-Forwarded-For` fait `return remote` (IP du proxy) : tous les clients partageraient un seau. Le traiter comme la fin de la chaîne de confiance : retourner la dernière IP non fiable déjà trouvée (parcours droite → gauche), sinon l'élément le plus à droite valide, et jamais l'IP d'un proxy de confiance quand un client est identifiable. Test table-driven (XFF invalide à gauche, au milieu, seul). Rendre explicite dans `deploy/Caddyfile` que Caddy remplace l'en-tête client (`header_up X-Forwarded-For {remote_host}` ou `trusted_proxies` documenté).
3. **Destinataire dans les logs de test d'envoi** — `testFailure` logge `err.Error()` après masquage des secrets, mais pas du destinataire (une réponse SMTP 550 contient l'adresse ; en mode `server` les secrets d'environnement ne sont pas masqués non plus). Ne plus logger le texte brut : logger une classification (code SMTP via `*textproto.Error`, statut HTTP, « timeout », « tls », « dial ») + IDs ; le texte complet reste affiché à l'admin dans la réponse (déjà le cas) mais avec destinataire et secrets masqués. Test : erreur contenant l'adresse → absente du log capturé.
4. **SSRF IPv6** — ajouter à `forbiddenPrefixes` : `64:ff9b:1::/48` (RFC 8215), `fec0::/10`, `2002::/16` (6to4), `2001::/32` (Teredo). Tests pour chacun.
5. **ICS (hygiène)** — `Event.Status` : liste blanche `CONFIRMED`/`TENTATIVE`/`CANCELLED`, valeur inconnue → ligne omise. Aujourd'hui seules des constantes y arrivent (pas exploitable).
6. **Config** — au démarrage, `slog.Warn` si `Env == prod` et `NotifyAllowPrivateHosts` (comportement voulu et documenté, on le rend visible ; pas de refus).

## Critères d'acceptation
- [ ] Corps borné avec ou sans en-tête CSRF (test).
- [ ] XFF malformé ne donne jamais l'IP du proxy comme clé (tests).
- [ ] Aucun email/téléphone/secret dans les logs d'un test d'envoi en échec (test).
- [ ] Préfixes IPv6 ajoutés et testés ; liste blanche ICS ; avertissement de config.
- [ ] `make check` vert.

## Vérification
```bash
make check
go test ./internal/httpx/ ./internal/notify/ ./internal/ics/ ./internal/service/ -run 'CSRF|ClientIP|Guard|Status|Transport' -count=1
```

## Hors scope
Refonte du rate limiting (T087) ; contenu de `outbox_message.last_error` (choix assumé, purge RGPD existante).

## Suivi

> Rempli par l'agent pendant et à la fin de chaque session sur cette tâche. Le statut officiel est dans `tasks/README.md`.

- **Sessions** : —
- **Fait** : —
- **Reste à faire** : —
- **Notes de reprise** (pièges, état intermédiaire, commandes utiles) : —
- **Écarts avec la spécification** (et pourquoi) : —
