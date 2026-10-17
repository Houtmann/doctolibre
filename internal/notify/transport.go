package notify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"doctolibre/internal/config"
	"doctolibre/internal/domain"
	"doctolibre/internal/outbox"
	"doctolibre/internal/secretbox"
	"doctolibre/internal/store/sqlcgen"
)

// Modes d'un canal (email ou SMS) pour un cabinet.
const (
	ModeServer   = "server"   // fournisseur du serveur (variables d'environnement)
	ModeCustom   = "custom"   // fournisseur propre au cabinet (identifiants chiffrés en base)
	ModeDisabled = "disabled" // canal désactivé
)

// Kinds de canal (identiques à outbox.KindEmail / outbox.KindSMS).
const (
	KindEmail = outbox.KindEmail
	KindSMS   = outbox.KindSMS
)

// ErrChannelDisabled : le cabinet a désactivé ce canal. Texte stocké dans
// outbox_message.last_error des messages annulés.
var ErrChannelDisabled = errors.New("channel_disabled")

// ErrUnreadable : les identifiants enregistrés ne se déchiffrent plus (clé changée).
var ErrUnreadable = errors.New("identifiants du fournisseur illisibles")

// Field décrit un champ de réglage d'un fournisseur ; Secret => chiffré en base, jamais réaffiché.
type Field struct {
	Key    string
	Secret bool
}

var (
	emailFields = []Field{{"host", false}, {"port", false}, {"tls", false}, {"from", false}, {"user", false}, {"password", true}}
	smsCommon   = []Field{{"sender", false}, {"transliterate", false}}
	smsFields   = map[string][]Field{
		config.SMSDriverHTTP:    {{"method", false}, {"url", false}, {"body", false}, {"basic_user", false}, {"basic_pass", true}, {"bearer", true}, {"headers", true}},
		config.SMSDriverAndroid: {{"url", false}, {"user", false}, {"password", true}},
		config.SMSDriverOVH:     {{"service", false}, {"app_key", false}, {"app_secret", true}, {"consumer_key", true}},
	}
)

// FieldsOf retourne les champs d'un fournisseur propre ; ok=false si (kind, driver) est inconnu.
func FieldsOf(kind, driver string) (fields []Field, ok bool) {
	switch kind {
	case KindEmail:
		return emailFields, driver == config.EmailDriverSMTP
	case KindSMS:
		f, ok := smsFields[driver]
		return append(append([]Field{}, smsCommon...), f...), ok
	}
	return nil, false
}

// Transport est le réglage d'un canal pour un cabinet.
type Transport struct {
	Mode, Driver string
	Settings     map[string]string // non secrets
	Secrets      map[string]string // en clair en mémoire seulement ; ne jamais journaliser
	UpdatedAt    time.Time
	// Unreadable : les secrets enregistrés ne se déchiffrent pas (Secrets est vide).
	Unreadable bool
}

func aad(practiceID uuid.UUID, kind string) []byte { return append(practiceID[:], kind...) }

// Transports lit, chiffre et construit les fournisseurs des cabinets.
type Transports struct {
	Pool         *pgxpool.Pool
	Box          secretbox.Box
	Env          config.Config // fournisseur du serveur (mode « server »)
	AllowPrivate bool          // DOCTOLIBRE_NOTIFY_ALLOW_PRIVATE_HOSTS
	Clock        domain.Clock
	Logger       *slog.Logger
	// NewEmail et NewSMS remplacent les constructeurs réels (tests) ; nil = défaut.
	NewEmail func(cfg config.EmailConfig, allowPrivate bool) (EmailSender, error)
	NewSMS   func(cfg config.SMSConfig, allowPrivate bool) (SMSSender, error)
}

func (t *Transports) logger() *slog.Logger {
	if t.Logger != nil {
		return t.Logger
	}
	return slog.Default()
}

// Decode transforme une ligne en Transport (déchiffre les secrets).
func (t *Transports) Decode(row sqlcgen.PracticeNotifyTransport) Transport {
	tr := Transport{Mode: row.Mode, Driver: row.Driver.String, UpdatedAt: row.UpdatedAt, Settings: map[string]string{}, Secrets: map[string]string{}}
	if len(row.Settings) > 0 {
		if err := json.Unmarshal(row.Settings, &tr.Settings); err != nil {
			tr.Settings = map[string]string{}
		}
	}
	if len(row.Secrets) > 0 {
		plain, err := t.Box.Open(row.Secrets, aad(row.PracticeID, row.Kind))
		if err == nil {
			err = json.Unmarshal(plain, &tr.Secrets)
		}
		if err != nil {
			tr.Secrets = map[string]string{}
			tr.Unreadable = true
			t.logger().Error("transport: identifiants illisibles", "practice_id", row.PracticeID, "kind", row.Kind)
		}
	}
	return tr
}

// Encode prépare les colonnes settings et secrets d'un Transport.
func (t *Transports) Encode(practiceID uuid.UUID, kind string, tr Transport) (settings, secrets []byte, err error) {
	if tr.Settings == nil {
		tr.Settings = map[string]string{}
	}
	if settings, err = json.Marshal(tr.Settings); err != nil {
		return nil, nil, fmt.Errorf("réglages: %w", err)
	}
	if len(tr.Secrets) == 0 {
		return settings, nil, nil
	}
	plain, err := json.Marshal(tr.Secrets)
	if err != nil {
		return nil, nil, fmt.Errorf("secrets: %w", err)
	}
	if secrets, err = t.Box.Seal(plain, aad(practiceID, kind)); err != nil {
		return nil, nil, err
	}
	return settings, secrets, nil
}

// Load lit le réglage d'un canal ; absence de ligne = mode « server ».
func (t *Transports) Load(ctx context.Context, q *sqlcgen.Queries, practiceID uuid.UUID, kind string) (Transport, error) {
	row, err := q.GetNotifyTransport(ctx, sqlcgen.GetNotifyTransportParams{PracticeID: practiceID, Kind: kind})
	if errors.Is(err, pgx.ErrNoRows) {
		return Transport{Mode: ModeServer}, nil
	}
	if err != nil {
		return Transport{}, fmt.Errorf("lecture du fournisseur %s: %w", kind, err)
	}
	return t.Decode(row), nil
}

// DefaultSMTPPort : port usuel selon le mode TLS.
func DefaultSMTPPort(tlsMode string) int {
	switch tlsMode {
	case config.SMTPTLSImplicit:
		return 465
	case config.SMTPTLSNone:
		return 25
	}
	return 587
}

// EmailConfigOf traduit un réglage SMTP de cabinet en config.EmailConfig.
func EmailConfigOf(tr Transport) (config.EmailConfig, error) {
	s := tr.Settings
	c := config.EmailConfig{
		Driver: config.EmailDriverSMTP, SMTPHost: strings.TrimSpace(s["host"]), SMTPUser: s["user"],
		SMTPPassword: tr.Secrets["password"], SMTPFrom: strings.TrimSpace(s["from"]), SMTPTLS: s["tls"],
	}
	if c.SMTPTLS == "" {
		c.SMTPTLS = config.SMTPTLSStartTLS
	}
	if strings.ContainsAny(c.SMTPHost, " \t/@\\") {
		return c, errors.New("hôte SMTP invalide")
	}
	if p := strings.TrimSpace(s["port"]); p == "" {
		c.SMTPPort = DefaultSMTPPort(c.SMTPTLS)
	} else if n, err := strconv.Atoi(p); err != nil || n < 1 || n > 65535 {
		return c, errors.New("port SMTP invalide")
	} else {
		c.SMTPPort = n
	}
	return c, nil
}

// SMSConfigOf traduit un réglage de cabinet en config.SMSConfig.
func SMSConfigOf(tr Transport) (config.SMSConfig, error) {
	s, sec := tr.Settings, tr.Secrets
	c := config.SMSConfig{
		Driver: tr.Driver, Sender: strings.TrimSpace(s["sender"]), Transliterate: s["transliterate"] != "false",
		AndroidURL: strings.TrimSpace(s["url"]), AndroidUser: s["user"], AndroidPass: sec["password"],
		OVHService: strings.TrimSpace(s["service"]), OVHAppKey: strings.TrimSpace(s["app_key"]),
		OVHAppSecret: sec["app_secret"], OVHConsumerKey: sec["consumer_key"],
		HTTP: config.SMSHTTPConfig{
			Method: strings.ToUpper(strings.TrimSpace(s["method"])), URL: strings.TrimSpace(s["url"]), Body: s["body"],
			BasicUser: s["basic_user"], BasicPass: sec["basic_pass"], Bearer: sec["bearer"],
		},
	}
	if c.HTTP.Method == "" {
		c.HTTP.Method = http.MethodPost
	}
	switch c.HTTP.Method {
	case http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch:
	default:
		return c, errors.New("méthode HTTP invalide")
	}
	if h := strings.TrimSpace(sec["headers"]); h != "" {
		if err := json.Unmarshal([]byte(h), &c.HTTP.Headers); err != nil {
			return c, errors.New("en-têtes : objet JSON attendu ({\"En-tête\":\"valeur\"})")
		}
	}
	return c, nil
}

func (t *Transports) emailSender(cfg config.EmailConfig) (EmailSender, error) {
	if t.NewEmail != nil {
		return t.NewEmail(cfg, t.AllowPrivate)
	}
	return NewCustomEmailSender(cfg, t.AllowPrivate)
}

func (t *Transports) smsSender(cfg config.SMSConfig) (SMSSender, error) {
	if t.NewSMS != nil {
		return t.NewSMS(cfg, t.AllowPrivate)
	}
	return NewCustomSMSSender(cfg, t.Clock, t.AllowPrivate)
}

// Check valide un réglage « custom » sans rien envoyer : règles de config puis construction du sender.
func (t *Transports) Check(kind string, tr Transport) error {
	_, _, err := t.build(kind, tr)
	return err
}

// build construit le sender d'un réglage « custom » et, pour le SMS, ses options d'envoi.
func (t *Transports) build(kind string, tr Transport) (any, config.SMSConfig, error) {
	switch kind {
	case KindEmail:
		cfg, err := EmailConfigOf(tr)
		if err != nil {
			return nil, config.SMSConfig{}, err
		}
		if err := cfg.Validate(); err != nil {
			return nil, config.SMSConfig{}, err
		}
		s, err := t.emailSender(cfg)
		return s, config.SMSConfig{}, err
	case KindSMS:
		cfg, err := SMSConfigOf(tr)
		if err != nil {
			return nil, cfg, err
		}
		if cfg.Driver == "" || cfg.Driver == config.SMSDriverLog {
			return nil, cfg, errors.New("driver SMS non autorisé pour un cabinet")
		}
		if err := cfg.Validate(); err != nil {
			return nil, cfg, err
		}
		s, err := t.smsSender(cfg)
		return s, cfg, err
	}
	return nil, config.SMSConfig{}, fmt.Errorf("kind %q inconnu", kind)
}

// customDispatcher construit le dispatcher outbox d'un réglage « custom ».
func (t *Transports) customDispatcher(kind string, tr Transport) (outbox.Dispatcher, error) {
	s, smsCfg, err := t.build(kind, tr)
	if err != nil {
		return nil, err
	}
	if kind == KindEmail {
		return NewEmailDispatcher(s.(EmailSender))
	}
	return SMSDispatch(s.(SMSSender), smsCfg.Sender, smsCfg.Transliterate), nil
}

// Resolved est un sender prêt à l'emploi pour l'envoi direct (message de test).
type Resolved struct {
	Email EmailSender
	SMS   SMSSender
	From  string // expéditeur SMS
	// Transliterate : le texte SMS doit rester en GSM-7.
	Transliterate bool
}

// Resolve construit le sender du canal d'un cabinet selon son réglage enregistré
// (serveur ou propre). ErrChannelDisabled / ErrUnreadable le cas échéant.
func (t *Transports) Resolve(ctx context.Context, q *sqlcgen.Queries, practiceID uuid.UUID, kind string) (Resolved, error) {
	tr, err := t.Load(ctx, q, practiceID, kind)
	if err != nil {
		return Resolved{}, err
	}
	switch tr.Mode {
	case ModeDisabled:
		return Resolved{}, ErrChannelDisabled
	case ModeServer:
		if kind == KindEmail {
			s, err := NewEmailSender(t.Env.Email, t.logger())
			return Resolved{Email: s}, err
		}
		s, err := NewSMSSender(t.Env.SMS, t.Clock, t.logger())
		return Resolved{SMS: s, From: t.Env.SMS.Sender, Transliterate: t.Env.SMS.Transliterate}, err
	}
	if tr.Unreadable {
		return Resolved{}, ErrUnreadable
	}
	s, smsCfg, err := t.build(kind, tr)
	if err != nil {
		return Resolved{}, err
	}
	if kind == KindEmail {
		return Resolved{Email: s.(EmailSender)}, nil
	}
	return Resolved{SMS: s.(SMSSender), From: smsCfg.Sender, Transliterate: smsCfg.Transliterate}, nil
}

// PracticeDispatcher choisit le fournisseur de chaque message selon le réglage
// de son cabinet. Les senders « custom » sont mis en cache par
// (cabinet, canal, updated_at) : une modification est prise en compte au message
// suivant, sans redémarrage.
type PracticeDispatcher struct {
	Server     outbox.Dispatcher // fournisseur du serveur (variables d'environnement)
	Transports *Transports

	mu    sync.Mutex
	cache map[cacheKey]cacheEntry
}

type cacheKey struct {
	practice uuid.UUID
	kind     string
}

type cacheEntry struct {
	updatedAt time.Time
	disp      outbox.Dispatcher
}

var _ outbox.Dispatcher = (*PracticeDispatcher)(nil)

// Send implémente outbox.Dispatcher.
func (d *PracticeDispatcher) Send(ctx context.Context, m outbox.Msg) (string, error) {
	if m.Kind != KindEmail && m.Kind != KindSMS {
		return d.Server.Send(ctx, m)
	}
	q := sqlcgen.New(d.Transports.Pool)
	row, err := q.GetNotifyTransport(ctx, sqlcgen.GetNotifyTransportParams{PracticeID: m.PracticeID, Kind: m.Kind})
	if errors.Is(err, pgx.ErrNoRows) {
		return d.Server.Send(ctx, m)
	}
	if err != nil {
		return "", fmt.Errorf("lecture du fournisseur %s: %w", m.Kind, err) // temporaire : retentée
	}
	switch row.Mode {
	case ModeServer:
		return d.Server.Send(ctx, m)
	case ModeDisabled:
		return "", outbox.Cancel(ErrChannelDisabled)
	}
	disp, err := d.custom(row)
	if err != nil {
		return "", err
	}
	return disp.Send(ctx, m)
}

func (d *PracticeDispatcher) custom(row sqlcgen.PracticeNotifyTransport) (outbox.Dispatcher, error) {
	key := cacheKey{row.PracticeID, row.Kind}
	d.mu.Lock()
	e, ok := d.cache[key]
	d.mu.Unlock()
	if ok && e.updatedAt.Equal(row.UpdatedAt) {
		return e.disp, nil
	}
	tr := d.Transports.Decode(row)
	if tr.Unreadable {
		return nil, ErrUnreadable // temporaire : retentée (le cabinet peut ressaisir ses identifiants)
	}
	disp, err := d.Transports.customDispatcher(row.Kind, tr)
	if err != nil {
		d.Transports.logger().Error("transport: configuration invalide", "practice_id", row.PracticeID, "kind", row.Kind)
		return nil, outbox.Permanent(fmt.Errorf("configuration du fournisseur invalide: %w", err))
	}
	d.mu.Lock()
	if d.cache == nil {
		d.cache = map[cacheKey]cacheEntry{}
	}
	d.cache[key] = cacheEntry{updatedAt: row.UpdatedAt, disp: disp}
	d.mu.Unlock()
	return disp, nil
}
