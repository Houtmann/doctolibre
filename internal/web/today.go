package web

import (
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"

	"doctolibre/internal/domain"
	"doctolibre/internal/httpx"
	"doctolibre/internal/service"
)

// Chemins de « Ma journée » (T072).
const (
	pathToday       = "/admin/today"
	pathTodayStatus = "/admin/today/appointments/{id}/status"
)

// todayRoutes enregistre « Ma journée » (admin, praticien et secrétariat).
func (d Deps) todayRoutes(mux *http.ServeMux, rd *Renderer, logger *slog.Logger) {
	h := &calendarHandlers{svc: d.Calendar, plans: d.CarePlans, rd: rd, logger: logger}
	require := d.Auth.RequireUser(domain.RoleAdmin, domain.RolePractitioner, domain.RoleSecretary)
	mux.Handle("GET "+pathToday, require(http.HandlerFunc(h.today)))
	mux.Handle("POST "+pathTodayStatus, require(http.HandlerFunc(h.todayStatus)))
}

// TodayPage est la donnée de la page admin_today.
type TodayPage struct {
	CSRF string
	V    service.MyDayView
	// Cards : une carte par RDV ; PrevURL/NextURL/TodayURL : navigation par jour.
	Cards                      []TodayCard
	PrevURL, NextURL, TodayURL string
	// RouteURL : itinéraire OpenStreetMap des visites à domicile ("" s'il n'y en a pas).
	RouteURL string
	Picker   bool // sélecteur de praticien (plusieurs praticiens visibles)
}

// TodayCard est la donnée du partial day_card.
type TodayCard struct {
	CSRF         string
	A            service.MyDayAppt
	Date         string
	Practitioner string
}

// TodayUpdate est la réponse htmx d'un pointage : la carte et le récapitulatif.
type TodayUpdate struct {
	Card    TodayCard
	Summary service.MyDaySummary
}

func todayURL(date string, practitioner uuid.UUID) string {
	q := url.Values{}
	if date != "" {
		q.Set("date", date)
	}
	if practitioner != uuid.Nil {
		q.Set(practitionerParam, practitioner.String())
	}
	if len(q) == 0 {
		return pathToday
	}
	return pathToday + "?" + q.Encode()
}

// routeURL : itinéraire OpenStreetMap passant par les adresses dans l'ordre
// (le géocodage est fait par OpenStreetMap dans le navigateur, pas par nous).
func routeURL(addrs []string) string {
	if len(addrs) == 0 {
		return ""
	}
	return "https://www.openstreetmap.org/directions?route=" + url.QueryEscape(strings.Join(addrs, ";"))
}

func practitionerFromRequest(r *http.Request) (uuid.UUID, bool) {
	raw := strings.TrimSpace(r.FormValue(practitionerParam))
	if raw == "" {
		return uuid.Nil, true
	}
	id, err := uuid.Parse(raw)
	return id, err == nil
}

func (h *calendarHandlers) today(w http.ResponseWriter, r *http.Request) {
	pid, ok := practitionerFromRequest(r)
	if !ok {
		h.rd.Error(w, r, http.StatusNotFound, "")
		return
	}
	v, err := h.svc.Day(r.Context(), actorFrom(r), pid, r.URL.Query().Get("date"))
	if msgs := validation(err); msgs != nil {
		h.rd.Error(w, r, http.StatusBadRequest, msgs[0])
		return
	}
	if err != nil {
		h.fail(w, r, err)
		return
	}
	csrf := httpx.CSRFToken(r.Context())
	p := TodayPage{
		CSRF: csrf, V: v,
		PrevURL:  todayURL(v.PrevDate, v.Practitioner.ID),
		NextURL:  todayURL(v.NextDate, v.Practitioner.ID),
		TodayURL: todayURL(v.TodayDate, v.Practitioner.ID),
		RouteURL: routeURL(v.HomeAddresses),
		Picker:   len(v.Choices) > 1,
	}
	for _, a := range v.Appts {
		p.Cards = append(p.Cards, TodayCard{CSRF: csrf, A: a, Date: v.Date, Practitioner: v.Practitioner.ID.String()})
	}
	h.rd.Page(w, r, "admin_today", p)
}

// todayStatus pointe un RDV (« Fait », « Absent », retour à « À venir ») via
// Calendar.SetStatus. htmx : la carte seule (et le récapitulatif) ; sinon redirection.
func (h *calendarHandlers) todayStatus(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	pid, ok2 := practitionerFromRequest(r)
	if !ok || !ok2 {
		h.rd.Error(w, r, http.StatusNotFound, "")
		return
	}
	date := r.PostFormValue("date")
	if _, err := time.Parse("2006-01-02", date); err != nil {
		date = ""
	}
	a := actorFrom(r)
	if err := h.svc.SetStatus(r.Context(), a, id, r.PostFormValue("status")); err != nil {
		if msgs, ok := formErrors(err); ok {
			h.rd.Error(w, r, http.StatusUnprocessableEntity, msgs[0])
			return
		}
		h.fail(w, r, err)
		return
	}
	if httpx.IsHTMX(r) {
		v, err := h.svc.Day(r.Context(), a, pid, date)
		if err == nil {
			for _, ap := range v.Appts {
				if ap.ID == id {
					h.rd.Partial(w, r, "day_card_update", TodayUpdate{
						Card:    TodayCard{CSRF: httpx.CSRFToken(r.Context()), A: ap, Date: v.Date, Practitioner: v.Practitioner.ID.String()},
						Summary: v.Summary,
					})
					return
				}
			}
		}
		// Carte introuvable (jour modifié entre-temps) : on laisse htmx recharger la page.
		w.Header().Set("HX-Redirect", todayURL(date, pid))
		w.WriteHeader(http.StatusNoContent)
		return
	}
	h.rd.SetFlash(w, "success", "Statut mis à jour.")
	httpx.Redirect(w, r, todayURL(date, pid))
}
