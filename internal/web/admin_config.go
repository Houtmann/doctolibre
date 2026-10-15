package web

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"doctolibre/internal/auth"
	"doctolibre/internal/domain"
	"doctolibre/internal/httpx"
	"doctolibre/internal/service"
	"doctolibre/internal/store/sqlcgen"
)

// Chemins de l'administration du cabinet.
const (
	pathSettings     = "/admin/settings"
	pathTypes        = "/admin/appointment-types"
	pathAvailability = "/admin/availability"
	pathAbsences     = "/admin/absences"
)

// configRoutes enregistre les pages de paramétrage du cabinet (admin et praticien).
func (d Deps) configRoutes(mux *http.ServeMux, rd *Renderer, logger *slog.Logger) {
	h := &configHandlers{svc: d.Practice, rd: rd, logger: logger}
	require := d.Auth.RequireUser(domain.RoleAdmin, domain.RolePractitioner)
	handle := func(pattern string, f http.HandlerFunc) { mux.Handle(pattern, require(f)) }

	handle("GET "+pathSettings, h.settingsForm)
	handle("POST "+pathSettings, h.settingsSubmit)

	handle("GET "+pathTypes, h.typesList)
	handle("POST "+pathTypes, h.typesCreate)
	handle("GET "+pathTypes+"/{id}", h.typesEdit)
	handle("POST "+pathTypes+"/{id}", h.typesUpdate)
	handle("POST "+pathTypes+"/{id}/active", h.typesActive)
	handle("POST "+pathTypes+"/{id}/move", h.typesMove)

	handle("GET "+pathAvailability, h.availabilityPage)
	handle("POST "+pathAvailability, h.availabilityAdd)
	handle("POST "+pathAvailability+"/{id}/delete", h.availabilityDelete)

	handle("GET "+pathAbsences, h.absencesPage)
	handle("POST "+pathAbsences, h.absencesAdd)
	handle("POST "+pathAbsences+"/{id}/delete", h.absencesDelete)
}

type configHandlers struct {
	svc    *service.Practice
	rd     *Renderer
	logger *slog.Logger
}

func actorFrom(r *http.Request) service.Actor {
	u, _ := auth.UserFrom(r.Context())
	return service.Actor{UserID: u.ID, PracticeID: u.PracticeID}
}

// fail traduit une erreur non attendue en réponse HTTP (un seul endroit).
func (h *configHandlers) fail(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, domain.ErrNotFound):
		h.rd.Error(w, r, http.StatusNotFound, "")
	case errors.Is(err, domain.ErrForbidden):
		h.rd.Error(w, r, http.StatusForbidden, "Accès refusé.")
	default:
		a := actorFrom(r)
		httpx.LoggerFrom(r.Context()).Error("administration du cabinet en échec",
			slog.String("user_id", a.UserID.String()), slog.String("err", err.Error()))
		h.rd.Error(w, r, http.StatusInternalServerError, "")
	}
}

// validation retourne les messages d'erreur de formulaire de err, ou nil si ce
// n'est pas une erreur de validation.
func validation(err error) []string {
	fe := domain.FieldErrors(err)
	if len(fe) == 0 {
		return nil
	}
	msgs := make([]string, len(fe))
	for i, e := range fe {
		msgs[i] = e.Msg
	}
	return msgs
}

func pathID(r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(r.PathValue("id"))
	return id, err == nil
}

// show rend une page (ou, pour htmx, son panneau seul). invalid → 422 hors htmx
// (htmx n'échange pas les réponses 4xx).
func (h *configHandlers) show(w http.ResponseWriter, r *http.Request, page, panel string, data any, invalid bool) {
	if httpx.IsHTMX(r) {
		h.rd.Partial(w, r, panel, data)
		return
	}
	status := http.StatusOK
	if invalid {
		status = http.StatusUnprocessableEntity
	}
	h.rd.PageStatus(w, r, status, page, data)
}

// succeed termine une action réussie : redirection 303 avec message flash, ou
// panneau rafraîchi avec une confirmation pour htmx.
func (h *configHandlers) succeed(w http.ResponseWriter, r *http.Request, path, msg, panel string, build func(notice string) (any, error)) {
	if httpx.IsHTMX(r) {
		data, err := build(msg)
		if err != nil {
			h.fail(w, r, err)
			return
		}
		h.rd.Partial(w, r, panel, data)
		return
	}
	h.rd.SetFlash(w, "success", msg)
	httpx.Redirect(w, r, path)
}

// --- Paramètres du cabinet et profil ---

// SettingsPage est la donnée de la page admin_settings.
type SettingsPage struct {
	Form   service.SettingsInput
	Errors []string
}

func settingsInputOf(s service.Settings) service.SettingsInput {
	return service.SettingsInput{
		Name: s.Practice.Name, Address: s.Practice.Address, Phone: s.Practice.Phone,
		BookingNoticeHours: strconv.Itoa(s.BookingNoticeHour),
		CancelNoticeHours:  strconv.Itoa(s.CancelNoticeHour),
		HorizonDays:        strconv.Itoa(s.HorizonDays),
		FirstName:          s.Practitioner.FirstName, LastName: s.Practitioner.LastName,
		Title: s.Practitioner.Title, Bio: s.Practitioner.Bio, Slug: s.Practitioner.Slug,
		HomeVisitOnline: s.Practice.HomeVisitOnline, HomeVisitZones: strings.Join(s.Practice.HomeVisitZones, "\n"),
	}
}

func (h *configHandlers) settingsForm(w http.ResponseWriter, r *http.Request) {
	s, err := h.svc.Settings(r.Context(), actorFrom(r))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	h.rd.Page(w, r, "admin_settings", SettingsPage{Form: settingsInputOf(s)})
}

func (h *configHandlers) settingsSubmit(w http.ResponseWriter, r *http.Request) {
	in := service.SettingsInput{
		Name: r.PostFormValue("name"), Address: r.PostFormValue("address"), Phone: r.PostFormValue("phone"),
		BookingNoticeHours: r.PostFormValue("booking_notice_hours"),
		CancelNoticeHours:  r.PostFormValue("cancel_notice_hours"),
		HorizonDays:        r.PostFormValue("horizon_days"),
		FirstName:          r.PostFormValue("first_name"), LastName: r.PostFormValue("last_name"),
		Title: r.PostFormValue("title"), Bio: r.PostFormValue("bio"), Slug: r.PostFormValue("slug"),
		HomeVisitOnline: r.PostFormValue("home_visit_online") == "1", HomeVisitZones: r.PostFormValue("home_visit_zones"),
	}
	err := h.svc.UpdateSettings(r.Context(), actorFrom(r), in)
	if msgs := validation(err); msgs != nil {
		h.rd.PageStatus(w, r, http.StatusUnprocessableEntity, "admin_settings", SettingsPage{Form: in, Errors: msgs})
		return
	}
	if err != nil {
		h.fail(w, r, err)
		return
	}
	h.rd.SetFlash(w, "success", "Paramètres enregistrés.")
	httpx.Redirect(w, r, pathSettings)
}

// --- Motifs ---

// TypeRow est un motif affiché dans la liste.
type TypeRow struct {
	ID                      string
	Name, Color             string
	Duration, Before, After int
	Travel                  int
	Online, AtHome, Active  bool
	First, Last             bool
}

// TypesPage est la donnée de la page admin_types et de son panneau htmx.
type TypesPage struct {
	CSRF   string
	Rows   []TypeRow
	Form   service.AppointmentTypeInput
	EditID string // vide = formulaire de création
	Errors []string
	Notice string
}

func defaultTypeForm() service.AppointmentTypeInput {
	return service.AppointmentTypeInput{DurationMin: "30", BufferBeforeMin: "0", BufferAfterMin: "0", TravelBufferMin: "15", Color: "#2563eb", OnlineBookable: true}
}

func typeFormOf(t sqlcgen.AppointmentType) service.AppointmentTypeInput {
	d, b, a := service.TypeMinutes(t)
	return service.AppointmentTypeInput{
		Name: t.Name, DurationMin: strconv.Itoa(d), BufferBeforeMin: strconv.Itoa(b), BufferAfterMin: strconv.Itoa(a),
		Color: t.Color, OnlineBookable: t.OnlineBookable, AtHome: t.AtHome, TravelBufferMin: strconv.Itoa(service.TravelMinutes(t)),
	}
}

func typeFormFrom(r *http.Request) service.AppointmentTypeInput {
	return service.AppointmentTypeInput{
		Name: r.PostFormValue("name"), DurationMin: r.PostFormValue("duration"),
		BufferBeforeMin: r.PostFormValue("buffer_before"), BufferAfterMin: r.PostFormValue("buffer_after"),
		TravelBufferMin: r.PostFormValue("travel_buffer"),
		Color:           r.PostFormValue("color"), OnlineBookable: r.PostFormValue("online_bookable") == "1", AtHome: r.PostFormValue("at_home") == "1",
	}
}

func (h *configHandlers) typesPage(r *http.Request, form service.AppointmentTypeInput, editID string, errs []string, notice string) (*TypesPage, error) {
	ts, err := h.svc.AppointmentTypes(r.Context(), actorFrom(r))
	if err != nil {
		return nil, err
	}
	p := &TypesPage{CSRF: httpx.CSRFToken(r.Context()), Form: form, EditID: editID, Errors: errs, Notice: notice}
	for i, t := range ts {
		d, b, a := service.TypeMinutes(t)
		p.Rows = append(p.Rows, TypeRow{
			ID: t.ID.String(), Name: t.Name, Color: t.Color, Duration: d, Before: b, After: a, Travel: service.TravelMinutes(t),
			Online: t.OnlineBookable, AtHome: t.AtHome, Active: t.Active, First: i == 0, Last: i == len(ts)-1,
		})
	}
	return p, nil
}

// typesShow rend la page des motifs avec la saisie et les erreurs données.
func (h *configHandlers) typesShow(w http.ResponseWriter, r *http.Request, form service.AppointmentTypeInput, editID string, errs []string) {
	p, err := h.typesPage(r, form, editID, errs, "")
	if err != nil {
		h.fail(w, r, err)
		return
	}
	h.show(w, r, "admin_types", "types_panel", p, len(errs) > 0)
}

func (h *configHandlers) typesDone(w http.ResponseWriter, r *http.Request, msg string) {
	h.succeed(w, r, pathTypes, msg, "types_panel", func(notice string) (any, error) {
		return h.typesPage(r, defaultTypeForm(), "", nil, notice)
	})
}

func (h *configHandlers) typesList(w http.ResponseWriter, r *http.Request) {
	h.typesShow(w, r, defaultTypeForm(), "", nil)
}

func (h *configHandlers) typesCreate(w http.ResponseWriter, r *http.Request) {
	in := typeFormFrom(r)
	_, err := h.svc.CreateAppointmentType(r.Context(), actorFrom(r), in)
	if msgs := validation(err); msgs != nil {
		h.typesShow(w, r, in, "", msgs)
		return
	}
	if err != nil {
		h.fail(w, r, err)
		return
	}
	h.typesDone(w, r, "Motif créé.")
}

func (h *configHandlers) typesEdit(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		h.rd.Error(w, r, http.StatusNotFound, "")
		return
	}
	t, err := h.svc.AppointmentType(r.Context(), actorFrom(r), id)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	h.typesShow(w, r, typeFormOf(t), id.String(), nil)
}

func (h *configHandlers) typesUpdate(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		h.rd.Error(w, r, http.StatusNotFound, "")
		return
	}
	in := typeFormFrom(r)
	err := h.svc.UpdateAppointmentType(r.Context(), actorFrom(r), id, in)
	if msgs := validation(err); msgs != nil {
		h.typesShow(w, r, in, id.String(), msgs)
		return
	}
	if err != nil {
		h.fail(w, r, err)
		return
	}
	h.typesDone(w, r, "Motif enregistré.")
}

func (h *configHandlers) typesActive(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		h.rd.Error(w, r, http.StatusNotFound, "")
		return
	}
	active := r.PostFormValue("active") == "1"
	if err := h.svc.SetAppointmentTypeActive(r.Context(), actorFrom(r), id, active); err != nil {
		h.fail(w, r, err)
		return
	}
	msg := "Motif désactivé."
	if active {
		msg = "Motif réactivé."
	}
	h.typesDone(w, r, msg)
}

func (h *configHandlers) typesMove(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		h.rd.Error(w, r, http.StatusNotFound, "")
		return
	}
	delta := 1
	if r.PostFormValue("dir") == "up" {
		delta = -1
	}
	if err := h.svc.MoveAppointmentType(r.Context(), actorFrom(r), id, delta); err != nil {
		h.fail(w, r, err)
		return
	}
	h.typesDone(w, r, "Ordre des motifs mis à jour.")
}

// --- Disponibilités hebdomadaires ---

// TypeOption est un motif sélectionnable dans le formulaire de plage.
type TypeOption struct {
	ID, Name string
	Checked  bool
}

// RuleForm est la saisie du formulaire d'ajout de plages.
type RuleForm struct {
	Start, End string
	Weekdays   map[string]bool
	TypeIDs    map[string]bool // nil = tous cochés (premier affichage)
}

// AvailabilityPage est la donnée de la page admin_availability et de son panneau.
type AvailabilityPage struct {
	CSRF   string
	Days   [7]service.DayView
	Types  []TypeOption
	Form   RuleForm
	Errors []string
	Notice string
}

func defaultRuleForm() RuleForm {
	return RuleForm{Start: "08:00", End: "12:00", Weekdays: map[string]bool{"0": true, "1": true, "2": true, "3": true, "4": true}}
}

func (h *configHandlers) availabilityData(r *http.Request, form RuleForm, errs []string, notice string) (*AvailabilityPage, error) {
	v, err := h.svc.Availability(r.Context(), actorFrom(r))
	if err != nil {
		return nil, err
	}
	p := &AvailabilityPage{CSRF: httpx.CSRFToken(r.Context()), Days: v.Days, Form: form, Errors: errs, Notice: notice}
	for _, t := range v.Types {
		p.Types = append(p.Types, TypeOption{ID: t.ID.String(), Name: t.Name, Checked: form.TypeIDs == nil || form.TypeIDs[t.ID.String()]})
	}
	return p, nil
}

func (h *configHandlers) availabilityShow(w http.ResponseWriter, r *http.Request, form RuleForm, errs []string) {
	p, err := h.availabilityData(r, form, errs, "")
	if err != nil {
		h.fail(w, r, err)
		return
	}
	h.show(w, r, "admin_availability", "availability_panel", p, len(errs) > 0)
}

func (h *configHandlers) availabilityDone(w http.ResponseWriter, r *http.Request, msg string) {
	h.succeed(w, r, pathAvailability, msg, "availability_panel", func(notice string) (any, error) {
		return h.availabilityData(r, defaultRuleForm(), nil, notice)
	})
}

func (h *configHandlers) availabilityPage(w http.ResponseWriter, r *http.Request) {
	h.availabilityShow(w, r, defaultRuleForm(), nil)
}

func (h *configHandlers) availabilityAdd(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	in := service.RuleInput{
		Weekdays: r.PostForm["weekday"], Start: r.PostFormValue("start"), End: r.PostFormValue("end"),
		TypeIDs: r.PostForm["type"],
	}
	err := h.svc.AddRules(r.Context(), actorFrom(r), in)
	if msgs := validation(err); msgs != nil {
		form := RuleForm{Start: in.Start, End: in.End, Weekdays: map[string]bool{}, TypeIDs: map[string]bool{}}
		for _, w := range in.Weekdays {
			form.Weekdays[w] = true
		}
		for _, t := range in.TypeIDs {
			form.TypeIDs[t] = true
		}
		h.availabilityShow(w, r, form, msgs)
		return
	}
	if err != nil {
		h.fail(w, r, err)
		return
	}
	h.availabilityDone(w, r, "Plages ajoutées.")
}

func (h *configHandlers) availabilityDelete(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		h.rd.Error(w, r, http.StatusNotFound, "")
		return
	}
	if err := h.svc.DeleteRule(r.Context(), actorFrom(r), id); err != nil {
		h.fail(w, r, err)
		return
	}
	h.availabilityDone(w, r, "Plage supprimée.")
}

// --- Absences ---

// AbsencesPage est la donnée de la page admin_absences et de son panneau.
type AbsencesPage struct {
	CSRF     string
	Closures []service.ClosureView
	Form     service.ClosureInput
	Errors   []string
	Notice   string
}

func (h *configHandlers) absencesData(r *http.Request, form service.ClosureInput, errs []string, notice string) (*AbsencesPage, error) {
	cs, err := h.svc.Closures(r.Context(), actorFrom(r))
	if err != nil {
		return nil, err
	}
	return &AbsencesPage{CSRF: httpx.CSRFToken(r.Context()), Closures: cs, Form: form, Errors: errs, Notice: notice}, nil
}

func (h *configHandlers) absencesShow(w http.ResponseWriter, r *http.Request, form service.ClosureInput, errs []string) {
	p, err := h.absencesData(r, form, errs, "")
	if err != nil {
		h.fail(w, r, err)
		return
	}
	h.show(w, r, "admin_absences", "absences_panel", p, len(errs) > 0)
}

func (h *configHandlers) absencesDone(w http.ResponseWriter, r *http.Request, msg string) {
	h.succeed(w, r, pathAbsences, msg, "absences_panel", func(notice string) (any, error) {
		return h.absencesData(r, service.ClosureInput{}, nil, notice)
	})
}

func (h *configHandlers) absencesPage(w http.ResponseWriter, r *http.Request) {
	h.absencesShow(w, r, service.ClosureInput{}, nil)
}

func (h *configHandlers) absencesAdd(w http.ResponseWriter, r *http.Request) {
	in := service.ClosureInput{
		StartDate: r.PostFormValue("start_date"), StartTime: r.PostFormValue("start_time"),
		EndDate: r.PostFormValue("end_date"), EndTime: r.PostFormValue("end_time"),
		Reason: r.PostFormValue("reason"),
	}
	err := h.svc.AddClosure(r.Context(), actorFrom(r), in)
	if msgs := validation(err); msgs != nil {
		h.absencesShow(w, r, in, msgs)
		return
	}
	if err != nil {
		h.fail(w, r, err)
		return
	}
	h.absencesDone(w, r, "Absence ajoutée.")
}

func (h *configHandlers) absencesDelete(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		h.rd.Error(w, r, http.StatusNotFound, "")
		return
	}
	if err := h.svc.DeleteClosure(r.Context(), actorFrom(r), id); err != nil {
		h.fail(w, r, err)
		return
	}
	h.absencesDone(w, r, "Absence supprimée.")
}
