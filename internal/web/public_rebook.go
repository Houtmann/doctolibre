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

// RebookConfirmData est la donnée de la page public_rebook_confirm (T098).
type RebookConfirmData struct {
	Summary Summary
	Start   string
	Action  string
	BackURL string
	// Instructions : consignes du motif ; AckRequired : case « J'ai lu » obligatoire.
	Instructions string
	AckRequired  bool
}

// RebookClosedData est la donnée de la page public_rebook_closed.
type RebookClosedData struct {
	AlreadyDone bool
	Phone       string
	TelHref     string
	PageURL     string // page du praticien
}

func rebookURL(token string) string { return manageURL(token) + "/rebook" }

// rebookClosed répond 403 par une page explicative avec retour vers la page du praticien.
func (h *publicHandlers) rebookClosed(w http.ResponseWriter, r *http.Request, c service.Confirmation, already bool) {
	w.Header().Set("Cache-Control", "no-store")
	h.rd.PageStatus(w, r, http.StatusForbidden, "public_rebook_closed", RebookClosedData{
		AlreadyDone: already, Phone: c.Practice.Phone, TelHref: telHref(c.Practice.Phone),
		PageURL: practitionerView(c.Practice.Slug, c.Practitioner).URL,
	})
}

// rebookError traduit les erreurs de la reprise en réponse HTTP ; ok=false si la réponse est écrite.
func (h *publicHandlers) rebookError(w http.ResponseWriter, r *http.Request, token string, c service.Confirmation, err error) {
	switch {
	case errors.Is(err, domain.ErrOnlineBookingBlocked):
		httpx.LoggerFrom(r.Context()).Info("reprise en ligne refusée (absences répétées)", slog.String("appointment_id", c.Appointment.ID.String()))
		renderBookingBlocked(h.rd, w, r, c.Practice.Phone, practitionerView(c.Practice.Slug, c.Practitioner).URL, "Retour à la page du praticien")
	case errors.Is(err, service.ErrRebookClosed):
		h.rebookClosed(w, r, c, false)
	case errors.Is(err, service.ErrAlreadyRebooked):
		h.rebookClosed(w, r, c, true)
	case errors.Is(err, domain.ErrSlotTaken):
		h.rd.SetFlash(w, "error", "Ce créneau vient d'être pris. Choisissez-en un autre.")
		httpx.Redirect(w, r, rebookURL(token))
	default:
		h.fail(w, r, err)
	}
}

func (h *publicHandlers) rebookGrid(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	c, err := h.svc.Confirmation(r.Context(), token)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	base := rebookURL(token)
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
	slots, err := h.svc.RebookSlots(r.Context(), token, mon, next)
	if err != nil {
		h.rebookError(w, r, token, c, err)
		return
	}
	weekURL := func(week time.Time) string {
		return base + "?" + url.Values{"week": {isoWeek(week)}}.Encode()
	}
	data := GridData{
		Practice: c.Practice.Name, Practitioner: practitionerView(c.Practice.Slug, c.Practitioner).Name,
		Type:      TypeView{ID: c.Type.ID.String(), Name: c.Type.Name, Duration: c.End.Sub(c.Start)},
		WeekLabel: fmt.Sprintf("du %s au %s", frenchDate(mon), frenchDate(mon.AddDate(0, 0, 6))),
		BackURL:   manageURL(token),
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
				URL: base + "/confirm?" + url.Values{"start": {s.Start.UTC().Format(time.RFC3339)}}.Encode(),
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
	h.rd.Page(w, r, "public_rebook", data)
}

func (h *publicHandlers) rebookConfirm(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	c, err := h.svc.Confirmation(r.Context(), token)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	start, err := time.Parse(time.RFC3339, r.URL.Query().Get("start"))
	if err != nil {
		httpx.Redirect(w, r, rebookURL(token))
		return
	}
	start = start.UTC()
	slots, err := h.svc.RebookSlots(r.Context(), token, start, start.Add(time.Second))
	if err != nil {
		h.rebookError(w, r, token, c, err)
		return
	}
	found := false
	for _, s := range slots {
		found = found || s.Start.Equal(start)
	}
	if !found {
		h.rd.SetFlash(w, "error", "Ce créneau n'est plus disponible. Choisissez-en un autre.")
		httpx.Redirect(w, r, rebookURL(token))
		return
	}
	instr, ack := instructionsOf(c.Type)
	noStore(w)
	h.rd.Page(w, r, "public_rebook_confirm", RebookConfirmData{
		Summary: summaryOf(c, start), Start: start.Format(time.RFC3339), Action: rebookURL(token),
		BackURL:      rebookURL(token) + "?" + url.Values{"week": {isoWeek(mondayOf(start.In(c.Loc), c.Loc))}}.Encode(),
		Instructions: instr, AckRequired: ack,
	})
}

func (h *publicHandlers) rebookSubmit(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	c, err := h.svc.Confirmation(r.Context(), token)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	start, perr := time.Parse(time.RFC3339, r.PostFormValue("start"))
	if perr != nil {
		httpx.Redirect(w, r, rebookURL(token))
		return
	}
	quota, ok := h.reservePractitioner(w, r, service.Target{Practice: c.Practice, Practitioner: c.Practitioner}, func(msg string) {
		h.rd.SetFlash(w, "error", msg)
		httpx.Redirect(w, r, manageURL(token))
	})
	if !ok {
		return
	}
	res, err := h.svc.RebookByPatient(r.Context(), token, start.UTC(), r.PostFormValue("instructions_ack") != "")
	if err != nil {
		quota.Cancel()
		var ve domain.ErrValidation
		if errors.As(err, &ve) {
			h.rd.SetFlash(w, "error", ve.Msg)
			httpx.Redirect(w, r, rebookURL(token)+"/confirm?"+url.Values{"start": {start.UTC().Format(time.RFC3339)}}.Encode())
			return
		}
		h.rebookError(w, r, token, c, err)
		return
	}
	httpx.LoggerFrom(r.Context()).Info("rendez-vous repris par le patient",
		slog.String("appointment_id", res.Appointment.ID.String()), slog.String("previous_appointment_id", c.Appointment.ID.String()))
	h.rd.SetFlash(w, "success", "Votre nouveau rendez-vous est confirmé.")
	httpx.Redirect(w, r, manageURL(res.Token))
}
