package main

import (
	"context"
	"fmt"
	"log/slog"

	"doctolibre/internal/config"
	"doctolibre/internal/domain"
	"doctolibre/internal/notify"
	"doctolibre/internal/outbox"
)

// buildDispatcher construit les senders email et SMS depuis la configuration
// et les route par kind. Une config de driver invalide est une erreur de démarrage.
func buildDispatcher(cfg config.Config, logger *slog.Logger) (outbox.Dispatcher, error) {
	emailSender, err := notify.NewEmailSender(cfg.Email, logger)
	if err != nil {
		return nil, fmt.Errorf("email: %w", err)
	}
	email, err := notify.NewEmailDispatcher(emailSender)
	if err != nil {
		return nil, fmt.Errorf("email: %w", err)
	}
	smsSender, err := notify.NewSMSSender(cfg.SMS, domain.SystemClock{}, logger)
	if err != nil {
		return nil, fmt.Errorf("sms: %w", err)
	}
	sms := notify.SMSDispatch(smsSender, cfg.SMS.Sender, cfg.SMS.Transliterate)
	warnSimulatedDrivers(cfg, logger)
	return newDispatcher(email, sms, logger), nil
}

// warnSimulatedDrivers avertit (sans bloquer) qu'en production un driver « log » n'envoie rien.
func warnSimulatedDrivers(cfg config.Config, logger *slog.Logger) {
	if !cfg.IsProd() {
		return
	}
	if cfg.Email.Driver == config.EmailDriverLog {
		logger.Warn("DOCTOLIBRE_EMAIL_DRIVER=log en production : aucun email réel ne sera envoyé")
	}
	if cfg.SMS.Driver == "" || cfg.SMS.Driver == config.SMSDriverLog {
		logger.Warn("DOCTOLIBRE_SMS_DRIVER=log en production : aucun SMS réel ne sera envoyé")
	}
}

// newDispatcher route un message vers le dispatcher de son kind. Un dispatcher
// nil est remplacé par un dispatcher « log » (ni destinataire ni contenu).
func newDispatcher(email, sms outbox.Dispatcher, logger *slog.Logger) outbox.Dispatcher {
	if email == nil {
		email = logDispatcher(logger)
	}
	if sms == nil {
		sms = logDispatcher(logger)
	}
	return outbox.DispatcherFunc(func(ctx context.Context, m outbox.Msg) (string, error) {
		switch m.Kind {
		case outbox.KindEmail:
			return email.Send(ctx, m)
		case outbox.KindSMS:
			return sms.Send(ctx, m)
		default:
			return "", outbox.Permanent(fmt.Errorf("kind %q non géré", m.Kind))
		}
	})
}

// logDispatcher ne fait qu'enregistrer l'ID et le kind (aucune donnée personnelle).
func logDispatcher(logger *slog.Logger) outbox.Dispatcher {
	return outbox.DispatcherFunc(func(_ context.Context, m outbox.Msg) (string, error) {
		logger.Info("outbox: envoi simulé (aucun driver configuré)", "message_id", m.ID, "kind", m.Kind)
		return "", nil
	})
}
