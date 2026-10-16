package web

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"doctolibre/internal/domain"
	"doctolibre/internal/httpx"
	"doctolibre/internal/service"
	"doctolibre/internal/store/sqlcgen"
)

const pathWaitlist = "/admin/waitlist"

// waitlistAdminRoutes enregistre l'écran du cabinet : inscriptions, ajout, retrait.
func (d Deps) waitlistAdminRoutes(mux *http.ServeMux, rd *Renderer, logger *slog.Logger) {
	h := &waitlistAdminHandlers{svc: d.Waitlist, rd: rd, logger: logger}
	require := d.Auth.RequireUser()
	mux.Handle("GET "+pathWaitlist, require(http.HandlerFunc(h.list)))
	mux.Handle("POST "+pathWaitlist, require(http.HandlerFunc(h.add)))
	mux.Handle("POST "+pathWaitlist+"/{id}/cancel", require(http.HandlerFunc(h.cancel)))
}

type waitlistAdminHandlers struct {
	svc    *service.Waitlist
	rd     *Renderer
	logger *slog.Logger
}

func (h *waitlistAdminHandlers) fail(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, domain.ErrNotFound):
		h.rd.Error(w, r, http.StatusNotFound, "")
	default:
		a := actorFrom(r)
		httpx.LoggerFrom(r.Context()).Error("liste d'attente (cabinet) en échec",
			slog.String("user_id", a.UserID.String()), slog.String("err", err.Error()))
		h.rd.Error(w, r, http.StatusInternalServerError, "")
	}
}

// WaitlistRow est une inscription affichée.
type WaitlistRow struct {
	ID           string
	Patient      string
	PatientURL   string
	Type         string
	Practitioner string
	Period       string
	Days         string
	Hours        string
	Status       string
	Source       string
	CanCancel    bool
}

// WaitlistPageData est la donnée de la page admin_waitlist.
type WaitlistPageData struct {
	Rows          []WaitlistRow
	Types         []TypeChoice
	Practitioners []PractitionerChoice
	Weekdays      []WeekdayChoice
	Values        WaitlistAdminValues
	Today         string
	Errors        []string
}

// PractitionerChoice est une option « praticien ».
type PractitionerChoice struct {
	ID       string
	Name     string
	Selected bool
}

// WaitlistAdminValues est la saisie conservée du formulaire du cabinet.
type WaitlistAdminValues struct {
	FirstName, LastName, Phone, Email  string
	TypeID, PractitionerID             string
	Earliest, Latest, TimeFrom, TimeTo string
	Weekdays                           []string
}

func statusLabel(s string) string {
	switch s {
	case "active":
		return "En attente"
	case "fulfilled":
		return "Créneau obtenu"
	case "expired":
		return "Expirée"
	case "cancelled":
		return "Retirée"
	}
	return s
}

func dateFR(d pgtype.Date) string { return d.Time.Format("02/01/2006") }

func clockFR(t pgtype.Time) string {
	m := t.Microseconds / int64(time.Minute/time.Microsecond)
	return fmt.Sprintf("%dh%02d", m/60, m%60)
}

func (h *waitlistAdminHandlers) row(e sqlcgen.ListWaitlistEntriesRow) WaitlistRow {
	r := WaitlistRow{
		ID: e.ID.String(), Patient: e.PatientFirstName + " " + e.PatientLastName, PatientURL: pathPatients + "/" + e.PatientID.String(),
		Type: e.TypeName, Practitioner: "Tous les praticiens", Period: "du " + dateFR(e.Earliest) + " au " + dateFR(e.Latest),
		Days: "Tous les jours", Hours: "Toute la journée", Status: statusLabel(e.Status), CanCancel: e.Status == "active",
		Source: "En ligne",
	}
	if e.Source == "staff" {
		r.Source = "Cabinet"
	}
	if e.PractitionerFirstName.Valid {
		r.Practitioner = strings.TrimSpace(e.PractitionerFirstName.String + " " + e.PractitionerLastName.String)
	}
	if len(e.Weekdays) > 0 {
		names := make([]string, len(e.Weekdays))
		for i, d := range e.Weekdays {
			names[i] = service.WeekdayName(int(d))
		}
		r.Days = strings.Join(names, ", ")
	}
	if e.TimeFrom.Valid && e.TimeTo.Valid {
		r.Hours = "de " + clockFR(e.TimeFrom) + " à " + clockFR(e.TimeTo)
	}
	return r
}

func (h *waitlistAdminHandlers) render(w http.ResponseWriter, r *http.Request, status int, v WaitlistAdminValues, errs []string) {
	f, err := h.svc.Form(r.Context(), actorFrom(r))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	if v.Earliest == "" {
		v.Earliest = f.Today
	}
	p := WaitlistPageData{Values: v, Today: f.Today, Errors: errs, Weekdays: weekdayChoices(v.Weekdays)}
	for _, e := range f.Entries {
		p.Rows = append(p.Rows, h.row(e))
	}
	for _, t := range f.Types {
		d, _, _ := service.TypeMinutes(t)
		p.Types = append(p.Types, TypeChoice{ID: t.ID.String(), Name: t.Name, Minutes: d, Selected: t.ID.String() == v.TypeID})
	}
	for _, pr := range f.Practitioners {
		if pr.Active {
			p.Practitioners = append(p.Practitioners, PractitionerChoice{
				ID: pr.ID.String(), Name: strings.TrimSpace(pr.FirstName + " " + pr.LastName), Selected: pr.ID.String() == v.PractitionerID,
			})
		}
	}
	h.rd.PageStatus(w, r, status, "admin_waitlist", p)
}

func (h *waitlistAdminHandlers) list(w http.ResponseWriter, r *http.Request) {
	h.render(w, r, http.StatusOK, WaitlistAdminValues{}, nil)
}

func (h *waitlistAdminHandlers) add(w http.ResponseWriter, r *http.Request) {
	v := func(k string) string { return strings.TrimSpace(r.PostFormValue(k)) }
	vals := WaitlistAdminValues{
		FirstName: v("first_name"), LastName: v("last_name"), Phone: v("phone"), Email: v("email"),
		TypeID: v("type_id"), PractitionerID: v("practitioner_id"),
		Earliest: v("earliest"), Latest: v("latest"), TimeFrom: v("time_from"), TimeTo: v("time_to"), Weekdays: r.PostForm["weekdays"],
	}
	win := service.WindowInput{Earliest: vals.Earliest, Latest: vals.Latest, Weekdays: vals.Weekdays, From: vals.TimeFrom, To: vals.TimeTo}
	typeID, err := uuid.Parse(vals.TypeID)
	if err != nil {
		h.render(w, r, http.StatusUnprocessableEntity, vals, []string{"Choisissez un motif."})
		return
	}
	win.TypeID = typeID
	if vals.PractitionerID != "" {
		if win.PractitionerID, err = uuid.Parse(vals.PractitionerID); err != nil {
			h.render(w, r, http.StatusUnprocessableEntity, vals, []string{"Praticien invalide."})
			return
		}
	}
	a := actorFrom(r)
	entry, err := h.svc.AddByStaff(r.Context(), a, service.StaffAddInput{
		Patient: service.PatientDetails{
			FirstName: vals.FirstName, LastName: vals.LastName, Phone: vals.Phone, Email: vals.Email, SMSOptIn: true, EmailOptIn: true,
		},
		Window: win,
	})
	if err != nil {
		if msgs := validation(err); msgs != nil {
			h.render(w, r, http.StatusUnprocessableEntity, vals, msgs)
			return
		}
		h.fail(w, r, err)
		return
	}
	httpx.LoggerFrom(r.Context()).Info("inscription en liste d'attente (cabinet)",
		slog.String("waitlist_entry_id", entry.ID.String()), slog.String("user_id", a.UserID.String()))
	h.rd.SetFlash(w, "success", "Patient inscrit sur la liste d'attente.")
	httpx.Redirect(w, r, pathWaitlist)
}

func (h *waitlistAdminHandlers) cancel(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		h.rd.Error(w, r, http.StatusNotFound, "")
		return
	}
	if err := h.svc.Cancel(r.Context(), actorFrom(r), id); err != nil {
		h.fail(w, r, err)
		return
	}
	h.rd.SetFlash(w, "success", "Inscription retirée de la liste d'attente.")
	httpx.Redirect(w, r, pathWaitlist)
}
