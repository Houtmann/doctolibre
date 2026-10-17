# T086 — Fournisseurs email et SMS configurables par cabinet

> Spécification rédigée le 2026-10-17 par l'orchestrateur, à la demande de l'humain : fournisseurs email et SMS réglables depuis l'admin, **par cabinet**, et chaque canal **complètement désactivable**.

- **Phase** : 5
- **Dépend de** : T023, T033 (UI en daisyUI : T080–T084 en review mais sur `main`, suivre leurs motifs)
- **Taille estimée** : L
- **Exécution** : sonnet (`task-implementer`) — revue Opus : **renforcée** (secrets chiffrés en base, appels réseau vers des hôtes saisis par un utilisateur, effets sur la réservation en ligne)

## Objectif
Dans l'admin, chaque cabinet choisit pour l'email et pour le SMS : le fournisseur du serveur (variables d'environnement, comportement actuel), son propre fournisseur (identifiants chiffrés en base), ou la désactivation complète du canal. Le changement s'applique au message suivant, sans redémarrage, et un bouton envoie un message de test.

## Contexte à lire
`internal/config/email.go`, `internal/config/sms.go` (structures, `validate`/`Validate`), `cmd/doctolibre/dispatch.go` (construction actuelle des senders, une fois au démarrage), `internal/notify/email.go`, `internal/notify/sms.go`, `internal/notify/smtp.go`, `internal/notify/sms/*` (drivers), `internal/outbox/worker.go` (`Dispatcher`, statuts, `cancelled`), `internal/outbox/outbox.go` (`Msg`, `Enqueue`), `internal/service/notification_settings.go` (réglages de canal par événement, T023) et sa page admin, `internal/service/otp.go` + `internal/service/booking.go` (choix du canal du code), `internal/auth/team.go` (invitations par email), `internal/config/config.go` (`SecretKey`), migration `00012_notifications.sql`.

## Spécification

### Modèle (migration 00023)
- Table `practice_notify_transport` : `practice_id uuid REFERENCES practice(id)`, `kind text CHECK (kind IN ('email','sms'))`, `mode text NOT NULL CHECK (mode IN ('server','custom','disabled'))`, `driver text` (NULL sauf `custom` ; mêmes valeurs que la config : `smtp` pour l'email, `http|android|ovh` pour le SMS ; `log` refusé en `custom`), `settings jsonb NOT NULL DEFAULT '{}'` (paramètres **non secrets** : hôte, port, TLS, expéditeur, URL, service OVH, endpoint…), `secrets bytea` (secrets chiffrés, voir plus bas), `updated_at timestamptz NOT NULL`, PK `(practice_id, kind)`. `CHECK ((mode = 'custom') = (driver IS NOT NULL))`.
- Absence de ligne = `server` : les installations existantes ne changent pas de comportement.
- `make sqlc` ; un seul agent sur `sqlc` à la fois.

### Chiffrement des secrets
- Secrets concernés : mot de passe SMTP, mot de passe de la passerelle Android, clés OVH (app secret, consumer key), en-têtes d'authentification du driver `http` s'il en a.
- AES-256-GCM (stdlib), clé dérivée de `DOCTOLIBRE_SECRET_KEY` par HMAC-SHA256 avec un libellé fixe (`"doctolibre/notify-transport/v1"`), nonce aléatoire `crypto/rand` préfixé au chiffré, `practice_id || kind` en données associées (un chiffré copié vers un autre cabinet ou canal ne se déchiffre pas). Petit paquet dédié (ex. `internal/secretbox`) ou fichier dans `notify` : au choix, sans dépendance nouvelle.
- Les secrets ne sont **jamais** renvoyés au navigateur : le formulaire affiche « défini » ; champ laissé vide = valeur conservée ; case « effacer » pour le retirer.
- Jamais dans les logs, ni dans `audit_log.meta`, ni dans les messages d'erreur rendus (une erreur SMTP peut contenir le nom d'utilisateur : ne rendre qu'un message générique + un ID de log).
- Échec de déchiffrement (clé changée) : le canal se comporte comme en erreur d'envoi (message `failed`, retentes normales), log `practice_id` + `kind`, et la page admin affiche « identifiants illisibles, à ressaisir ».

### Envoi (dispatcher)
- Le dispatcher construit dans `cmd/doctolibre/dispatch.go` choisit le sender **par message** à partir de `m.PracticeID` et `m.Kind` :
  - `server` → sender actuel construit depuis l'environnement (inchangé) ;
  - `custom` → sender construit depuis la ligne en base, mis en cache par `(practice_id, kind, updated_at)` ; une modification change `updated_at`, donc le message suivant utilise la nouvelle config (pas d'invalidation manuelle, pas de redémarrage) ;
  - `disabled` → le message passe en `cancelled` sans appel réseau ni retente (raison lisible dans la colonne d'erreur existante, ex. `channel_disabled`), y compris les messages déjà en file avant la désactivation.
- Lecture de la ligne via une requête sqlc dédiée hors de toute transaction métier ; aucun appel réseau dans une transaction.
- La validation d'une config `custom` réutilise les règles de `config` (extraire ce qu'il faut pour valider une `EmailConfig` / `SMSConfig` sans passer par l'environnement) ; pas de seconde copie des règles.

### Hôtes saisis par le cabinet (SSRF)
- Les drivers `smtp`, `http` et `android` en mode `custom` font appeler par le serveur un hôte saisi dans l'admin. Par défaut, refuser les adresses de bouclage, privées, link-local et non routables, **à la connexion** (contrôle de l'IP résolue dans un `net.Dialer.Control`, pas seulement à la saisie : rebinding DNS).
- Variable `DOCTOLIBRE_NOTIFY_ALLOW_PRIVATE_HOSTS` (défaut `false`) pour l'autoriser, cas de la passerelle Android sur le réseau du cabinet ; documentée dans `docs/install.md` et `docs/sms.md`.
- `http` : `https` obligatoire sauf si la variable ci-dessus est vraie. Aucun suivi de redirection.

### Désactivation et effets de bord
- Réglages par événement (T023) : à l'enregistrement d'un canal `disabled`, dans la même transaction, les réglages qui l'utilisent sont ramenés à l'autre canal (`both` → l'autre) ou à `none` ; la page des réglages de T023 n'offre plus le canal désactivé. Message flash qui le dit.
- Code de vérification patient (réservation, liste d'attente, tout parcours public qui envoie un code) : seuls les canaux actifs sont proposés ; si **aucun** canal n'est actif, la réservation en ligne et l'inscription en liste d'attente sont fermées avec un message neutre invitant à appeler le cabinet (téléphone du cabinet affiché s'il existe). Le service refuse aussi un canal désactivé (pas seulement l'UI).
- Invitations d'équipe (email) : email désactivé → l'invitation est refusée avec une erreur claire (« activez l'email pour inviter »). Pas de lien copiable dans cette tâche.
- Tableau de bord : bandeau d'avertissement quand un canal est désactivé.

### Admin
- Page `/admin/notifications/transport` (ou section de la page de T023, au choix, documenté dans Suivi), visible et modifiable seulement avec la permission qui protège déjà les réglages de notification du cabinet ; contrôle dans le service, filtre `practice_id` en SQL.
- Par canal : choix `Fournisseur du serveur` / `Mon fournisseur` / `Désactivé`, puis les champs du driver choisi. Sans JS : tous les blocs de champs du canal sont rendus et seuls ceux du driver choisi sont lus ; htmx peut n'afficher que le bloc utile.
- Mode `server` : afficher le driver du serveur (`smtp`, `log`, `ovh`…) **sans** aucun de ses paramètres, et l'avertissement existant si c'est `log` en production.
- Bouton « Envoyer un test » par canal : envoie **directement** (pas via l'outbox, hors transaction) un message court à l'email de l'utilisateur connecté, ou au numéro saisi pour le SMS, avec la config **enregistrée**. Rate limit (ex. 5 / heure / cabinet) ; résultat affiché (succès, ou erreur générique + ID de log).
- `audit_log` : changement de mode, de driver, de secret (« modifié », jamais la valeur), envoi de test — IDs seulement.
- daisyUI, `<label>` sur chaque champ, mots de passe en `type="password"` `autocomplete="new-password"`, avertissement visible sur le stockage des identifiants. `make css`.

### Doc
- `docs/install.md` : réglage par cabinet, priorité (l'admin passe devant l'environnement pour ce cabinet), `DOCTOLIBRE_NOTIFY_ALLOW_PRIVATE_HOSTS`, **changer `DOCTOLIBRE_SECRET_KEY` rend les identifiants enregistrés illisibles** (à ressaisir).
- `docs/rgpd.md` : identifiants de fournisseurs stockés chiffrés ; qui détient la base et la clé peut les lire.
- `docs/decisions.md` : géré par l'orchestrateur.

## Critères d'acceptation
- [x] Sans ligne en base, les envois sont identiques à avant (tests existants verts, test dédié).
- [x] Cabinet A en `custom` SMTP, cabinet B en `server` : chaque message part par le bon sender (test avec faux SMTP et faux SMS).
- [x] Modifier la config `custom` est pris en compte au message suivant sans redémarrer le worker (test).
- [x] Canal `disabled` : les messages, y compris ceux déjà en file, passent en `cancelled` sans appel réseau (test).
- [x] Secrets : chiffrés en base (la colonne ne contient pas la valeur en clair), jamais rendus dans le HTML, absents des logs et de `audit_log` (tests) ; chiffré déplacé vers un autre cabinet → échec de déchiffrement (test) ; champ vide conserve, « effacer » retire (test).
- [x] Hôte privé ou de bouclage refusé par défaut à la connexion, accepté avec `DOCTOLIBRE_NOTIFY_ALLOW_PRIVATE_HOSTS=true` (test avec un serveur sur 127.0.0.1).
- [x] Désactiver un canal réécrit les réglages par événement qui l'utilisaient, dans la même transaction (test).
- [x] Code patient : canal désactivé refusé par le service ; aucun canal actif → réservation en ligne et liste d'attente fermées, message neutre (tests service + handler).
- [x] Invitation refusée si l'email est désactivé (test).
- [ ] Permissions : rôle sans droit → 403, autre cabinet → 404 (tests handler).
- [x] Bouton de test : succès et échec affichés, rate limit (tests avec faux sender).
- [x] Formulaires utilisables sans JS (POST + redirect) ; `make css` à jour ; `make check` vert.
- [ ] Dans le navigateur : page de réglage clair/sombre à 360 et 1280 px, envoi de test vers Mailpit.

## Vérification
```bash
make sqlc && make css && make check
```

## Hors scope
Interdire au niveau serveur les fournisseurs propres aux cabinets (variable d'opérateur), lien d'invitation copiable quand l'email est désactivé, plusieurs fournisseurs par canal (repli), rotation de `DOCTOLIBRE_SECRET_KEY` avec re-chiffrement, quotas ou suivi de coût SMS par cabinet, réglages par praticien.

## Suivi

> Rempli par l'agent pendant et à la fin de chaque session sur cette tâche. Le statut officiel est dans `tasks/README.md`.

- **Sessions** : 2026-10-17 (sonnet), une session.
- **Fait** : migration 00023 (`practice_notify_transport`) + requêtes sqlc (`notify_transport.sql`, `MarkOutboxCancelled`) ; `internal/secretbox` (AES-256-GCM, clé HMAC-SHA256(SECRET_KEY, libellé), AAD = practice_id||kind) ; `DOCTOLIBRE_NOTIFY_ALLOW_PRIVATE_HOSTS` ; `notify/guard.go` (contrôle de l'IP résolue dans `net.Dialer.Control`, client HTTP sans proxy ni redirection) ; `notify/transport.go` (`Transports`, `PracticeDispatcher` avec cache par `(cabinet, canal, updated_at)`) ; `outbox.Cancel` / `IsCancelled` + statut `cancelled` dans le worker ; service `NotifyTransport` (vue, mise à jour chiffrée, repli des réglages par événement dans la même transaction, test d'envoi limité à 5/h/cabinet, audit) ; refus d'un canal désactivé pour le code patient (réservation + liste d'attente) et fermeture si aucun canal ; refus d'invitation si l'email est désactivé (`auth.ErrEmailDisabled`) ; page `/admin/settings/notifications/transport` (sans JS, POST + redirect), options de canal masquées dans les réglages de notification, bandeau au tableau de bord ; pages publiques fermées avec message neutre + téléphone ; docs `install.md`, `sms.md`, `rgpd.md`, `.env.example`, `docker-compose.yml`.
- **Reste à faire** : vérification dans le navigateur (page clair/sombre à 360 et 1280 px, envoi de test vers Mailpit), non faite. Test 404 « autre cabinet » non écrit (voir écarts).
- **Notes de reprise** : le mode `server` garde le chemin existant tel quel (`buildDispatcher` inchangé, `perPractice` l'enveloppe quand le pool existe). Erreur `outbox.Cancel(notify.ErrChannelDisabled)` => statut `cancelled`, `last_error = channel_disabled`, sans retente ni nettoyage du payload (renvoi manuel possible). `Transports.NewEmail` / `NewSMS` sont des crochets de test (faux senders). Passer en mode `server` ou `disabled` efface les identifiants enregistrés.
- **Écarts avec la spécification** :
  - Page séparée `/admin/settings/notifications/transport` (lien depuis les paramètres et les notifications), mêmes rôles que les réglages de notification (admin, praticien).
  - Critère « autre cabinet → 404 » non testable tel quel : aucune URL ne porte d'identifiant de cabinet (tout est borné au cabinet de l'utilisateur connecté, `practice_id` en SQL) ; testés : secrétaire 403 (service et handler), canal inconnu 404.
  - L'endpoint OVH n'est pas saisissable (toujours celui par défaut) pour ne pas ouvrir un second hôte saisi ; l'en-tête `headers` du driver `http` est entièrement traité comme secret.
  - L'URL du driver `android` doit aussi être en https hors `DOCTOLIBRE_NOTIFY_ALLOW_PRIVATE_HOSTS` (URL vide = cloud, https).
  - Le contrôle SSRF n'est appliqué qu'aux senders `custom` ; le mode `server` est inchangé.
  - Une config `custom` devenue invalide au moment de l'envoi donne une erreur permanente (`failed`), une base illisible (clé changée) une erreur temporaire (retentes normales).
  - `render.go` : un 503 avec message métier conserve son message (utilisé pour la réservation fermée).
