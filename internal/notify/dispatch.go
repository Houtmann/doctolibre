package notify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"doctolibre/internal/outbox"
)

// NewEmailDispatcher adapte un EmailSender en outbox.DispatcherFunc pour les
// messages kind='email' : rend le template Msg.Template avec le payload JSON
// comme données, destinataire = Msg.Recipient. Un template inconnu, un payload
// invalide, un message mal formé ou un refus SMTP 5xx sont permanents ; les
// erreurs réseau et 4xx sont temporaires (retentées par le worker).
//
// Branchement (T024) : outbox.Worker{Dispatcher: <mux par kind>} avec cette
// fonction pour outbox.KindEmail.
func NewEmailDispatcher(s EmailSender) (outbox.DispatcherFunc, error) {
	tpl, err := NewTemplates()
	if err != nil {
		return nil, err
	}
	return func(ctx context.Context, m outbox.Msg) (string, error) {
		if m.Kind != outbox.KindEmail {
			return "", outbox.Permanent(fmt.Errorf("kind %q non géré par le dispatcher email", m.Kind))
		}
		var data map[string]any
		if len(m.Payload) > 0 {
			if err := json.Unmarshal(m.Payload, &data); err != nil {
				return "", outbox.Permanent(fmt.Errorf("payload email illisible: %w", err))
			}
		}
		e, err := tpl.Render(m.Template, data)
		if err != nil {
			return "", outbox.Permanent(err)
		}
		e.To = m.Recipient
		ref, err := s.Send(ctx, e)
		if err != nil {
			if IsPermanentSMTP(err) || errors.Is(err, ErrUnknownTemplate) {
				return "", outbox.Permanent(err)
			}
			return "", err
		}
		return ref, nil
	}, nil
}
