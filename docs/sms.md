# SMS : fournisseurs et coûts

> Relevé du 2026-10-15, prix vers la France. Les tarifs bougent : vérifier sur le site du fournisseur avant de chiffrer.

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

## Configuration

Le fournisseur se choisit par `DOCTOLIBRE_SMS_DRIVER` (`log` par défaut, `http`, `android`, `ovh`) ; changer de fournisseur = changer ces variables, rien d'autre. Variables communes :

| Variable | Rôle |
|---|---|
| `DOCTOLIBRE_SMS_SENDER` | Expéditeur alphanumérique (OVH, ou `{{.Sender}}` dans le driver `http`). Ignoré par `android`. |
| `DOCTOLIBRE_SMS_TRANSLITERATE` | `true` (défaut) : `ê`->`e`, `œ`->`oe`, apostrophes typographiques… pour rester en GSM-7 (1 segment). |

Les variables sont validées au démarrage : un driver mal configuré empêche le démarrage.

### `log` (développement)

Rien à configurer. Journalise un numéro masqué, le nombre de segments et la longueur ; jamais le corps (il peut contenir un code OTP ou un lien de gestion).

### `ovh`

| Variable | Valeur |
|---|---|
| `DOCTOLIBRE_SMS_OVH_SERVICE` | Nom du service, ex. `sms-ab123456-1` |
| `DOCTOLIBRE_SMS_OVH_APP_KEY` | Clé d'application (https://eu.api.ovh.com/createApp/) |
| `DOCTOLIBRE_SMS_OVH_APP_SECRET` | Secret d'application |
| `DOCTOLIBRE_SMS_OVH_CONSUMER_KEY` | Consumer key, avec le droit `POST /sms/*` |
| `DOCTOLIBRE_SMS_OVH_ENDPOINT` | Optionnel, défaut `https://eu.api.ovh.com/1.0` |

L'expéditeur (`DOCTOLIBRE_SMS_SENDER`) doit être déclaré dans l'espace client OVH ; sans lui, OVH répond avec son numéro court. Les SMS partent avec `noStopClause` (transactionnels : pas de « STOP » ajouté).

### `android` (SMS Gateway for Android)

| Variable | Valeur |
|---|---|
| `DOCTOLIBRE_SMS_ANDROID_URL` | Mode local : `http://192.168.1.20:8080` (IP du téléphone). Vide = cloud (`https://api.sms-gate.app/3rdparty/v1`). |
| `DOCTOLIBRE_SMS_ANDROID_USER` | Identifiant affiché dans l'app |
| `DOCTOLIBRE_SMS_ANDROID_PASSWORD` | Mot de passe affiché dans l'app |

Le SMS part avec la SIM du téléphone (pas d'expéditeur personnalisé). En mode local, le serveur doit joindre le téléphone (même réseau ou VPN) ; ne pas exposer le port du téléphone sur Internet.

### `http` (webhook générique)

| Variable | Rôle |
|---|---|
| `DOCTOLIBRE_SMS_HTTP_URL` | URL appelée (obligatoire) |
| `DOCTOLIBRE_SMS_HTTP_METHOD` | Défaut `POST` |
| `DOCTOLIBRE_SMS_HTTP_HEADERS` | Objet JSON : `{"Content-Type":"application/json"}` |
| `DOCTOLIBRE_SMS_HTTP_BODY` | Corps, en `text/template` (obligatoire) |
| `DOCTOLIBRE_SMS_HTTP_BASIC_USER` / `_BASIC_PASSWORD` | Authentification basic |
| `DOCTOLIBRE_SMS_HTTP_BEARER` | Jeton Bearer (prioritaire sur basic) |

Champs du template : `{{.To}}` (E.164), `{{.Body}}`, `{{.Sender}}`. Fonctions : `{{json .Body}}` (chaîne JSON entre guillemets, à utiliser dans un corps JSON) et `{{urlquery .Body}}` (formulaire). Toute réponse 2xx est un succès ; 4xx (sauf 408/429) = échec définitif, le reste est retenté. Si la réponse JSON contient `sid`, `id` ou `messageId`, il est conservé comme référence. L'URL ne peut pas être un template : y mettre directement l'identifiant de compte.

Exemples (valeurs fictives, à adapter selon la documentation du fournisseur) :

**Twilio** (formulaire, auth basic = Account SID / Auth Token) :

```env
DOCTOLIBRE_SMS_DRIVER=http
DOCTOLIBRE_SMS_HTTP_URL=https://api.twilio.com/2010-04-01/Accounts/ACxxxxxxxx/Messages.json
DOCTOLIBRE_SMS_HTTP_HEADERS={"Content-Type":"application/x-www-form-urlencoded"}
DOCTOLIBRE_SMS_HTTP_BASIC_USER=ACxxxxxxxx
DOCTOLIBRE_SMS_HTTP_BASIC_PASSWORD=auth_token
DOCTOLIBRE_SMS_HTTP_BODY=To={{urlquery .To}}&From={{urlquery .Sender}}&Body={{urlquery .Body}}
DOCTOLIBRE_SMS_SENDER=+33700000000
```

**Brevo** (API transactionnelle, JSON, clé dans l'en-tête `api-key`) :

```env
DOCTOLIBRE_SMS_DRIVER=http
DOCTOLIBRE_SMS_HTTP_URL=https://api.brevo.com/v3/transactionalSMS/sms
DOCTOLIBRE_SMS_HTTP_HEADERS={"Content-Type":"application/json","api-key":"xkeysib-..."}
DOCTOLIBRE_SMS_HTTP_BODY={"sender":{{json .Sender}},"recipient":{{json .To}},"content":{{json .Body}},"type":"transactional"}
DOCTOLIBRE_SMS_SENDER=KINEDUPONT
```

**Plivo** (JSON, auth basic = Auth ID / Auth Token) :

```env
DOCTOLIBRE_SMS_DRIVER=http
DOCTOLIBRE_SMS_HTTP_URL=https://api.plivo.com/v1/Account/MAxxxxxxxx/Message/
DOCTOLIBRE_SMS_HTTP_HEADERS={"Content-Type":"application/json"}
DOCTOLIBRE_SMS_HTTP_BASIC_USER=MAxxxxxxxx
DOCTOLIBRE_SMS_HTTP_BASIC_PASSWORD=auth_token
DOCTOLIBRE_SMS_HTTP_BODY={"src":{{json .Sender}},"dst":{{json .To}},"text":{{json .Body}}}
DOCTOLIBRE_SMS_SENDER=+33700000000
```

Ces exemples n'ont pas été testés contre les API réelles (seulement contre un serveur de test local).

## Règles d'enfilement (utilisées par T023)

- Opt-out : `patient.sms_opt_in = false` => pas de SMS. Le service qui enfile ne doit pas créer le message ; il copie aussi la valeur dans le payload (`sms_opt_in`) pour un second contrôle à l'envoi.
- Plage silencieuse : `notify.SMSRunAt(now, loc, isOTP)` renvoie l'instant d'envoi (8 h locales suivantes entre 20 h et 8 h ; immédiat pour un OTP).
- Payload outbox `kind='sms'` : `{"body":"texte rendu","sms_opt_in":true,"otp":false}`.

## Sources

- [OVHcloud SMS Pro (officiel)](https://www.ovhcloud.com/fr/sms/)
- [Twilio — SMS pricing France (officiel)](https://www.twilio.com/en-us/sms/pricing/fr)
- [Plivo — SMS pricing France](https://www.plivo.com/sms/pricing/fr/)
- [Conexteo — comparatif plateformes SMS 2026](https://conexteo.com/comparatif-plateforme-sms-pro/)
- [SMS Proxima — OVH vs SMS Proxima](https://sms-proxima.com/sms-ovh-vs-sms-proxima)
- [textbee — Twilio France](https://textbee.dev/sms-pricing/twilio/fr)
