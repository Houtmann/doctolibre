package web

import (
	"net/http"
	"net/url"
	"time"

	"doctolibre/internal/ics"
	"doctolibre/internal/service"
)

// SeriesSession est une séance de la page /s/{jeton}.
type SeriesSession struct {
	Start       time.Time
	Status      string
	StatusLabel string
	Address     string
	ManageURL   string // "" : pas de lien (séance passée, annulée ou lien indisponible)
}

// SeriesData est la donnée de la page public_series.
type SeriesData struct {
	Practice     string
	Phone        string
	Address      string
	Practitioner string
	Type         string
	FirstName    string
	Prescribed   int
	Upcoming     []SeriesSession
	Past         []SeriesSession
	ICSURL       string
}

func seriesURL(token string) string { return "/s/" + url.PathEscape(token) }

func seriesSessions(in []service.PlanLinkSession) []SeriesSession {
	out := make([]SeriesSession, len(in))
	for i, s := range in {
		out[i] = SeriesSession{Start: s.Start, Status: s.Status, StatusLabel: s.StatusLabel, Address: s.Address}
		if s.ManageToken != "" {
			out[i].ManageURL = manageURL(s.ManageToken)
		}
	}
	return out
}

// series affiche la page de la série : séances à venir (avec leur lien de gestion) et passées.
func (h *publicHandlers) series(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	p, err := h.svc.CarePlanByToken(r.Context(), token)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	noStore(w)
	d := SeriesData{
		Practice: p.Practice.Name, Phone: p.Practice.Phone, Address: p.Practice.Address,
		Practitioner: practitionerView(p.Practice.Slug, p.Practitioner).Name, Type: p.Type.Name,
		FirstName: p.FirstName, Prescribed: p.Prescribed,
		Upcoming: seriesSessions(p.Upcoming), Past: seriesSessions(p.Past),
	}
	if len(p.Upcoming) > 0 {
		d.ICSURL = seriesURL(token) + "/ics"
	}
	h.rd.Page(w, r, "public_series", d)
}

// seriesCalendar télécharge un .ics des seules séances à venir non annulées.
func (h *publicHandlers) seriesCalendar(w http.ResponseWriter, r *http.Request) {
	p, err := h.svc.CarePlanByToken(r.Context(), r.PathValue("token"))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	desc := ""
	if p.Practice.Phone != "" {
		desc = "Cabinet : " + p.Practice.Phone
	}
	events := make([]ics.Event, 0, len(p.Upcoming))
	for _, s := range p.Upcoming {
		location := p.Practice.Address
		if p.Type.AtHome && s.Address != "" {
			location = s.Address // le praticien se déplace
		}
		events = append(events, ics.Event{
			// Même UID que le fichier d'une séance seule : un second import met à jour, sans doublon.
			UID: s.ID.String() + "@doctolibre", Start: s.Start, End: s.End, Stamp: h.svc.Now(),
			Summary: p.Type.Name + " - " + p.Practice.Name, Location: location, Description: desc,
			Alarms: []time.Duration{24 * time.Hour, 2 * time.Hour},
		})
	}
	w.Header().Set("Content-Type", "text/calendar; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="seances.ics"`)
	noStore(w)
	_, _ = w.Write(ics.Feed("", events))
}
