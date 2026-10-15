package notify

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/smtp"
	"net/textproto"
	"strconv"
	"strings"
	"time"

	"doctolibre/internal/config"
)

// Délais par défaut du client SMTP.
const (
	defaultDialTimeout = 10 * time.Second
	defaultTotalTime   = 30 * time.Second
)

// SMTPConfig configure SMTPSender.
type SMTPConfig struct {
	Host     string
	Port     int
	User     string // vide = pas d'authentification
	Password string // ne jamais journaliser
	From     string // "Nom <adresse>" ou adresse
	TLS      string // config.SMTPTLSStartTLS | SMTPTLSImplicit | SMTPTLSNone
	// Optionnels.
	HelloName   string        // nom EHLO ; défaut "localhost"
	DialTimeout time.Duration // défaut 10 s
	Timeout     time.Duration // durée totale d'un envoi ; défaut 30 s
	TLSConfig   *tls.Config   // tests : racines de confiance ; ServerName est forcé à Host
	Now         func() time.Time
}

// SMTPSender envoie via un serveur SMTP quelconque (net/smtp).
type SMTPSender struct{ cfg SMTPConfig }

var _ EmailSender = (*SMTPSender)(nil)

// NewSMTPSender valide cfg et construit l'expéditeur.
func NewSMTPSender(cfg SMTPConfig) (*SMTPSender, error) {
	if cfg.Host == "" || cfg.Port < 1 || cfg.Port > 65535 {
		return nil, errors.New("smtp: hôte ou port invalide")
	}
	switch cfg.TLS {
	case config.SMTPTLSStartTLS, config.SMTPTLSImplicit, config.SMTPTLSNone:
	default:
		return nil, fmt.Errorf("smtp: mode TLS %q inconnu", cfg.TLS)
	}
	if _, err := buildMessage(cfg.From, Email{To: "a@example.test", Text: "x"}, time.Time{}); err != nil {
		return nil, fmt.Errorf("smtp: expéditeur invalide: %w", err)
	}
	if cfg.HelloName == "" {
		cfg.HelloName = "localhost"
	}
	if cfg.DialTimeout <= 0 {
		cfg.DialTimeout = defaultDialTimeout
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = defaultTotalTime
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return &SMTPSender{cfg: cfg}, nil
}

func (s *SMTPSender) tlsConfig() *tls.Config {
	c := &tls.Config{MinVersion: tls.VersionTLS12}
	if s.cfg.TLSConfig != nil {
		c = s.cfg.TLSConfig.Clone()
		if c.MinVersion == 0 {
			c.MinVersion = tls.VersionTLS12
		}
	}
	c.ServerName = s.cfg.Host
	return c
}

// Send implémente EmailSender. Les erreurs de protocole conservent leur
// *textproto.Error (voir IsPermanentSMTP).
func (s *SMTPSender) Send(ctx context.Context, e Email) (string, error) {
	msg, err := buildMessage(s.cfg.From, e, s.cfg.Now())
	if err != nil {
		return "", err
	}
	addr := net.JoinHostPort(s.cfg.Host, strconv.Itoa(s.cfg.Port))
	d := net.Dialer{Timeout: s.cfg.DialTimeout}
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return "", fmt.Errorf("smtp: connexion: %w", err)
	}
	deadline := time.Now().Add(s.cfg.Timeout) // horloge réelle : délai réseau, pas du métier
	if dl, ok := ctx.Deadline(); ok && dl.Before(deadline) {
		deadline = dl
	}
	if err := conn.SetDeadline(deadline); err != nil {
		_ = conn.Close()
		return "", fmt.Errorf("smtp: délai: %w", err)
	}
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()

	if s.cfg.TLS == config.SMTPTLSImplicit {
		tc := tls.Client(conn, s.tlsConfig())
		if err := tc.HandshakeContext(ctx); err != nil {
			_ = conn.Close()
			return "", fmt.Errorf("smtp: TLS: %w", err)
		}
		conn = tc
	}
	c, err := smtp.NewClient(conn, s.cfg.Host)
	if err != nil {
		_ = conn.Close()
		return "", fmt.Errorf("smtp: accueil: %w", err)
	}
	defer func() { _ = c.Close() }()

	if err := c.Hello(s.cfg.HelloName); err != nil {
		return "", fmt.Errorf("smtp: EHLO: %w", err)
	}
	if s.cfg.TLS == config.SMTPTLSStartTLS {
		if ok, _ := c.Extension("STARTTLS"); !ok {
			return "", errors.New("smtp: le serveur n'annonce pas STARTTLS")
		}
		if err := c.StartTLS(s.tlsConfig()); err != nil {
			return "", fmt.Errorf("smtp: STARTTLS: %w", err)
		}
	}
	if s.cfg.User != "" {
		auth, err := s.pickAuth(c)
		if err != nil {
			return "", err
		}
		if err := c.Auth(auth); err != nil {
			return "", fmt.Errorf("smtp: authentification: %w", err)
		}
	}
	if err := c.Mail(msg.From); err != nil {
		return "", fmt.Errorf("smtp: MAIL FROM: %w", err)
	}
	if err := c.Rcpt(msg.To); err != nil {
		return "", fmt.Errorf("smtp: RCPT TO: %w", err)
	}
	w, err := c.Data()
	if err != nil {
		return "", fmt.Errorf("smtp: DATA: %w", err)
	}
	if _, err := w.Write(msg.Raw); err != nil {
		return "", fmt.Errorf("smtp: écriture: %w", err)
	}
	if err := w.Close(); err != nil {
		return "", fmt.Errorf("smtp: fin de DATA: %w", err)
	}
	_ = c.Quit() // le message est accepté ; l'échec du QUIT est sans conséquence
	return msg.MessageID, nil
}

func (s *SMTPSender) pickAuth(c *smtp.Client) (smtp.Auth, error) {
	ok, params := c.Extension("AUTH")
	if !ok {
		return nil, errors.New("smtp: le serveur n'annonce pas AUTH")
	}
	mechs := strings.Fields(strings.ToUpper(params))
	for _, m := range mechs {
		if m == "PLAIN" {
			return smtp.PlainAuth("", s.cfg.User, s.cfg.Password, s.cfg.Host), nil
		}
	}
	for _, m := range mechs {
		if m == "LOGIN" {
			return &loginAuth{user: s.cfg.User, pass: s.cfg.Password, host: s.cfg.Host}, nil
		}
	}
	return nil, errors.New("smtp: mécanisme AUTH non supporté (PLAIN ou LOGIN requis)")
}

// loginAuth implémente AUTH LOGIN (non fourni par net/smtp). Comme PlainAuth,
// refuse d'envoyer le mot de passe sans TLS, sauf vers localhost.
type loginAuth struct{ user, pass, host string }

func (a *loginAuth) Start(server *smtp.ServerInfo) (string, []byte, error) {
	if !server.TLS && !isLocalhost(server.Name) {
		return "", nil, errors.New("smtp: AUTH LOGIN refusé sur une connexion non chiffrée")
	}
	if server.Name != a.host {
		return "", nil, errors.New("smtp: nom d'hôte du serveur inattendu")
	}
	return "LOGIN", nil, nil
}

func (a *loginAuth) Next(fromServer []byte, more bool) ([]byte, error) {
	if !more {
		return nil, nil
	}
	switch strings.ToLower(strings.TrimRight(string(fromServer), ": ")) {
	case "username":
		return []byte(a.user), nil
	case "password":
		return []byte(a.pass), nil
	}
	return nil, fmt.Errorf("smtp: question AUTH LOGIN inattendue %q", fromServer)
}

func isLocalhost(name string) bool {
	return name == "localhost" || name == "127.0.0.1" || name == "::1"
}

// IsPermanentSMTP indique si err est un refus définitif du serveur (réponse 5xx),
// donc inutile à retenter. Les échecs d'authentification (530, 534, 535) sont
// exclus : c'est une erreur de configuration corrigeable, les messages doivent
// rester retentables. Les erreurs réseau et 4xx sont temporaires. Un message mal
// formé (ErrInvalidEmail) est permanent.
func IsPermanentSMTP(err error) bool {
	if errors.Is(err, ErrInvalidEmail) {
		return true
	}
	var pe *textproto.Error
	if !errors.As(err, &pe) {
		return false
	}
	switch pe.Code {
	case 530, 534, 535:
		return false
	}
	return pe.Code >= 500 && pe.Code < 600
}
