package web

import (
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/google/uuid"

	"doctolibre/internal/domain"
	"doctolibre/internal/httpx"
	"doctolibre/internal/service"
)

// Chemins des statistiques de séances (T073).
const (
	pathStats    = "/admin/stats"
	pathStatsCSV = "/admin/stats.csv"
)

// statsRoutes enregistre la page et l'export CSV : administrateur et praticien
// (le secrétariat reçoit un 403 de RequireUser ; le service borne un praticien à lui-même).
func (d Deps) statsRoutes(mux *http.ServeMux, rd *Renderer, logger *slog.Logger) {
	h := &statsHandlers{svc: d.Stats, rd: rd, logger: logger}
	require := d.Auth.RequireUser(domain.RoleAdmin, domain.RolePractitioner)
	mux.Handle("GET "+pathStats, require(http.HandlerFunc(h.show)))
	mux.Handle("GET "+pathStatsCSV, require(http.HandlerFunc(h.csv)))
}

type statsHandlers struct {
	svc    *service.Stats
	rd     *Renderer
	logger *slog.Logger
}

// StatsPage est la donnée de la page admin_stats.
type StatsPage struct {
	Report service.StatsReport
	// From, To, Practitioner : valeurs du formulaire (la saisie, ou la période effective).
	From, To, Practitioner string
	Errors                 []string
	CSVURL                 string
	MaxDays                int
}

func (h *statsHandlers) params(r *http.Request) (service.StatsParams, bool) {
	q := r.URL.Query()
	p := service.StatsParams{From: strings.TrimSpace(q.Get("from")), To: strings.TrimSpace(q.Get("to"))}
	if raw := strings.TrimSpace(q.Get(practitionerParam)); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			return p, false
		}
		p.Practitioner = id
	}
	return p, true
}

func statsQuery(from, to string, practitioner uuid.UUID) string {
	q := url.Values{"from": {from}, "to": {to}}
	if practitioner != uuid.Nil {
		q.Set(practitionerParam, practitioner.String())
	}
	return q.Encode()
}

func (h *statsHandlers) fail(w http.ResponseWriter, r *http.Request, err error) {
	if msgs := validation(err); msgs != nil {
		h.rd.Error(w, r, http.StatusBadRequest, msgs[0])
		return
	}
	switch {
	case errors.Is(err, domain.ErrNotFound):
		h.rd.Error(w, r, http.StatusNotFound, "")
	case errors.Is(err, domain.ErrForbidden):
		h.rd.Error(w, r, http.StatusForbidden, "Accès refusé.")
	default:
		a := actorFrom(r)
		httpx.LoggerFrom(r.Context()).Error("statistiques en échec",
			slog.String("user_id", a.UserID.String()), slog.String("err", err.Error()))
		h.rd.Error(w, r, http.StatusInternalServerError, "")
	}
}

func (h *statsHandlers) show(w http.ResponseWriter, r *http.Request) {
	p, ok := h.params(r)
	if !ok {
		h.rd.Error(w, r, http.StatusNotFound, "")
		return
	}
	a := actorFrom(r)
	rep, err := h.svc.Report(r.Context(), a, p)
	if msgs := validation(err); msgs != nil {
		// Période invalide : le formulaire est ré-affiché avec la saisie et le message.
		pg := StatsPage{From: p.From, To: p.To, Errors: msgs, MaxDays: service.MaxStatsDays}
		if p.Practitioner != uuid.Nil {
			pg.Practitioner = p.Practitioner.String()
		}
		if def, derr := h.svc.Report(r.Context(), a, service.StatsParams{Practitioner: p.Practitioner}); derr == nil {
			pg.Report = service.StatsReport{Choices: def.Choices}
		}
		h.rd.PageStatus(w, r, http.StatusUnprocessableEntity, "admin_stats", pg)
		return
	}
	if err != nil {
		h.fail(w, r, err)
		return
	}
	pg := StatsPage{Report: rep, From: rep.From, To: rep.To, MaxDays: service.MaxStatsDays,
		CSVURL: pathStatsCSV + "?" + statsQuery(rep.From, rep.To, rep.Practitioner)}
	if rep.Practitioner != uuid.Nil {
		pg.Practitioner = rep.Practitioner.String()
	}
	h.rd.Page(w, r, "admin_stats", pg)
}

func (h *statsHandlers) csv(w http.ResponseWriter, r *http.Request) {
	p, ok := h.params(r)
	if !ok {
		h.rd.Error(w, r, http.StatusNotFound, "")
		return
	}
	// Le CSV est construit en mémoire : les en-têtes de téléchargement ne sont posés
	// qu'une fois l'accès, la période et l'audit passés (sinon page d'erreur HTML).
	var buf strings.Builder
	rep, err := h.svc.ExportCSV(r.Context(), actorFrom(r), p, &buf)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	setDownloadHeaders(w, "text/csv; charset=utf-8", "seances-"+rep.From+"_"+rep.To+".csv")
	if _, err := w.Write([]byte(buf.String())); err != nil {
		httpx.LoggerFrom(r.Context()).Error("export CSV des statistiques en échec", slog.String("err", err.Error()))
	}
}
