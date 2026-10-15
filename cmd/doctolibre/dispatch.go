package main

import (
	"context"
	"fmt"
	"log/slog"

	"doctolibre/internal/outbox"
)

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
