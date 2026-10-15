package config

import (
	"strings"
	"testing"
)

func TestLoadFromEmail(t *testing.T) {
	t.Parallel()
	c, err := LoadFrom(env(nil))
	if err != nil {
		t.Fatal(err)
	}
	if c.Email.Driver != EmailDriverLog || c.Email.SMTPPort != 587 || c.Email.SMTPTLS != SMTPTLSStartTLS {
		t.Fatalf("défauts email inattendus: %+v", c.Email)
	}
	c, err = LoadFrom(env(map[string]string{
		"DOCTOLIBRE_EMAIL_DRIVER": "smtp", "DOCTOLIBRE_SMTP_HOST": "smtp.example.test",
		"DOCTOLIBRE_SMTP_FROM": "Cabinet <rdv@example.test>", "DOCTOLIBRE_SMTP_TLS": "tls",
	}))
	if err != nil || c.Email.SMTPPort != 465 {
		t.Fatalf("smtp tls: %+v %v", c.Email, err)
	}
	bad := []struct {
		name string
		env  map[string]string
		want string
	}{
		{"driver", map[string]string{"DOCTOLIBRE_EMAIL_DRIVER": "pigeon"}, "DOCTOLIBRE_EMAIL_DRIVER"},
		{"host manquant", map[string]string{"DOCTOLIBRE_EMAIL_DRIVER": "smtp", "DOCTOLIBRE_SMTP_FROM": "a@b.fr"}, "DOCTOLIBRE_SMTP_HOST"},
		{"from manquant", map[string]string{"DOCTOLIBRE_EMAIL_DRIVER": "smtp", "DOCTOLIBRE_SMTP_HOST": "h"}, "DOCTOLIBRE_SMTP_FROM"},
		{"port", map[string]string{"DOCTOLIBRE_SMTP_PORT": "abc"}, "DOCTOLIBRE_SMTP_PORT"},
		{"tls", map[string]string{"DOCTOLIBRE_SMTP_TLS": "ssl"}, "DOCTOLIBRE_SMTP_TLS"},
		{"user sans mot de passe", map[string]string{
			"DOCTOLIBRE_EMAIL_DRIVER": "smtp", "DOCTOLIBRE_SMTP_HOST": "h", "DOCTOLIBRE_SMTP_FROM": "a@b.fr", "DOCTOLIBRE_SMTP_USER": "u",
		}, "vont ensemble"},
	}
	for _, tt := range bad {
		if _, err := LoadFrom(env(tt.env)); err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%s: erreur attendue contenant %q, obtenu %v", tt.name, tt.want, err)
		}
	}
}
