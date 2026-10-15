// Package notify envoie les notifications (email ici ; SMS dans d'autres fichiers).
// Les destinataires et contenus sont des données personnelles : ne jamais les
// journaliser.
package notify

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"doctolibre/internal/config"
)

// Email est un message prêt à envoyer. From n'en fait pas partie : c'est un
// réglage de l'expéditeur (DOCTOLIBRE_SMTP_FROM).
type Email struct {
	To      string // adresse seule (ou "Nom <adresse>")
	Subject string
	Text    string // obligatoire
	HTML    string // optionnel : si présent, multipart/alternative
	ReplyTo string
	// Headers : en-têtes additionnels (ex. List-Unsubscribe). Les valeurs ne
	// doivent pas contenir de saut de ligne.
	Headers map[string]string
}

// EmailSender envoie un email et retourne une référence (Message-ID).
type EmailSender interface {
	Send(ctx context.Context, e Email) (ref string, err error)
}

// ErrInvalidEmail : l'email est mal formé (adresse, en-tête avec saut de ligne,
// corps vide). Jamais retentable.
var ErrInvalidEmail = errors.New("email invalide")

// NewEmailSender construit l'expéditeur selon cfg.Driver.
func NewEmailSender(cfg config.EmailConfig, logger *slog.Logger) (EmailSender, error) {
	if logger == nil {
		logger = slog.Default()
	}
	switch cfg.Driver {
	case config.EmailDriverSMTP:
		return NewSMTPSender(SMTPConfig{
			Host: cfg.SMTPHost, Port: cfg.SMTPPort, User: cfg.SMTPUser, Password: cfg.SMTPPassword,
			From: cfg.SMTPFrom, TLS: cfg.SMTPTLS,
		})
	case config.EmailDriverLog:
		from := cfg.SMTPFrom
		if from == "" {
			from = "Doctolibre <noreply@localhost>"
		}
		return NewLogSender(from, cfg.LogDir, logger)
	default:
		return nil, fmt.Errorf("driver email %q inconnu", cfg.Driver)
	}
}
