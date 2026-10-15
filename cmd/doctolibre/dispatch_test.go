package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"log/slog"

	"github.com/google/uuid"

	"doctolibre/internal/config"
	"doctolibre/internal/outbox"
)

func TestBuildDispatcherLogDrivers(t *testing.T) {
	t.Parallel()
	var sb strings.Builder
	logger := slog.New(slog.NewTextHandler(&sb, nil))
	cfg := config.Config{
		Env:   config.EnvProd,
		Email: config.EmailConfig{Driver: config.EmailDriverLog, LogDir: t.TempDir()},
		SMS:   config.SMSConfig{Driver: config.SMSDriverLog},
	}
	d, err := buildDispatcher(cfg, logger)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sb.String(), "aucun email réel") || !strings.Contains(sb.String(), "aucun SMS réel") {
		t.Errorf("avertissements prod absents: %s", sb.String())
	}
	ctx := context.Background()
	payload := json.RawMessage(`{"reset_url":"https://x.test/r?token=secret","valid_for":"1 heure"}`)
	if _, err := d.Send(ctx, outbox.Msg{ID: uuid.New(), Kind: outbox.KindEmail, Template: "password_reset", Payload: payload, Recipient: "jean.dupont@example.fr"}); err != nil {
		t.Fatalf("email: %v", err)
	}
	sms := outbox.Msg{ID: uuid.New(), Kind: outbox.KindSMS, Payload: json.RawMessage(`{"body":"Bonjour Jean"}`), Recipient: "+33612345678"}
	if _, err := d.Send(ctx, sms); err != nil {
		t.Fatalf("sms: %v", err)
	}
	out := sb.String()
	for _, pii := range []string{"jean.dupont", "+3361", "Jean", "secret"} {
		if strings.Contains(out, pii) {
			t.Errorf("PII %q dans les logs: %s", pii, out)
		}
	}
}

func TestBuildDispatcherInvalidConfig(t *testing.T) {
	t.Parallel()
	cfg := config.Config{Email: config.EmailConfig{Driver: "smtp"}} // SMTP_HOST manquant
	if _, err := buildDispatcher(cfg, discardLogger()); err == nil {
		t.Error("config email invalide acceptée")
	}
	cfg = config.Config{Email: config.EmailConfig{Driver: config.EmailDriverLog, LogDir: t.TempDir()}, SMS: config.SMSConfig{Driver: "ovh"}}
	if _, err := buildDispatcher(cfg, discardLogger()); err == nil {
		t.Error("config SMS invalide acceptée")
	}
}
