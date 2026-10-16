package web

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"github.com/google/uuid"

	"doctolibre/internal/auth"
	"doctolibre/internal/domain"
	"doctolibre/internal/httpx"
	"doctolibre/internal/service"
	"doctolibre/internal/store/sqlcgen"
)

// Chemins de l'agenda.
const (
	pathCalendar       = "/admin/calendar"
	pathCalendarEvents = "/admin/agenda/events"
	pathAppointments   = "/admin/appointments"
	pathPatientFind    = "/admin/patients/search"
)

// calendarRoutes enregistre l'agenda (admin, praticien et secrétariat).
func (d Deps) calendarRoutes(mux *http.ServeMux, rd *Renderer, logger *slog.Logger) {
	h := &calendarHandlers{svc: d.Calendar, plans: d.CarePlans, rd: rd, logger: logger}
	require := d.Auth.RequireUser(domain.RoleAdmin, domain.RolePractitioner, domain.RoleSecretary)
	handle := func(pattern string, f http.HandlerFunc) { mux.Handle(pattern, require(f)) }
	// Le praticien visé par une création de RDV est contrôlé par la matrice de permissions.
	target := withTarget(rd, auth.ActionAppointmentManage)
	handleFor := func(pattern string, f http.HandlerFunc) { mux.Handle(pattern, require(target(f))) }

	handle("GET "+pathCalendar, h.calendar)
	handle("GET "+pathCalendarEvents, h.events)
	handle("GET "+pathPatientFind, h.patientSearch)
	handleFor("GET "+pathAppointments+"/new", h.newForm)
	handleFor("POST "+pathAppointments, h.create)
	handle("GET "+pathAppointments+"/{id}", h.detail)
	handle("POST "+pathAppointments+"/{id}/move", h.move)
	handle("POST "+pathAppointments+"/{id}/cancel", h.cancel)
	handle("POST "+pathAppointments+"/{id}/status", h.status)
}

type calendarHandlers struct {
	svc    *service.Calendar
	plans  *service.CarePlans // optionnel
	rd     *Renderer
	logger *slog.Logger
}

func (h *calendarHandlers) fail(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, domain.ErrNotFound):
		h.rd.Error(w, r, http.StatusNotFound, "")
	case errors.Is(err, domain.ErrForbidden):
		h.rd.Error(w, r, http.StatusForbidden, "Accès refusé.")
	default:
		a := actorFrom(r)
		httpx.LoggerFrom(r.Context()).Error("agenda en échec",
			slog.String("user_id", a.UserID.String()), slog.String("err", err.Error()))
		h.rd.Error(w, r, http.StatusInternalServerError, "")
	}
}

// formErrors traduit une erreur de saisie (validation, créneau pris) en
// messages à afficher ; ok=false si l'erreur est inattendue.
func formErrors(err error) (msgs []string, ok bool) {
	if errors.Is(err, domain.ErrSlotTaken) {
		return []string{service.ErrSlotTakenMsg}, true
	}
	if m := validation(err); m != nil {
		return m, true
	}
	return nil, false
}

// show rend une page complète, ou son fragment pour htmx (200 même invalide :
// htmx n'échange pas les réponses 4xx).
func (h *calendarHandlers) show(w http.ResponseWriter, r *http.Request, page, partial string, data any, invalid bool) {
	if httpx.IsHTMX(r) {
		h.rd.Partial(w, r, partial, data)
		return
	}
	status := http.StatusOK
	if invalid {
		status = http.StatusUnprocessableEntity
	}
	h.rd.PageStatus(w, r, status, page, data)
}

// --- Agenda ---

// CalendarPage est la donnée de la page admin_calendar. Le calendrier
// (EventCalendar) est configuré par des attributs data-* ; la liste du jour
// rendue côté serveur sert de repli sans JavaScript.
type CalendarPage struct {
	CSRF string
	A    service.Agenda
	// View : « week », « day » ou « practitioners » (vue par praticien) ; vide = le défaut du navigateur.
	View string
	// ResourcesJSON : praticiens affichés, [{"id","title"}], pour EventCalendar.
	ResourcesJSON string
	PrevURL       string
	NextURL       string
	TodayURL      string
	NewURL        string
	// Picker : sélecteur de praticiens (nil si un seul est accessible).
	Picker *CalendarPicker
}

// CalendarPicker est le sélecteur de praticiens de l'agenda.
type CalendarPicker struct {
	Date, Mode string
	Options    []PractitionerOption
	AllChecked bool
}

// calSel est la sélection de praticiens de la requête, reportée dans les liens.
type calSel struct {
	ids []uuid.UUID
	all bool
}

func (c calSel) apply(q url.Values) {
	if c.all {
		q.Set(practitionerParam, "all")
		return
	}
	for _, id := range c.ids {
		q.Add(practitionerParam, id.String())
	}
}

// calendarViews : valeurs acceptées pour le paramètre « view » de l'agenda.
var calendarViews = map[string]bool{"week": true, "day": true, "practitioners": true}

func calendarURL(mode, date string, sel calSel) string {
	q := url.Values{"view": {mode}, "date": {date}}
	sel.apply(q)
	return pathCalendar + "?" + q.Encode()
}

// selectionFrom lit les praticiens demandés (« practitioner=<id> » répété, ou « all »).
func selectionFrom(r *http.Request) (calSel, bool) {
	var sel calSel
	for _, raw := range r.URL.Query()[practitionerParam] {
		if raw == "all" {
			sel.all = true
			continue
		}
		id, err := uuid.Parse(raw)
		if err != nil {
			return sel, false
		}
		sel.ids = append(sel.ids, id)
	}
	if sel.all {
		sel.ids = nil
	}
	return sel, true
}

func (h *calendarHandlers) calendar(w http.ResponseWriter, r *http.Request) {
	begin := time.Now()
	qv := r.URL.Query()
	sel, ok := selectionFrom(r)
	if !ok {
		h.rd.Error(w, r, http.StatusBadRequest, "Praticien invalide.")
		return
	}
	view := qv.Get("view")
	if !calendarViews[view] {
		view = ""
	}
	ag, err := h.svc.AgendaDay(r.Context(), actorFrom(r), qv.Get("date"), service.Selection{IDs: sel.ids, All: sel.all})
	if msgs := validation(err); msgs != nil {
		h.rd.Error(w, r, http.StatusBadRequest, msgs[0])
		return
	}
	if err != nil {
		h.fail(w, r, err)
		return
	}
	// Les liens reportent la sélection effective : explicite, ou celle par défaut
	// du secrétariat (tous les praticiens) pour que la navigation la conserve.
	link := sel
	if len(link.ids) == 0 && !link.all && actorFrom(r).Role == domain.RoleSecretary {
		link.all = true
	}
	mode := view
	if mode == "" || mode == "practitioners" {
		mode = "day"
	}
	newQ := url.Values{"start": {ag.Date + "T09:00"}}
	if len(ag.Practitioners) == 1 {
		newQ.Set(practitionerParam, ag.Practitioners[0].ID.String())
	}
	res := make([]calendarResource, len(ag.Practitioners))
	for i, p := range ag.Practitioners {
		res[i] = calendarResource{ID: p.ID.String(), Title: p.Name}
	}
	resJSON, err := json.Marshal(res)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	p := CalendarPage{
		CSRF: httpx.CSRFToken(r.Context()), A: ag, View: view, ResourcesJSON: string(resJSON),
		PrevURL:  calendarURL(mode, ag.PrevDate, link),
		NextURL:  calendarURL(mode, ag.NextDate, link),
		TodayURL: calendarURL(mode, ag.TodayDate, link),
		NewURL:   pathAppointments + "/new?" + newQ.Encode(),
	}
	if p.Picker, err = h.picker(r, ag, view, sel); err != nil {
		h.fail(w, r, err)
		return
	}
	h.rd.Page(w, r, "admin_calendar", p)
	httpx.LoggerFrom(r.Context()).Info("agenda rendu",
		slog.String("view", view), slog.Int("appointments", len(ag.Appts)),
		slog.Int64("duration_ms", time.Since(begin).Milliseconds()))
}

// calendarResource est un praticien tel que l'attend EventCalendar.
type calendarResource struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

// picker construit le sélecteur de praticiens ; nil si un seul est accessible.
func (h *calendarHandlers) picker(r *http.Request, ag service.Agenda, view string, sel calSel) (*CalendarPicker, error) {
	a := actorFrom(r)
	prs, err := h.svc.ViewablePractitioners(r.Context(), a)
	if err != nil {
		return nil, err
	}
	if len(prs) < 2 {
		return nil, nil
	}
	shown := map[uuid.UUID]bool{}
	for _, p := range ag.Practitioners {
		shown[p.ID] = true
	}
	pk := &CalendarPicker{Date: ag.Date, Mode: view, AllChecked: sel.all}
	for _, p := range prs {
		pk.Options = append(pk.Options, PractitionerOption{ID: p.ID.String(), Name: service.PractitionerName(p), Selected: shown[p.ID]})
	}
	return pk, nil
}

// --- Création ---

// PatientRow est un patient proposé dans les résultats de recherche.
type PatientRow struct {
	ID, Name, Contact string
	Selected          bool
}

// FormValues est la saisie du formulaire de création.
type FormValues struct {
	PatientID                                     string
	NewFirstName, NewLastName, NewPhone, NewEmail string
	TypeID                                        string
	Date, Time                                    string
	Address                                       string
	Force                                         bool
	View                                          string
	Query                                         string
	Start                                         string // pour le formulaire de recherche sans JS
	Practitioner                                  string // praticien visé (vide = le sien)
}

// TypeChoice est un motif du select.
type TypeChoice struct {
	ID, Name string
	Minutes  int
	Selected bool
}

// FormPage est la donnée du fragment appointment_form et de la page admin_appointment_new.
type FormPage struct {
	CSRF      string
	Form      FormValues
	Types     []TypeChoice
	Patients  []PatientRow
	QuickOpen bool
	Errors    []string
	// Practitioners : choix du praticien (secrétariat, administrateur) ; vide = un seul possible.
	Practitioners []PractitionerOption
	// Modal : rendu dans la boîte de dialogue de l'agenda (htmx) plutôt qu'en page.
	Modal bool
}

func patientRow(p sqlcgen.Patient, selected bool) PatientRow {
	contact := p.Phone.String
	if contact == "" {
		contact = p.Email.String
	}
	return PatientRow{ID: p.ID.String(), Name: p.LastName + " " + p.FirstName, Contact: contact, Selected: selected}
}

func (h *calendarHandlers) formPage(r *http.Request, f FormValues, errs []string) (*FormPage, error) {
	a := actorFrom(r)
	if err := h.svc.CheckTarget(r.Context(), a); err != nil {
		return nil, err
	}
	ts, err := h.svc.ActiveTypes(r.Context(), a)
	if err != nil {
		return nil, err
	}
	p := &FormPage{CSRF: httpx.CSRFToken(r.Context()), Form: f, Errors: errs, Modal: httpx.IsHTMX(r)}
	for _, t := range ts {
		d, _, _ := service.TypeMinutes(t)
		p.Types = append(p.Types, TypeChoice{ID: t.ID.String(), Name: t.Name, Minutes: d, Selected: t.ID.String() == f.TypeID})
	}
	var selected *sqlcgen.Patient
	if id, err := uuid.Parse(f.PatientID); err == nil {
		if pt, err := h.svc.Patient(r.Context(), a, id); err == nil {
			selected = &pt
		}
	}
	found, err := h.svc.SearchPatients(r.Context(), a, f.Query)
	if err != nil {
		return nil, err
	}
	if selected != nil {
		p.Patients = append(p.Patients, patientRow(*selected, true))
	}
	for _, pt := range found {
		if selected == nil || pt.ID != selected.ID {
			p.Patients = append(p.Patients, patientRow(pt, false))
		}
	}
	if prs, err := h.svc.ViewablePractitioners(r.Context(), a); err != nil {
		return nil, err
	} else if len(prs) > 1 {
		for _, pr := range prs {
			p.Practitioners = append(p.Practitioners, PractitionerOption{
				ID: pr.ID.String(), Name: service.PractitionerName(pr),
				Selected: pr.ID.String() == f.Practitioner || (f.Practitioner == "" && pr.ID == a.PractitionerID),
			})
		}
	}
	p.QuickOpen = f.NewFirstName != "" || f.NewLastName != "" || f.NewPhone != "" || f.NewEmail != ""
	return p, nil
}

func (h *calendarHandlers) newForm(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := FormValues{PatientID: q.Get("patient_id"), Query: q.Get("q"), View: q.Get("view"), Start: q.Get("start"), Practitioner: q.Get(practitionerParam)}
	if t, err := time.Parse("2006-01-02T15:04", f.Start); err == nil {
		f.Date, f.Time = t.Format("2006-01-02"), t.Format("15:04")
	}
	p, err := h.formPage(r, f, nil)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	h.show(w, r, "admin_appointment_new", "appointment_form", p, false)
}

func (h *calendarHandlers) patientSearch(w http.ResponseWriter, r *http.Request) {
	f := FormValues{Query: r.URL.Query().Get("q")}
	p, err := h.formPage(r, f, nil)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	h.rd.Partial(w, r, "patient_results", p)
}

func (h *calendarHandlers) create(w http.ResponseWriter, r *http.Request) {
	f := FormValues{
		PatientID:    r.PostFormValue("patient_id"),
		NewFirstName: r.PostFormValue("new_first_name"), NewLastName: r.PostFormValue("new_last_name"),
		NewPhone: r.PostFormValue("new_phone"), NewEmail: r.PostFormValue("new_email"),
		TypeID: r.PostFormValue("type_id"), Date: r.PostFormValue("date"), Time: r.PostFormValue("time"),
		Address: r.PostFormValue("address"),
		Force:   r.PostFormValue("force") == "1", View: r.PostFormValue("view"),
		Practitioner: r.PostFormValue(practitionerParam),
	}
	_, err := h.svc.Create(r.Context(), actorFrom(r), service.NewAppointmentInput{
		PatientID: f.PatientID, NewFirstName: f.NewFirstName, NewLastName: f.NewLastName,
		NewPhone: f.NewPhone, NewEmail: f.NewEmail, TypeID: f.TypeID, Date: f.Date, Time: f.Time, Force: f.Force, Address: f.Address,
	})
	if msgs, ok := formErrors(err); ok {
		p, perr := h.formPage(r, f, msgs)
		if perr != nil {
			h.fail(w, r, perr)
			return
		}
		h.show(w, r, "admin_appointment_new", "appointment_form", p, true)
		return
	}
	if err != nil {
		h.fail(w, r, err)
		return
	}
	h.finish(w, r, "Rendez-vous créé.", f.View, f.Date, f.Practitioner)
}

// backURL retourne l'agenda sur la semaine (ou le jour) de la date donnée ;
// une date invalide donne l'agenda courant.
func backURL(view, date, practitioner string) string {
	if !calendarViews[view] {
		view = "week"
	}
	var sel calSel
	if id, err := uuid.Parse(practitioner); err == nil {
		sel.ids = []uuid.UUID{id}
	}
	if _, err := time.Parse("2006-01-02", date); err != nil {
		q := url.Values{"view": {view}}
		sel.apply(q)
		return pathCalendar + "?" + q.Encode()
	}
	return calendarURL(view, date, sel)
}

// finish termine une action réussie. Dans la boîte de dialogue de l'agenda (requête
// htmx), il n'y a ni message ni redirection : l'en-tête HX-Trigger demande à
// l'agenda de se rafraîchir (et d'aller à la date concernée). Sinon : message
// et redirection vers l'agenda.
func (h *calendarHandlers) finish(w http.ResponseWriter, r *http.Request, flash, view, date, practitioner string) {
	if httpx.IsHTMX(r) {
		detail := map[string]string{}
		if _, err := time.Parse("2006-01-02", date); err == nil {
			detail["date"] = date
		}
		b, _ := json.Marshal(map[string]any{eventAgendaChanged: detail})
		w.Header().Set("HX-Trigger", string(b))
		w.WriteHeader(http.StatusNoContent)
		return
	}
	h.rd.SetFlash(w, "success", flash)
	httpx.Redirect(w, r, backURL(view, date, practitioner))
}

// eventAgendaChanged est l'événement htmx qui fait recharger les événements de l'agenda.
const eventAgendaChanged = "agenda:changed"

// --- Détail et actions ---

// DetailPage est la donnée du fragment appointment_detail et de la page admin_appointment.
type DetailPage struct {
	CSRF   string
	D      service.AppointmentDetail
	Move   service.MoveInput
	Reason string
	View   string
	Errors []string
	Plan   *service.PlanBadge // série de séances, nil sinon
	Modal  bool               // rendu dans la boîte de dialogue de l'agenda (htmx)
}

func (h *calendarHandlers) detailPage(w http.ResponseWriter, r *http.Request, id uuid.UUID, move *service.MoveInput, reason string, errs []string) {
	d, err := h.svc.Detail(r.Context(), actorFrom(r), id)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	p := DetailPage{CSRF: httpx.CSRFToken(r.Context()), D: d, Move: service.MoveInput{Date: d.MoveDate, Time: d.MoveTime}, Reason: reason, View: r.FormValue("view"), Errors: errs, Modal: httpx.IsHTMX(r)}
	if move != nil {
		p.Move = *move
	}
	if h.plans != nil {
		if p.Plan, err = h.plans.ForAppointment(r.Context(), actorFrom(r), id); err != nil {
			h.fail(w, r, err)
			return
		}
	}
	h.show(w, r, "admin_appointment", "appointment_detail", p, len(errs) > 0)
}

func (h *calendarHandlers) detail(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		h.rd.Error(w, r, http.StatusNotFound, "")
		return
	}
	h.detailPage(w, r, id, nil, "", nil)
}

// done termine une action : redirection vers l'agenda à la date donnée, ou
// ré-affichage de la fiche avec les erreurs de saisie.
func (h *calendarHandlers) done(w http.ResponseWriter, r *http.Request, id uuid.UUID, err error, move *service.MoveInput, reason, flash, date string) {
	if msgs, ok := formErrors(err); ok {
		h.detailPage(w, r, id, move, reason, msgs)
		return
	}
	if err != nil {
		h.fail(w, r, err)
		return
	}
	h.finish(w, r, flash, r.PostFormValue("view"), date, r.PostFormValue(practitionerParam))
}

func (h *calendarHandlers) move(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		h.rd.Error(w, r, http.StatusNotFound, "")
		return
	}
	in := service.MoveInput{Date: r.PostFormValue("date"), Time: r.PostFormValue("time"), Force: r.PostFormValue("force") == "1"}
	err := h.svc.Move(r.Context(), actorFrom(r), id, in)
	h.done(w, r, id, err, &in, "", "Rendez-vous déplacé.", in.Date)
}

func (h *calendarHandlers) cancel(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		h.rd.Error(w, r, http.StatusNotFound, "")
		return
	}
	reason := r.PostFormValue("reason")
	err := h.svc.Cancel(r.Context(), actorFrom(r), id, reason)
	h.done(w, r, id, err, nil, reason, "Rendez-vous annulé.", r.PostFormValue("date"))
}

func (h *calendarHandlers) status(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		h.rd.Error(w, r, http.StatusNotFound, "")
		return
	}
	err := h.svc.SetStatus(r.Context(), actorFrom(r), id, r.PostFormValue("status"))
	h.done(w, r, id, err, nil, "", "Statut mis à jour.", r.PostFormValue("date"))
}
