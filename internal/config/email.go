package config

import (
	"errors"
	"fmt"
	"net/mail"
	"strconv"
)

// Drivers d'envoi d'email.
const (
	EmailDriverSMTP = "smtp"
	EmailDriverLog  = "log"
)

// Modes TLS du client SMTP.
const (
	SMTPTLSStartTLS = "starttls"
	SMTPTLSImplicit = "tls"
	SMTPTLSNone     = "none"
)

// EmailConfig regroupe la configuration d'envoi des emails.
type EmailConfig struct {
	Driver       string // smtp | log
	SMTPHost     string
	SMTPPort     int
	SMTPUser     string
	SMTPPassword string // ne jamais journaliser
	SMTPFrom     string // ex. "Cabinet Dupont <rdv@exemple.fr>"
	SMTPTLS      string // starttls | tls | none
	LogDir       string // driver log : dossier des .eml
}

func loadEmail(get func(key, def string) string) (EmailConfig, error) {
	e := EmailConfig{
		Driver:       get("DOCTOLIBRE_EMAIL_DRIVER", EmailDriverLog),
		SMTPHost:     get("DOCTOLIBRE_SMTP_HOST", ""),
		SMTPUser:     get("DOCTOLIBRE_SMTP_USER", ""),
		SMTPPassword: get("DOCTOLIBRE_SMTP_PASSWORD", ""),
		SMTPFrom:     get("DOCTOLIBRE_SMTP_FROM", ""),
		SMTPTLS:      get("DOCTOLIBRE_SMTP_TLS", SMTPTLSStartTLS),
		LogDir:       get("DOCTOLIBRE_EMAIL_LOG_DIR", "tmp/mails"),
	}
	port := get("DOCTOLIBRE_SMTP_PORT", "")
	if port == "" {
		switch e.SMTPTLS {
		case SMTPTLSImplicit:
			e.SMTPPort = 465
		case SMTPTLSNone:
			e.SMTPPort = 25
		default:
			e.SMTPPort = 587
		}
		return e, nil
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		return e, fmt.Errorf("DOCTOLIBRE_SMTP_PORT: %q invalide", port)
	}
	e.SMTPPort = n
	return e, nil
}

// Validate vérifie la configuration email (variables d'environnement ou réglage d'un cabinet, T086).
func (e EmailConfig) Validate() error {
	var errs []error
	switch e.Driver {
	case EmailDriverLog:
	case EmailDriverSMTP:
		if e.SMTPHost == "" {
			errs = append(errs, errors.New("DOCTOLIBRE_SMTP_HOST: obligatoire avec le driver smtp"))
		}
		if e.SMTPFrom == "" {
			errs = append(errs, errors.New("DOCTOLIBRE_SMTP_FROM: obligatoire avec le driver smtp"))
		} else if _, err := mail.ParseAddress(e.SMTPFrom); err != nil {
			errs = append(errs, errors.New("DOCTOLIBRE_SMTP_FROM: adresse invalide"))
		}
		if (e.SMTPUser == "") != (e.SMTPPassword == "") {
			errs = append(errs, errors.New("DOCTOLIBRE_SMTP_USER et DOCTOLIBRE_SMTP_PASSWORD vont ensemble"))
		}
	default:
		errs = append(errs, fmt.Errorf("DOCTOLIBRE_EMAIL_DRIVER: %q invalide (smtp|log)", e.Driver))
	}
	switch e.SMTPTLS {
	case SMTPTLSStartTLS, SMTPTLSImplicit, SMTPTLSNone:
	default:
		errs = append(errs, fmt.Errorf("DOCTOLIBRE_SMTP_TLS: %q invalide (starttls|tls|none)", e.SMTPTLS))
	}
	return errors.Join(errs...)
}
