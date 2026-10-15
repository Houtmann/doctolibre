package web

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"doctolibre/internal/domain"
	"doctolibre/internal/httpx"
	"doctolibre/internal/service"
)

// CancelData est la donnée de la page public_cancel.
type CancelData struct {
	Summary Summary
	Action  string
	BackURL string
}

// RescheduleConfirmData est la donnée de la page public_reschedule_confirm.
type RescheduleConfirmData struct {
	Summary  Summary   // nouveau créneau
	Previous time.Time // créneau actuel
	Start    string
	Action   string
	BackURL  string
}

func manageURL(token string) string { return "/r/" + url.PathEscape(token) }

// noStore pose les en-têtes des pages porteuses du secret du lien.
func noStore(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
}

func summaryOf(c service.Confirmation, start time.Time) Summary {
	return Summary{
		Practice: c.Practice.Name, Practitioner: practitionerView(c.Practice.Slug, c.Practitioner).Name,
		Type: c.Type.Name, Duration: c.End.Sub(c.Start), Start: start.In(c.Loc),
	}
}

func phoneHint(c service.Confirmation) string {
	if c.Practice.Phone == "" {
		return ""
	}
	return " Appelez le cabinet au " + c.Practice.Phone + "."
}

// tooLate renvoie vers la page du rendez-vous avec le message de refus (téléphone du cabinet).
func (h *publicHandlers) tooLate(w http.ResponseWriter, r *http.Request, token string, c service.Confirmation) {
	msg := fmt.Sprintf("Ce rendez-vous ne peut plus être modifié en ligne (délai de %d h avant le rendez-vous).", int(c.MinCancel.Hours()))
	h.rd.SetFlash(w, "error", msg+phoneHint(c))
	httpx.Redirect(w, r, manageURL(token))
}

// changeable charge le rendez-vous du lien et vérifie qu'il est modifiable ;
// ok=false si la réponse (404 ou redirection) est déjà écrite.
func (h *publicHandlers) changeable(w http.ResponseWriter, r *http.Request) (c service.Confirmation, ok bool) {
	token := r.PathValue("token")
	c, err := h.svc.Confirmation(r.Context(), token)
	if err != nil {
		h.fail(w, r, err)
		return c, false
	}
	if !c.Changeable {
		h.tooLate(w, r, token, c)
		return c, false
	}
	return c, true
}

func (h *publicHandlers) cancelForm(w http.ResponseWriter, r *http.Request) {
	c, ok := h.changeable(w, r)
	if !ok {
		return
	}
	token := r.PathValue("token")
	noStore(w)
	h.rd.Page(w, r, "public_cancel", CancelData{
		Summary: summaryOf(c, c.Start), Action: manageURL(token) + "/cancel", BackURL: manageURL(token),
	})
}

func (h *publicHandlers) cancelSubmit(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	c, err := h.svc.Confirmation(r.Context(), token)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	switch err := h.svc.CancelByPatient(r.Context(), token); {
	case err == nil:
		httpx.LoggerFrom(r.Context()).Info("rendez-vous annulé par le patient", slog.String("appointment_id", c.Appointment.ID.String()))
		h.rd.SetFlash(w, "success", "Votre rendez-vous est annulé.")
		httpx.Redirect(w, r, manageURL(token))
	case errors.Is(err, service.ErrChangeTooLate):
		h.tooLate(w, r, token, c)
	default:
		h.fail(w, r, err)
	}
}

func (h *publicHandlers) rescheduleGrid(w http.ResponseWriter, r *http.Request) {
	c, ok := h.changeable(w, r)
	if !ok {
		return
	}
	token := r.PathValue("token")
	base := manageURL(token)
	now := h.svc.Now()
	minMon, maxMon := mondayOf(now, c.Loc), mondayOf(now.Add(c.Horizon), c.Loc)
	mon, okWeek := parseWeek(r.URL.Query().Get("week"), c.Loc)
	if !okWeek || mon.Before(minMon) {
		mon = minMon
	}
	if mon.After(maxMon) {
		mon = maxMon
	}
	next := mon.AddDate(0, 0, 7)
	slots, err := h.svc.RescheduleSlots(r.Context(), token, mon, next)
	switch {
	case errors.Is(err, service.ErrChangeTooLate):
		h.tooLate(w, r, token, c)
		return
	case errors.Is(err, domain.ErrNotFound):
		// Motif retiré de la réservation en ligne : seul le cabinet peut déplacer.
		h.rd.SetFlash(w, "error", "Ce rendez-vous ne peut pas être déplacé en ligne."+phoneHint(c))
		httpx.Redirect(w, r, base)
		return
	case err != nil:
		h.fail(w, r, err)
		return
	}
	weekURL := func(week time.Time) string {
		return base + "/reschedule?" + url.Values{"week": {isoWeek(week)}}.Encode()
	}
	data := GridData{
		Practice: c.Practice.Name, Practitioner: practitionerView(c.Practice.Slug, c.Practitioner).Name,
		Type:      TypeView{ID: c.Type.ID.String(), Name: c.Type.Name, Duration: c.End.Sub(c.Start)},
		WeekLabel: fmt.Sprintf("du %s au %s", frenchDate(mon), frenchDate(mon.AddDate(0, 0, 6))),
		BackURL:   base,
	}
	if mon.After(minMon) {
		data.PrevURL = weekURL(mon.AddDate(0, 0, -7))
	}
	if next.Before(maxMon.AddDate(0, 0, 7)) {
		data.NextURL = weekURL(next)
	}
	for i := range 7 {
		day := mon.AddDate(0, 0, i)
		gd := GridDay{Label: frenchDate(day)}
		for _, s := range slots {
			st := s.Start.In(c.Loc)
			if st.Year() != day.Year() || st.YearDay() != day.YearDay() {
				continue
			}
			gd.Slots = append(gd.Slots, GridSlot{
				Time: st, Datetime: st.Format(time.RFC3339),
				URL: base + "/reschedule/confirm?" + url.Values{"start": {s.Start.UTC().Format(time.RFC3339)}}.Encode(),
			})
		}
		if len(gd.Slots) > 0 {
			data.Days = append(data.Days, gd)
		}
	}
	noStore(w)
	w.Header().Add("Vary", "HX-Request")
	if httpx.IsHTMX(r) {
		h.rd.Partial(w, r, "booking_grid", data)
		return
	}
	h.rd.Page(w, r, "public_reschedule", data)
}

func (h *publicHandlers) rescheduleConfirm(w http.ResponseWriter, r *http.Request) {
	c, ok := h.changeable(w, r)
	if !ok {
		return
	}
	token := r.PathValue("token")
	start, err := time.Parse(time.RFC3339, r.URL.Query().Get("start"))
	if err != nil {
		httpx.Redirect(w, r, manageURL(token)+"/reschedule")
		return
	}
	start = start.UTC()
	slots, err := h.svc.RescheduleSlots(r.Context(), token, start, start.Add(time.Second))
	if err != nil {
		h.rescheduleError(w, r, token, c, err)
		return
	}
	found := false
	for _, s := range slots {
		found = found || s.Start.Equal(start)
	}
	if !found {
		h.rd.SetFlash(w, "error", "Ce créneau n'est plus disponible. Choisissez-en un autre.")
		httpx.Redirect(w, r, manageURL(token)+"/reschedule")
		return
	}
	noStore(w)
	h.rd.Page(w, r, "public_reschedule_confirm", RescheduleConfirmData{
		Summary: summaryOf(c, start), Previous: c.Start, Start: start.Format(time.RFC3339),
		Action: manageURL(token) + "/reschedule", BackURL: manageURL(token) + "/reschedule",
	})
}

// rescheduleError traduit les erreurs de déplacement en redirection avec message.
func (h *publicHandlers) rescheduleError(w http.ResponseWriter, r *http.Request, token string, c service.Confirmation, err error) {
	switch {
	case errors.Is(err, service.ErrChangeTooLate):
		h.tooLate(w, r, token, c)
	case errors.Is(err, domain.ErrSlotTaken):
		h.rd.SetFlash(w, "error", "Ce créneau vient d'être pris. Votre rendez-vous actuel est conservé : choisissez un autre créneau.")
		httpx.Redirect(w, r, manageURL(token)+"/reschedule")
	default:
		h.fail(w, r, err)
	}
}

func (h *publicHandlers) rescheduleSubmit(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	c, err := h.svc.Confirmation(r.Context(), token)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	start, perr := time.Parse(time.RFC3339, r.PostFormValue("start"))
	if perr != nil {
		httpx.Redirect(w, r, manageURL(token)+"/reschedule")
		return
	}
	if err := h.svc.RescheduleByPatient(r.Context(), token, start.UTC()); err != nil {
		h.rescheduleError(w, r, token, c, err)
		return
	}
	httpx.LoggerFrom(r.Context()).Info("rendez-vous déplacé par le patient", slog.String("appointment_id", c.Appointment.ID.String()))
	h.rd.SetFlash(w, "success", "Votre rendez-vous est déplacé.")
	httpx.Redirect(w, r, manageURL(token))
}
