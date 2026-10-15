package web

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"doctolibre/internal/auth"
	"doctolibre/internal/domain"
	"doctolibre/internal/httpx"
	"doctolibre/internal/store/sqlcgen"
)

// recentMessages : nombre de messages affichés sur /admin/messages.
const recentMessages = 50

// MessageQueue est la partie de l'outbox utilisée par la page admin (*outbox.Queue).
type MessageQueue interface {
	ListRecent(ctx context.Context, practiceID uuid.UUID, n int32) ([]sqlcgen.OutboxMessage, error)
	Resend(ctx context.Context, practiceID, id uuid.UUID) error
}

// MessageRow est une ligne de la page /admin/messages (destinataire masqué).
type MessageRow struct {
	ID        uuid.UUID
	Kind      string
	Template  string
	Status    string
	Recipient string // masqué
	Attempts  int32
	LastError string // destinataire masqué aussi
	CreatedAt time.Time
	CanResend bool
}

// MessagesData est la donnée de la page /admin/messages.
type MessagesData struct{ Messages []MessageRow }

func (d Deps) messagesRoutes(mux *http.ServeMux, rd *Renderer, require httpx.Middleware) {
	if d.Messages == nil {
		return
	}
	h := &messagesHandlers{q: d.Messages, rd: rd}
	mux.Handle("GET /admin/messages", require(http.HandlerFunc(h.list)))
	mux.Handle("POST /admin/messages/{id}/resend", require(http.HandlerFunc(h.resend)))
}

type messagesHandlers struct {
	q  MessageQueue
	rd *Renderer
}

func (h *messagesHandlers) list(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.UserFrom(r.Context())
	rows, err := h.q.ListRecent(r.Context(), u.PracticeID, recentMessages)
	if err != nil {
		httpx.LoggerFrom(r.Context()).Error("messages: lecture en échec", slog.String("err", err.Error()))
		h.rd.Error(w, r, http.StatusInternalServerError, "")
		return
	}
	data := MessagesData{Messages: make([]MessageRow, 0, len(rows))}
	for _, m := range rows {
		masked := maskRecipient(m.Recipient)
		row := MessageRow{
			ID: m.ID, Kind: m.Kind, Template: m.Template, Status: m.Status,
			Recipient: masked, Attempts: m.Attempts, CreatedAt: m.CreatedAt,
			CanResend: m.Status == "failed" || m.Status == "cancelled",
		}
		if m.LastError.Valid {
			row.LastError = strings.ReplaceAll(m.LastError.String, m.Recipient, masked)
		}
		data.Messages = append(data.Messages, row)
	}
	h.rd.Page(w, r, "admin_messages", data)
}

func (h *messagesHandlers) resend(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.UserFrom(r.Context())
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		h.rd.Error(w, r, http.StatusNotFound, "")
		return
	}
	switch err := h.q.Resend(r.Context(), u.PracticeID, id); {
	case errors.Is(err, domain.ErrNotFound):
		h.rd.Error(w, r, http.StatusNotFound, "")
		return
	case err != nil:
		httpx.LoggerFrom(r.Context()).Error("messages: renvoi en échec",
			slog.String("message_id", id.String()), slog.String("err", err.Error()))
		h.rd.Error(w, r, http.StatusInternalServerError, "")
		return
	}
	h.rd.SetFlash(w, "success", "Message remis en file d'envoi.")
	httpx.Redirect(w, r, "/admin/messages")
}

const maskChar = "•"

// maskRecipient masque un destinataire pour l'affichage : « 06•••••12 »
// (téléphone), « j•••@exemple.fr » (email). Les valeurs trop courtes sont
// entièrement masquées.
func maskRecipient(s string) string {
	s = strings.TrimSpace(s)
	if at := strings.LastIndex(s, "@"); at >= 0 {
		local, domainPart := []rune(s[:at]), s[at:]
		if len(local) < 2 {
			return strings.Repeat(maskChar, 3) + domainPart
		}
		return string(local[:1]) + strings.Repeat(maskChar, 3) + domainPart
	}
	rs := []rune(s)
	head := 2
	if strings.HasPrefix(s, "+") {
		head = 3 // « + » et deux chiffres d'indicatif
	}
	const tail = 2
	if len(rs) < head+tail+3 {
		return strings.Repeat(maskChar, 3)
	}
	return string(rs[:head]) + strings.Repeat(maskChar, len(rs)-head-tail) + string(rs[len(rs)-tail:])
}
