package web

import (
	"net/http"
	"strconv"

	"github.com/google/uuid"

	"doctolibre/internal/httpx"
	"doctolibre/internal/service"
)

const pathBulkCancel = pathAbsences + "/cancel"

// BulkCancelPage est la donnée de la page admin_absences_cancel (aperçu puis confirmation).
type BulkCancelPage struct {
	CSRF    string
	Target  string // praticien visé (vide = son propre agenda)
	Q       string
	Form    service.ClosureInput
	Preview service.BulkCancelPreview
}

func closureInputOf(r *http.Request) service.ClosureInput {
	return service.ClosureInput{
		StartDate: r.FormValue("start_date"), StartTime: r.FormValue("start_time"),
		EndDate: r.FormValue("end_date"), EndTime: r.FormValue("end_time"),
		Reason: r.FormValue("reason"),
	}
}

// bulkInvalid réaffiche la page des absences avec les erreurs du formulaire d'annulation groupée.
func (h *configHandlers) bulkInvalid(w http.ResponseWriter, r *http.Request, in service.ClosureInput, errs []string) {
	p, err := h.absencesData(r, service.ClosureInput{}, nil, "")
	if err != nil {
		h.fail(w, r, err)
		return
	}
	p.Bulk, p.BulkErrors = in, errs
	h.rd.PageStatus(w, r, http.StatusUnprocessableEntity, "admin_absences", p)
}

// bulkPreview (GET) : liste les rendez-vous touchés, sans rien modifier.
func (h *configHandlers) bulkPreview(w http.ResponseWriter, r *http.Request) {
	in := closureInputOf(r)
	pv, err := h.cal.CancelPreview(r.Context(), actorFrom(r), in)
	if msgs := validation(err); msgs != nil {
		h.bulkInvalid(w, r, in, msgs)
		return
	}
	if err != nil {
		h.fail(w, r, err)
		return
	}
	target := ""
	if id := targetFrom(r.Context()); id != uuid.Nil {
		target = id.String()
	}
	h.rd.Page(w, r, "admin_absences_cancel", &BulkCancelPage{
		CSRF: httpx.CSRFToken(r.Context()), Target: target, Q: targetQuery(r), Form: in, Preview: pv,
	})
}

// bulkConfirm (POST) : crée l'absence, annule les rendez-vous et prévient les patients.
func (h *configHandlers) bulkConfirm(w http.ResponseWriter, r *http.Request) {
	in := closureInputOf(r)
	n, err := h.cal.CancelRange(r.Context(), actorFrom(r), in)
	if msgs := validation(err); msgs != nil {
		h.bulkInvalid(w, r, in, msgs)
		return
	}
	if err != nil {
		h.fail(w, r, err)
		return
	}
	msg := "Absence enregistrée ; aucun rendez-vous à annuler."
	switch {
	case n == 1:
		msg = "Absence enregistrée ; 1 rendez-vous annulé, le patient est prévenu."
	case n > 1:
		msg = "Absence enregistrée ; " + strconv.Itoa(n) + " rendez-vous annulés, les patients sont prévenus."
	}
	h.rd.SetFlash(w, "success", msg)
	httpx.Redirect(w, r, pathAbsences+targetQuery(r))
}
