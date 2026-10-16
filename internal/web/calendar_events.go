package web

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"doctolibre/internal/domain"
	"doctolibre/internal/httpx"
	"doctolibre/internal/service"
)

// eventJSON est un événement au format EventCalendar. Aucun téléphone, email ni
// note : le titre est celui que l'agenda a toujours affiché (« Nom Prénom »).
type eventJSON struct {
	ID            string         `json:"id"`
	ResourceID    string         `json:"resourceId"`
	Start         string         `json:"start"`
	End           string         `json:"end"`
	Title         string         `json:"title,omitempty"`
	Display       string         `json:"display,omitempty"`
	ClassNames    []string       `json:"classNames,omitempty"`
	ExtendedProps map[string]any `json:"extendedProps"`
}

// apptClass : classe CSS (voir input.css) du statut d'un RDV.
func apptClass(status string) string {
	switch status {
	case "done":
		return "appt-done"
	case "no_show":
		return "appt-noshow"
	case "cancelled":
		return "appt-cancelled"
	}
	return "appt-booked"
}

// jsonError répond une erreur en JSON (l'endpoint est appelé par fetch, pas par un navigateur).
func jsonError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

// events sert GET /admin/agenda/events?start=&end=&practitioner= : les RDV non
// annulés et les plages d'indisponibilité du cabinet de la session. Les
// praticiens demandés sont contrôlés comme pour la page (403 si l'agenda d'un
// confrère est interdit, 404 s'il est d'un autre cabinet).
func (h *calendarHandlers) events(w http.ResponseWriter, r *http.Request) {
	begin := time.Now()
	sel, ok := selectionFrom(r)
	if !ok {
		jsonError(w, http.StatusBadRequest, "Praticien invalide.")
		return
	}
	q := r.URL.Query()
	res, err := h.svc.AgendaRange(r.Context(), actorFrom(r), service.EventsInput{
		Start: q.Get("start"), End: q.Get("end"), Selection: service.Selection{IDs: sel.ids, All: sel.all},
	})
	switch {
	case err == nil:
	case validation(err) != nil:
		jsonError(w, http.StatusBadRequest, validation(err)[0])
		return
	case errors.Is(err, domain.ErrForbidden):
		jsonError(w, http.StatusForbidden, "Accès refusé.")
		return
	case errors.Is(err, domain.ErrNotFound):
		jsonError(w, http.StatusNotFound, "Praticien introuvable.")
		return
	default:
		httpx.LoggerFrom(r.Context()).Error("événements de l'agenda en échec",
			slog.String("user_id", actorFrom(r).UserID.String()), slog.String("err", err.Error()))
		jsonError(w, http.StatusInternalServerError, "Erreur interne.")
		return
	}
	out := make([]eventJSON, 0, len(res.Appts)+len(res.Closed))
	for _, a := range res.Appts {
		out = append(out, eventJSON{
			ID: a.ID.String(), ResourceID: a.PractitionerID.String(),
			Start: a.Start.Format(time.RFC3339), End: a.End.Format(time.RFC3339),
			Title: a.Patient, ClassNames: []string{apptClass(a.Status)},
			ExtendedProps: map[string]any{
				"status": a.Status, "status_label": a.StatusName, "type": a.TypeName, "home": a.AtHome,
				"blocked_start": a.BlockedStart.Format(time.RFC3339), "blocked_end": a.BlockedEnd.Format(time.RFC3339),
			},
		})
	}
	for i, c := range res.Closed {
		out = append(out, eventJSON{
			ID: "closed-" + strconv.Itoa(i), ResourceID: c.PractitionerID.String(),
			Start: c.Start.Format(time.RFC3339), End: c.End.Format(time.RFC3339),
			Display: "background", ClassNames: []string{"agenda-closed"},
			ExtendedProps: map[string]any{"kind": "closed"},
		})
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(out)
	httpx.LoggerFrom(r.Context()).Info("événements de l'agenda",
		slog.Int("appointments", len(res.Appts)), slog.Int("closed", len(res.Closed)),
		slog.Int64("duration_ms", time.Since(begin).Milliseconds()))
}
