package web

import (
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"doctolibre/internal/domain"
	"doctolibre/internal/httpx"
	"doctolibre/internal/service"
	"doctolibre/internal/store/sqlcgen"
)

const (
	pathPrivacy = "/admin/privacy"
	pathAudit   = "/admin/audit"
	// maxAuditAction borne le filtre d'action relu dans la requête.
	maxAuditAction = 64
)

// privacyRoutes enregistre les réglages RGPD (administrateur uniquement).
func (d Deps) privacyRoutes(mux *http.ServeMux, rd *Renderer, logger *slog.Logger) {
	h := &privacyHandlers{svc: d.Privacy, rd: rd, logger: logger}
	admin := d.Auth.RequireUser(domain.RoleAdmin)
	mux.Handle("GET "+pathPrivacy, admin(http.HandlerFunc(h.show)))
	mux.Handle("POST "+pathPrivacy, admin(http.HandlerFunc(h.save)))
}

// auditRoutes enregistre /admin/audit (administrateur uniquement).
func (d Deps) auditRoutes(mux *http.ServeMux, rd *Renderer, logger *slog.Logger) {
	h := &privacyHandlers{audit: d.Audit, rd: rd, logger: logger}
	mux.Handle("GET "+pathAudit, d.Auth.RequireUser(domain.RoleAdmin)(http.HandlerFunc(h.auditList)))
}

// privacyPublicRoutes enregistre la page d'information des patients.
func (d Deps) privacyPublicRoutes(mux *http.ServeMux, rd *Renderer, logger *slog.Logger) {
	h := &privacyHandlers{svc: d.Privacy, rd: rd, logger: logger}
	mux.HandleFunc("GET /p/{practice}/privacy", h.notice)
}

type privacyHandlers struct {
	svc    *service.Privacy
	audit  *service.Audit
	rd     *Renderer
	logger *slog.Logger
}

func (h *privacyHandlers) fail(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, domain.ErrNotFound) {
		h.rd.Error(w, r, http.StatusNotFound, "")
		return
	}
	a := actorFrom(r)
	httpx.LoggerFrom(r.Context()).Error("confidentialité en échec",
		slog.String("user_id", a.UserID.String()), slog.String("err", err.Error()))
	h.rd.Error(w, r, http.StatusInternalServerError, "")
}

// PrivacyForm est la saisie des réglages de conservation.
type PrivacyForm struct {
	OutboxDays, PatientYears, AuditDays, Contact string
	PurgeEnabled                                 bool
}

// PrivacyPage est la donnée de la page admin_privacy.
type PrivacyPage struct {
	Slug    string
	Form    PrivacyForm
	Errors  []string
	Preview service.PurgePreview
}

func (h *privacyHandlers) page(r *http.Request, form *PrivacyForm, errs []string) (PrivacyPage, error) {
	a := actorFrom(r)
	set, err := h.svc.Settings(r.Context(), a)
	if err != nil {
		return PrivacyPage{}, err
	}
	pv, err := h.svc.Preview(r.Context(), a)
	if err != nil {
		return PrivacyPage{}, err
	}
	p := PrivacyPage{
		Form: PrivacyForm{
			OutboxDays: strconv.Itoa(set.OutboxDays), PatientYears: strconv.Itoa(set.PatientYears), AuditDays: strconv.Itoa(set.AuditDays),
			Contact: set.Contact, PurgeEnabled: set.PurgeEnabled,
		},
		Errors: errs, Preview: pv, Slug: set.Slug,
	}
	if form != nil {
		p.Form = *form
	}
	return p, nil
}

func (h *privacyHandlers) show(w http.ResponseWriter, r *http.Request) {
	p, err := h.page(r, nil, nil)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	h.rd.Page(w, r, "admin_privacy", p)
}

func (h *privacyHandlers) save(w http.ResponseWriter, r *http.Request) {
	v := func(k string) string { return strings.TrimSpace(r.PostFormValue(k)) }
	f := PrivacyForm{
		OutboxDays: v("retention_outbox_days"), PatientYears: v("retention_patient_years"), AuditDays: v("retention_audit_days"),
		Contact: v("privacy_contact"), PurgeEnabled: r.PostFormValue("patient_purge_enabled") == "1",
	}
	days, e1 := strconv.Atoi(f.OutboxDays)
	years, e2 := strconv.Atoi(f.PatientYears)
	audit, e3 := strconv.Atoi(f.AuditDays)
	var err error
	var msgs []string
	if e1 != nil || e2 != nil || e3 != nil {
		msgs = []string{"Les durées de conservation doivent être des nombres entiers."}
	} else {
		err = h.svc.UpdateSettings(r.Context(), actorFrom(r), service.PrivacySettings{
			OutboxDays: days, PatientYears: years, AuditDays: audit, PurgeEnabled: f.PurgeEnabled, Contact: f.Contact,
		})
		msgs = validation(err)
	}
	if msgs != nil {
		p, perr := h.page(r, &f, msgs)
		if perr != nil {
			h.fail(w, r, perr)
			return
		}
		h.rd.PageStatus(w, r, http.StatusUnprocessableEntity, "admin_privacy", p)
		return
	}
	if err != nil {
		h.fail(w, r, err)
		return
	}
	h.rd.SetFlash(w, "success", "Réglages enregistrés.")
	httpx.Redirect(w, r, pathPrivacy)
}

// AuditRowView est une ligne affichée du journal.
type AuditRowView struct {
	At                        time.Time
	User, Action, Entity      string
	EntityID, EntityURL, Meta string
}

// AuditPageData est la donnée de la page admin_audit.
type AuditPageData struct {
	Action  string
	Rows    []AuditRowView
	NextURL string
}

func (h *privacyHandlers) auditList(w http.ResponseWriter, r *http.Request) {
	action := strings.TrimSpace(r.URL.Query().Get("action"))
	if len(action) > maxAuditAction {
		action = action[:maxAuditAction]
	}
	before, _ := strconv.ParseInt(r.URL.Query().Get("before"), 10, 64)
	if before < 0 {
		before = 0
	}
	res, err := h.audit.List(r.Context(), actorFrom(r), action, before)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	data := AuditPageData{Action: action}
	for _, e := range res.Entries {
		row := AuditRowView{At: e.At, User: e.User, Action: e.Action, Entity: e.Entity, Meta: e.Meta}
		if row.User == "" {
			row.User = e.Actor
		}
		if e.EntityID.String() != "00000000-0000-0000-0000-000000000000" {
			row.EntityID = e.EntityID.String()
			switch e.Entity {
			case "patient":
				row.EntityURL = pathPatients + "/" + row.EntityID
			case "appointment":
				row.EntityURL = "/admin/appointments/" + row.EntityID
			}
		}
		data.Rows = append(data.Rows, row)
	}
	if res.Next != 0 {
		data.NextURL = pathAudit + "?" + url.Values{"action": {action}, "before": {strconv.FormatInt(res.Next, 10)}}.Encode()
	}
	h.rd.Page(w, r, "admin_audit", data)
}

// PrivacyNoticeData est la donnée de la page publique public_privacy.
type PrivacyNoticeData struct {
	Practice     sqlcgen.Practice
	Contact      string
	OutboxDays   int
	PatientYears int
}

func (h *privacyHandlers) notice(w http.ResponseWriter, r *http.Request) {
	pc, err := h.svc.PublicNotice(r.Context(), r.PathValue("practice"))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	contact := pc.PrivacyContact
	if contact == "" {
		contact = pc.Phone
	}
	h.rd.Page(w, r, "public_privacy", PrivacyNoticeData{
		Practice: pc, Contact: contact, OutboxDays: int(pc.RetentionOutboxDays), PatientYears: int(pc.RetentionPatientYears),
	})
}
