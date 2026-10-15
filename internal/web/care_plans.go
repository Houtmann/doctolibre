package web

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"doctolibre/internal/domain"
	"doctolibre/internal/httpx"
	"doctolibre/internal/service"
)

// Chemins des séries de séances.
const pathCarePlans = "/admin/care-plans"

// carePlanRoutes enregistre les séries de séances (admin et praticien : la
// planification se fait dans l'agenda du praticien connecté).
func (d Deps) carePlanRoutes(mux *http.ServeMux, rd *Renderer, logger *slog.Logger) {
	h := &carePlanHandlers{svc: d.CarePlans, rd: rd, logger: logger}
	require := d.Auth.RequireUser(domain.RoleAdmin, domain.RolePractitioner)
	handle := func(pattern string, f http.HandlerFunc) { mux.Handle(pattern, require(f)) }

	handle("GET "+pathPatients+"/{id}/care-plans/new", h.newForm)
	handle("POST "+pathPatients+"/{id}/care-plans/propose", h.propose)
	handle("POST "+pathPatients+"/{id}/care-plans", h.create)
	handle("POST "+pathCarePlans+"/{id}/status", h.status)
	handle("POST "+pathCarePlans+"/{id}/replace", h.replace)
}

type carePlanHandlers struct {
	svc    *service.CarePlans
	rd     *Renderer
	logger *slog.Logger
}

func (h *carePlanHandlers) fail(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, domain.ErrNotFound):
		h.rd.Error(w, r, http.StatusNotFound, "")
	case errors.Is(err, domain.ErrForbidden):
		h.rd.Error(w, r, http.StatusForbidden, "Accès refusé.")
	default:
		a := actorFrom(r)
		httpx.LoggerFrom(r.Context()).Error("séries de séances en échec",
			slog.String("user_id", a.UserID.String()), slog.String("err", err.Error()))
		h.rd.Error(w, r, http.StatusInternalServerError, "")
	}
}

// CarePlanValues est la saisie conservée d'un formulaire de série.
type CarePlanValues struct {
	TypeID, Label, Prescribed, PerWeek, StartDate string
	WindowStart, WindowEnd, Address               string
	Weekdays                                      []string
}

// WeekdayChoice est une case « jour préféré ».
type WeekdayChoice struct {
	Value   string
	Label   string
	Checked bool
}

// CarePlanPage est la donnée de la page admin_care_plan_new (étape 1 : critères ;
// étape 2 : critères + séances proposées, dès que Lines n'est pas vide).
type CarePlanPage struct {
	PatientID, PatientName string
	Values                 CarePlanValues
	Types                  []TypeChoice
	Weekdays               []WeekdayChoice
	Lines                  []service.ProposalLine
	Found                  int
	Want                   int
	AtHome                 bool
	ConflictRow            int
	Errors                 []string
}

func readPlanValues(r *http.Request) CarePlanValues {
	v := func(k string) string { return strings.TrimSpace(r.PostFormValue(k)) }
	return CarePlanValues{
		TypeID: v("type_id"), Label: v("label"), Prescribed: v("prescribed"), PerWeek: v("per_week"), StartDate: v("start_date"),
		WindowStart: v("window_start"), WindowEnd: v("window_end"), Address: v("address"), Weekdays: r.PostForm["weekdays"],
	}
}

func (v CarePlanValues) input(patientID uuid.UUID) service.PlanInput {
	return service.PlanInput{
		PatientID: patientID.String(), TypeID: v.TypeID, Label: v.Label, Prescribed: v.Prescribed, PerWeek: v.PerWeek,
		Weekdays: v.Weekdays, WindowStart: v.WindowStart, WindowEnd: v.WindowEnd, StartDate: v.StartDate, Address: v.Address,
	}
}

func readLines(r *http.Request) []service.LineInput {
	dates, times := r.PostForm["line_date"], r.PostForm["line_time"]
	out := make([]service.LineInput, len(dates))
	for i := range dates {
		out[i].Date = strings.TrimSpace(dates[i])
		if i < len(times) {
			out[i].Time = strings.TrimSpace(times[i])
		}
	}
	return out
}

func (h *carePlanHandlers) page(r *http.Request, patientID uuid.UUID, v CarePlanValues, lines []service.LineInput, errs []string) (CarePlanPage, error) {
	f, err := h.svc.Form(r.Context(), actorFrom(r), patientID)
	if err != nil {
		return CarePlanPage{}, err
	}
	if v.StartDate == "" {
		v.StartDate = f.Today
	}
	p := CarePlanPage{
		PatientID: patientID.String(), PatientName: f.Patient.FirstName + " " + f.Patient.LastName, Values: v, Errors: errs,
	}
	for _, t := range f.Types {
		d, _, _ := service.TypeMinutes(t)
		p.Types = append(p.Types, TypeChoice{ID: t.ID.String(), Name: t.Name, Minutes: d, Selected: t.ID.String() == v.TypeID})
		if t.ID.String() == v.TypeID {
			p.AtHome = t.AtHome
		}
	}
	chosen := map[string]bool{}
	for _, w := range v.Weekdays {
		chosen[w] = true
	}
	for i := range 7 {
		val := fmt.Sprint(i)
		p.Weekdays = append(p.Weekdays, WeekdayChoice{Value: val, Label: service.Capitalize(service.WeekdayName(i)), Checked: chosen[val]})
	}
	for i, l := range lines {
		p.Lines = append(p.Lines, service.ProposalLine{Row: i + 1, Date: l.Date, Time: l.Time})
		if l.Date != "" || l.Time != "" {
			p.Found++
		}
	}
	p.Want = len(lines)
	return p, nil
}

func (h *carePlanHandlers) render(w http.ResponseWriter, r *http.Request, status int, p CarePlanPage) {
	h.rd.PageStatus(w, r, status, "admin_care_plan_new", p)
}

func (h *carePlanHandlers) newForm(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		h.rd.Error(w, r, http.StatusNotFound, "")
		return
	}
	p, err := h.page(r, id, CarePlanValues{Prescribed: "10", PerWeek: "2"}, nil, nil)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	h.render(w, r, http.StatusOK, p)
}

// propose calcule la proposition de séances et affiche l'étape d'ajustement.
func (h *carePlanHandlers) propose(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		h.rd.Error(w, r, http.StatusNotFound, "")
		return
	}
	v := readPlanValues(r)
	prop, err := h.svc.Propose(r.Context(), actorFrom(r), v.input(id))
	if msgs := validation(err); msgs != nil {
		p, perr := h.page(r, id, v, nil, msgs)
		if perr != nil {
			h.fail(w, r, perr)
			return
		}
		h.render(w, r, http.StatusUnprocessableEntity, p)
		return
	}
	if err != nil {
		h.fail(w, r, err)
		return
	}
	lines := make([]service.LineInput, len(prop.Lines))
	for i, l := range prop.Lines {
		lines[i] = service.LineInput{Date: l.Date, Time: l.Time}
	}
	p, err := h.page(r, id, v, lines, nil)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	if prop.Found < len(prop.Lines) {
		p.Errors = []string{fmt.Sprintf("Seulement %d séance(s) sur %d ont pu être placées dans les disponibilités : complétez les lignes vides à la main, ou ajoutez des plages de disponibilité.", prop.Found, len(prop.Lines))}
	}
	h.render(w, r, http.StatusOK, p)
}

// create valide la série : toutes les séances ou aucune.
func (h *carePlanHandlers) create(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		h.rd.Error(w, r, http.StatusNotFound, "")
		return
	}
	v, lines := readPlanValues(r), readLines(r)
	plan, err := h.svc.Create(r.Context(), actorFrom(r), service.CreateInput{Plan: v.input(id), Lines: lines})
	var conflict *service.LineConflict
	var msgs []string
	row := 0
	switch {
	case err == nil:
		h.rd.SetFlash(w, "success", "Série créée : les séances sont planifiées.")
		httpx.Redirect(w, r, pathPatients+"/"+plan.PatientID.String())
		return
	case errors.As(err, &conflict):
		row = conflict.Row
		msgs = []string{fmt.Sprintf("Séance %d (%s à %s) : %s Aucune séance n'a été créée ; ajustez cette ligne puis validez à nouveau.",
			conflict.Row, frenchDate(conflict.Start), conflict.Start.Format("15h04"), service.ErrSlotTakenMsg)}
	default:
		if msgs = validation(err); msgs == nil {
			h.fail(w, r, err)
			return
		}
	}
	p, perr := h.page(r, id, v, lines, msgs)
	if perr != nil {
		h.fail(w, r, perr)
		return
	}
	p.ConflictRow = row
	h.render(w, r, http.StatusUnprocessableEntity, p)
}

func (h *carePlanHandlers) status(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		h.rd.Error(w, r, http.StatusNotFound, "")
		return
	}
	plan, err := h.svc.SetStatus(r.Context(), actorFrom(r), id, r.PostFormValue("status"))
	if msgs := validation(err); msgs != nil {
		h.rd.SetFlash(w, "error", strings.Join(msgs, " "))
		httpx.Redirect(w, r, pathPatients)
		return
	}
	if err != nil {
		h.fail(w, r, err)
		return
	}
	h.rd.SetFlash(w, "success", "Série mise à jour.")
	httpx.Redirect(w, r, pathPatients+"/"+plan.PatientID.String())
}

// replace planifie une séance de remplacement proposée après une annulation.
func (h *carePlanHandlers) replace(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		h.rd.Error(w, r, http.StatusNotFound, "")
		return
	}
	back := pathAppointments
	if appt, err := uuid.Parse(r.PostFormValue("appointment_id")); err == nil {
		back = pathAppointments + "/" + appt.String()
	}
	ap, err := h.svc.AddSession(r.Context(), actorFrom(r), id, r.PostFormValue("date"), r.PostFormValue("time"))
	if msgs, ok := formErrors(err); ok {
		h.rd.SetFlash(w, "error", strings.Join(msgs, " "))
		httpx.Redirect(w, r, back)
		return
	}
	if err != nil {
		h.fail(w, r, err)
		return
	}
	h.rd.SetFlash(w, "success", "Séance de remplacement planifiée.")
	httpx.Redirect(w, r, pathAppointments+"/"+ap.ID.String())
}
