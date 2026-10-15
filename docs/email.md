# Envoi des emails

Doctolibre envoie ses emails (confirmations, rappels, mot de passe oublié) par **n'importe quel serveur SMTP**. Les messages passent d'abord par l'outbox (voir [`PLAN.md`](../PLAN.md)) : si le serveur SMTP est indisponible, l'envoi est retenté automatiquement ; si le serveur refuse définitivement l'adresse (réponse 5xx), le message est marqué en échec.

## Variables d'environnement

| Variable | Défaut | Rôle |
|---|---|---|
| `DOCTOLIBRE_EMAIL_DRIVER` | `log` | `smtp` (envoi réel) ou `log` (dev : écrit des fichiers `.eml`, n'envoie rien) |
| `DOCTOLIBRE_SMTP_HOST` | | Serveur SMTP (obligatoire avec `smtp`) |
| `DOCTOLIBRE_SMTP_PORT` | selon `SMTP_TLS` | 587 (`starttls`), 465 (`tls`), 25 (`none`) |
| `DOCTOLIBRE_SMTP_USER` / `DOCTOLIBRE_SMTP_PASSWORD` | vide | Identifiants ; vont ensemble. Vide = pas d'authentification |
| `DOCTOLIBRE_SMTP_FROM` | | Expéditeur, ex. `Cabinet Dupont <rdv@cabinet-dupont.fr>` (obligatoire avec `smtp`) |
| `DOCTOLIBRE_SMTP_TLS` | `starttls` | `starttls` (connexion en clair puis chiffrée), `tls` (chiffrée dès le début), `none` (jamais en production) |
| `DOCTOLIBRE_EMAIL_LOG_DIR` | `tmp/mails` | Driver `log` : dossier des `.eml` |

Le mot de passe SMTP n'est jamais journalisé. Le client refuse d'envoyer le mot de passe sur une connexion non chiffrée (sauf vers `localhost`).

> En production, pensez à passer `DOCTOLIBRE_EMAIL_DRIVER=smtp` : avec le driver `log` par défaut, aucun email ne part.

## Exemples

### OVH (MX Plan / email pro)
```
DOCTOLIBRE_EMAIL_DRIVER=smtp
DOCTOLIBRE_SMTP_HOST=ssl0.ovh.net
DOCTOLIBRE_SMTP_PORT=465
DOCTOLIBRE_SMTP_TLS=tls
DOCTOLIBRE_SMTP_USER=rdv@cabinet-dupont.fr
DOCTOLIBRE_SMTP_PASSWORD=…
DOCTOLIBRE_SMTP_FROM=Cabinet Dupont <rdv@cabinet-dupont.fr>
```

### Brevo (ex-Sendinblue)
```
DOCTOLIBRE_EMAIL_DRIVER=smtp
DOCTOLIBRE_SMTP_HOST=smtp-relay.brevo.com
DOCTOLIBRE_SMTP_PORT=587
DOCTOLIBRE_SMTP_TLS=starttls
DOCTOLIBRE_SMTP_USER=<identifiant SMTP Brevo>
DOCTOLIBRE_SMTP_PASSWORD=<clé SMTP>
DOCTOLIBRE_SMTP_FROM=Cabinet Dupont <rdv@cabinet-dupont.fr>
```
L'expéditeur doit être un domaine ou une adresse validée dans Brevo.

### Gmail / Google Workspace
```
DOCTOLIBRE_EMAIL_DRIVER=smtp
DOCTOLIBRE_SMTP_HOST=smtp.gmail.com
DOCTOLIBRE_SMTP_PORT=587
DOCTOLIBRE_SMTP_TLS=starttls
DOCTOLIBRE_SMTP_USER=moi@gmail.com
DOCTOLIBRE_SMTP_PASSWORD=<mot de passe d'application>
DOCTOLIBRE_SMTP_FROM=Cabinet Dupont <moi@gmail.com>
```
Gmail exige la validation en deux étapes et un « mot de passe d'application » (pas votre mot de passe habituel). Gmail réécrit l'expéditeur avec votre adresse Gmail ; limite d'environ 500 messages par jour, suffisante pour un cabinet.

### Postfix local
Si un Postfix tourne sur la même machine : `SMTP_HOST=localhost`, `SMTP_PORT=25`, `SMTP_TLS=none`, sans identifiants.

## En développement

- **Driver `log`** (défaut) : chaque email est écrit dans `tmp/mails/*.eml` (ouvrable avec n'importe quel client mail). Le journal ne contient que l'identifiant du message et le chemin du fichier, jamais le destinataire ni le contenu.
- **Mailpit** (faux serveur SMTP avec interface web) :
  ```bash
  docker compose -f deploy/docker-compose.dev.yml up -d mailpit
  DOCTOLIBRE_EMAIL_DRIVER=smtp DOCTOLIBRE_SMTP_HOST=localhost DOCTOLIBRE_SMTP_PORT=1025 \
    DOCTOLIBRE_SMTP_TLS=none DOCTOLIBRE_SMTP_FROM=rdv@localhost make run
  ```
  Les messages reçus s'affichent sur http://localhost:8025.

## Pour que les emails n'aillent pas en spam : SPF, DKIM, DMARC

Un email est jugé « de confiance » si le domaine de l'expéditeur prouve que le serveur d'envoi a le droit de parler en son nom. Trois enregistrements DNS sur **votre** domaine (celui de `SMTP_FROM`) :

- **SPF** : liste des serveurs autorisés à envoyer pour le domaine. Enregistrement TXT sur le domaine, par exemple `v=spf1 include:spf.mail.ovh.net ~all` (OVH) ou `v=spf1 include:spf.brevo.com ~all` (Brevo). Un seul enregistrement SPF par domaine : fusionnez les `include:`.
- **DKIM** : signature cryptographique ajoutée par le serveur d'envoi. Le fournisseur (OVH, Brevo…) vous donne une clé publique à publier en TXT (ou CNAME) sous un nom du type `selecteur._domainkey.votre-domaine.fr`. Doctolibre n'a rien à signer lui-même : c'est le serveur SMTP qui le fait.
- **DMARC** : dit aux destinataires quoi faire si SPF et DKIM échouent, et vous envoie des rapports. Pour commencer : TXT sur `_dmarc.votre-domaine.fr` avec `v=DMARC1; p=none; rua=mailto:postmaster@votre-domaine.fr`. Passez à `p=quarantine` une fois les rapports propres.

Pour vérifier : envoyez un email de test à une adresse Gmail, ouvrez « Afficher l'original » et cherchez `SPF: PASS`, `DKIM: PASS`, `DMARC: PASS`.

Utilisez une adresse d'expéditeur sur **votre propre domaine** (pas une adresse Gmail ou Outlook que votre serveur n'a pas le droit de signer) et une adresse qui reçoit vraiment les réponses ou les retours d'erreur.

## Mot de passe oublié (personnel)

La page de connexion propose « Mot de passe oublié ? » (`/admin/password/forgot`). Le lien reçu est valable 1 heure, ne sert qu'une fois, et la page répond de la même façon que l'adresse existe ou non. Changer le mot de passe déconnecte toutes les sessions du compte. Le jeton n'est stocké que haché en base ; il figure en clair dans le message en attente de l'outbox jusqu'à son envoi.

## Templates

`internal/notify/templates/email/<nom>.{subject,txt,html}.tmpl` : sujet et texte en `text/template`, HTML en `html/template` avec la mise en page commune `layout.html.tmpl` (tableaux, styles en ligne, aucune image distante). Les données sont le payload JSON du message d'outbox ; une clé manquante est une erreur permanente.
