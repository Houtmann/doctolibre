package web

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/google/uuid"

	"doctolibre/internal/auth"
	"doctolibre/internal/domain"
	"doctolibre/internal/httpx"
	"doctolibre/internal/service"
	"doctolibre/internal/store/sqlcgen"
)

const (
	pathPatients       = "/admin/patients"
	mergeCandidatesMax = 10
)

// patientRoutes enregistre les fiches patients (tous les rôles du cabinet).
func (d Deps) patientRoutes(mux *http.ServeMux, rd *Renderer, logger *slog.Logger) {
	h := &patientHandlers{svc: d.Patients, plans: d.CarePlans, rd: rd, logger: logger}
	require := d.Auth.RequireUser(domain.RoleAdmin, domain.RolePractitioner, domain.RoleSecretary)
	handle := func(pattern string, f http.HandlerFunc) { mux.Handle(pattern, require(f)) }

	handle("GET "+pathPatients, h.list)
	handle("GET "+pathPatients+"/new", h.newForm)
	handle("POST "+pathPatients, h.create)
	handle("GET "+pathPatients+"/{id}", h.detail)
	handle("POST "+pathPatients+"/{id}", h.update)
	handle("POST "+pathPatients+"/{id}/merge", h.merge)
	// Export et suppression (droits RGPD) : pas pour le secrétariat.
	clinical := d.Auth.RequireUser(domain.RoleAdmin, domain.RolePractitioner)
	mux.Handle("GET "+pathPatients+"/{id}/export.json", clinical(http.HandlerFunc(h.exportJSON)))
	mux.Handle("GET "+pathPatients+"/{id}/export.csv", clinical(http.HandlerFunc(h.exportCSV)))
	mux.Handle("POST "+pathPatients+"/{id}/delete", clinical(http.HandlerFunc(h.erase)))
}

type patientHandlers struct {
	svc    *service.Patients
	plans  *service.CarePlans // optionnel
	rd     *Renderer
	logger *slog.Logger
}

func (h *patientHandlers) fail(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, domain.ErrNotFound):
		h.rd.Error(w, r, http.StatusNotFound, "")
	case errors.Is(err, domain.ErrForbidden):
		h.rd.Error(w, r, http.StatusForbidden, "Accès refusé.")
	default:
		a := actorFrom(r)
		httpx.LoggerFrom(r.Context()).Error("fiches patients en échec",
			slog.String("user_id", a.UserID.String()), slog.String("err", err.Error()))
		h.rd.Error(w, r, http.StatusInternalServerError, "")
	}
}

// PatientForm est la saisie d'une fiche (création ou édition).
type PatientForm struct {
	FirstName, LastName, Phone, Email, BirthDate, Notes string
	SMSOptIn, EmailOptIn                                string // "1" si cochée
	Force                                               bool
	Invalid                                             map[string]bool // champs en erreur (clé = nom du champ)
}

func (f PatientForm) details() service.PatientDetails {
	return service.PatientDetails{
		FirstName: f.FirstName, LastName: f.LastName, Phone: f.Phone, Email: f.Email, BirthDate: f.BirthDate,
		SMSOptIn: f.SMSOptIn == "1", EmailOptIn: f.EmailOptIn == "1",
	}
}

func readPatientForm(r *http.Request) PatientForm {
	v := func(k string) string { return strings.TrimSpace(r.PostFormValue(k)) }
	f := PatientForm{
		FirstName: v("first_name"), LastName: v("last_name"), Phone: v("phone"), Email: v("email"),
		BirthDate: v("birth_date"), Notes: strings.TrimSpace(r.PostFormValue("notes_admin")),
		Force: r.PostFormValue("force") == "1",
	}
	if r.PostFormValue("sms_opt_in") == "1" {
		f.SMSOptIn = "1"
	}
	if r.PostFormValue("email_opt_in") == "1" {
		f.EmailOptIn = "1"
	}
	return f
}

func formOf(p sqlcgen.Patient) PatientForm {
	f := PatientForm{FirstName: p.FirstName, LastName: p.LastName, Phone: p.Phone.String, Email: p.Email.String, Notes: p.NotesAdmin}
	if p.BirthDate.Valid {
		f.BirthDate = p.BirthDate.Time.Format("2006-01-02")
	}
	if p.SmsOptIn {
		f.SMSOptIn = "1"
	}
	if p.EmailOptIn {
		f.EmailOptIn = "1"
	}
	return f
}

// PatientRowView est une ligne de la liste des patients.
type PatientRowView struct {
	ID, Name, Phone, Email string
}

func rowOf(p sqlcgen.Patient) PatientRowView {
	return PatientRowView{ID: p.ID.String(), Name: p.LastName + " " + p.FirstName, Phone: p.Phone.String, Email: p.Email.String}
}

// PatientsPage est la donnée de la page admin_patients.
type PatientsPage struct {
	Query   string
	Rows    []PatientRowView
	NextURL string
	NewURL  string
}

func (h *patientHandlers) list(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	res, err := h.svc.List(r.Context(), actorFrom(r), q, r.URL.Query().Get("after"))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	p := PatientsPage{Query: q, NewURL: pathPatients + "/new"}
	for _, pt := range res.Patients {
		p.Rows = append(p.Rows, rowOf(pt))
	}
	if res.Next != "" {
		p.NextURL = pathPatients + "?" + url.Values{"q": {q}, "after": {res.Next}}.Encode()
	}
	h.rd.Page(w, r, "admin_patients", p)
}

// PatientNewPage est la donnée de la page admin_patient_new.
type PatientNewPage struct {
	Form       PatientForm
	Errors     []string
	Duplicates []PatientRowView
}

func (h *patientHandlers) newForm(w http.ResponseWriter, r *http.Request) {
	h.rd.Page(w, r, "admin_patient_new", PatientNewPage{Form: PatientForm{SMSOptIn: "1", EmailOptIn: "1"}})
}

func (h *patientHandlers) create(w http.ResponseWriter, r *http.Request) {
	f := readPatientForm(r)
	p, dups, err := h.svc.Create(r.Context(), actorFrom(r), f.details(), f.Notes, f.Force)
	if msgs := validation(err); msgs != nil {
		f.Invalid = invalidFields(err)
		h.rd.PageStatus(w, r, http.StatusUnprocessableEntity, "admin_patient_new", PatientNewPage{Form: f, Errors: msgs})
		return
	}
	if err != nil {
		h.fail(w, r, err)
		return
	}
	if len(dups) > 0 {
		page := PatientNewPage{Form: f}
		for _, d := range dups {
			page.Duplicates = append(page.Duplicates, rowOf(d))
		}
		h.rd.Page(w, r, "admin_patient_new", page)
		return
	}
	h.rd.SetFlash(w, "success", "Fiche patient créée.")
	httpx.Redirect(w, r, pathPatients+"/"+p.ID.String())
}

// PatientPage est la donnée de la page admin_patient.
type PatientPage struct {
	ID         string
	Name       string
	Form       PatientForm
	Errors     []string
	Upcoming   []service.PatientAppt
	History    []service.PatientAppt
	NoShows    int
	Plans      []service.PlanView
	MergeQuery string
	Candidates []PatientRowView
	CanManage  bool // export et suppression (administrateur, praticien)
}

func (h *patientHandlers) page(r *http.Request, id uuid.UUID, form *PatientForm, errs []string) (PatientPage, error) {
	a := actorFrom(r)
	d, err := h.svc.Detail(r.Context(), a, id)
	if err != nil {
		return PatientPage{}, err
	}
	u, _ := auth.UserFrom(r.Context())
	p := PatientPage{
		CanManage: u.Role != domain.RoleSecretary,
		ID:        d.Patient.ID.String(), Name: d.Patient.FirstName + " " + d.Patient.LastName,
		Form: formOf(d.Patient), Errors: errs, Upcoming: d.Upcoming, History: d.History, NoShows: d.NoShows,
	}
	if h.plans != nil {
		if p.Plans, err = h.plans.ForPatient(r.Context(), a, id); err != nil {
			return PatientPage{}, err
		}
	}
	if form != nil {
		p.Form = *form
	}
	if mq := strings.TrimSpace(r.URL.Query().Get("mq")); mq != "" {
		p.MergeQuery = mq
		res, err := h.svc.List(r.Context(), a, mq, "")
		if err != nil {
			return PatientPage{}, err
		}
		for _, c := range res.Patients {
			if c.ID != id && len(p.Candidates) < mergeCandidatesMax {
				p.Candidates = append(p.Candidates, rowOf(c))
			}
		}
	}
	return p, nil
}

func (h *patientHandlers) detail(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		h.rd.Error(w, r, http.StatusNotFound, "")
		return
	}
	p, err := h.page(r, id, nil, nil)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	if err := h.svc.AuditView(r.Context(), actorFrom(r), id); err != nil {
		h.fail(w, r, err)
		return
	}
	h.rd.Page(w, r, "admin_patient", p)
}

func (h *patientHandlers) update(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		h.rd.Error(w, r, http.StatusNotFound, "")
		return
	}
	f := readPatientForm(r)
	_, err := h.svc.Update(r.Context(), actorFrom(r), id, f.details(), f.Notes)
	if msgs := validation(err); msgs != nil {
		f.Invalid = invalidFields(err)
		p, perr := h.page(r, id, &f, msgs)
		if perr != nil {
			h.fail(w, r, perr)
			return
		}
		h.rd.PageStatus(w, r, http.StatusUnprocessableEntity, "admin_patient", p)
		return
	}
	if err != nil {
		h.fail(w, r, err)
		return
	}
	h.rd.SetFlash(w, "success", "Fiche enregistrée.")
	httpx.Redirect(w, r, pathPatients+"/"+id.String())
}

// merge fusionne la fiche {id} (le doublon) dans la fiche « into » (query ou formulaire).
func (h *patientHandlers) merge(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		h.rd.Error(w, r, http.StatusNotFound, "")
		return
	}
	into, err := uuid.Parse(r.FormValue("into"))
	if err != nil {
		p, perr := h.page(r, id, nil, []string{"Choisissez la fiche à conserver."})
		if perr != nil {
			h.fail(w, r, perr)
			return
		}
		h.rd.PageStatus(w, r, http.StatusUnprocessableEntity, "admin_patient", p)
		return
	}
	n, err := h.svc.Merge(r.Context(), actorFrom(r), id, into)
	if msgs := validation(err); msgs != nil {
		p, perr := h.page(r, id, nil, msgs)
		if perr != nil {
			h.fail(w, r, perr)
			return
		}
		h.rd.PageStatus(w, r, http.StatusUnprocessableEntity, "admin_patient", p)
		return
	}
	if err != nil {
		h.fail(w, r, err)
		return
	}
	httpx.LoggerFrom(r.Context()).Info("patients fusionnés",
		slog.String("from", id.String()), slog.String("into", into.String()), slog.Int64("appointments", n))
	h.rd.SetFlash(w, "success", "Fiches fusionnées : les rendez-vous ont été rattachés à cette fiche.")
	httpx.Redirect(w, r, pathPatients+"/"+into.String())
}

// exportJSON télécharge toutes les données du patient (JSON).
func (h *patientHandlers) exportJSON(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		h.rd.Error(w, r, http.StatusNotFound, "")
		return
	}
	exp, err := h.svc.Export(r.Context(), actorFrom(r), id, "json")
	if err != nil {
		h.fail(w, r, err)
		return
	}
	setDownloadHeaders(w, "application/json; charset=utf-8", "patient-"+id.String()+".json")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(exp); err != nil {
		httpx.LoggerFrom(r.Context()).Error("export JSON en échec", slog.String("patient_id", id.String()), slog.String("err", err.Error()))
	}
}

// exportCSV télécharge les coordonnées et les rendez-vous du patient (CSV).
func (h *patientHandlers) exportCSV(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		h.rd.Error(w, r, http.StatusNotFound, "")
		return
	}
	exp, err := h.svc.Export(r.Context(), actorFrom(r), id, "csv")
	if err != nil {
		h.fail(w, r, err)
		return
	}
	setDownloadHeaders(w, "text/csv; charset=utf-8", "patient-"+id.String()+".csv")
	if err := exp.WriteCSV(w); err != nil {
		httpx.LoggerFrom(r.Context()).Error("export CSV en échec", slog.String("patient_id", id.String()), slog.String("err", err.Error()))
	}
}

// setDownloadHeaders : téléchargement jamais mis en cache (données personnelles).
func setDownloadHeaders(w http.ResponseWriter, contentType, filename string) {
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
}

// erase supprime ou anonymise la fiche (confirmation « confirm=1 » obligatoire).
func (h *patientHandlers) erase(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		h.rd.Error(w, r, http.StatusNotFound, "")
		return
	}
	var err error
	msgs := []string(nil)
	if r.PostFormValue("confirm") != "1" {
		msgs = []string{"Cochez la case pour confirmer la suppression."}
	} else {
		_, err = h.svc.Erase(r.Context(), actorFrom(r), id)
		msgs = validation(err)
	}
	if msgs != nil {
		p, perr := h.page(r, id, nil, msgs)
		if perr != nil {
			h.fail(w, r, perr)
			return
		}
		h.rd.PageStatus(w, r, http.StatusUnprocessableEntity, "admin_patient", p)
		return
	}
	if err != nil {
		h.fail(w, r, err)
		return
	}
	h.rd.SetFlash(w, "success", "Fiche patient supprimée : ses données personnelles ont été effacées.")
	httpx.Redirect(w, r, pathPatients)
}
