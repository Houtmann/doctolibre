// Package logsms est le driver de développement : il ne sort rien du process
// et journalise un envoi fictif (numéro masqué, segments ; jamais le corps,
// qui peut contenir un code OTP ou un lien de gestion).
package logsms

import (
	"context"
	"log/slog"

	"doctolibre/internal/notify/sms"
)

// Sender journalise les SMS.
type Sender struct{ Logger *slog.Logger }

// Send implémente sms.Sender.
func (s Sender) Send(ctx context.Context, m sms.SMS) (string, error) {
	n, ucs2 := sms.Segments(m.Body)
	l := s.Logger
	if l == nil {
		l = slog.Default()
	}
	l.InfoContext(ctx, "sms (driver log, non envoyé)", "to", sms.MaskPhone(m.To), "segments", n, "ucs2", ucs2, "chars", len([]rune(m.Body)))
	return "log", nil
}
