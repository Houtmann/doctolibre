package web

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"doctolibre/internal/domain"
	"doctolibre/internal/httpx"
	"doctolibre/internal/service"
)

// SeriesPlanLine est une séance proposée au patient. Value (RFC 3339, UTC) est reposté tel quel :
// le service revalide chaque créneau à la confirmation.
type SeriesPlanLine struct {
	Index int
	Start time.Time
	Value string
}

// SeriesPlanData est la donnée de la page public_series_plan et du fragment de proposition.
type SeriesPlanData struct {
	CSRF, Action, BackURL string
	Remaining             int
	Count                 string
	Weekdays              []WeekdayChoice
	WindowStart           string
	WindowEnd             string
	Instructions          string
	AckRequired           bool
	Ack                   bool
	Lines                 []SeriesPlanLine
	Errors                []string
	Notice                string
}

func planWeekdayChoices(selected []string) []WeekdayChoice {
	chosen := map[string]bool{}
	for _, w := range selected {
		chosen[w] = true
	}
	out := make([]WeekdayChoice, 0, 7)
	for i := range 7 {
		v := strconv.Itoa(i)
		out = append(out, WeekdayChoice{Value: v, Label: service.Capitalize(service.WeekdayName(i)), Checked: chosen[v]})
	}
	return out
}

// planContext retrouve la série du lien et vérifie que la planification est ouverte ; sinon 404 neutre.
func (h *publicHandlers) planContext(w http.ResponseWriter, r *http.Request) (service.SelfView, bool) {
	noStore(w)
	v, err := h.svc.SelfSchedule(r.Context(), r.PathValue("token"))
	if err != nil {
		h.fail(w, r, err)
		return v, false
	}
	return v, true
}

// planQuota prend un jeton du plafond horaire de propositions de la série (clé = id de série).
func (h *publicHandlers) planQuota(w http.ResponseWriter, r *http.Request, v service.SelfView) bool {
	res := h.lim.plan.Reserve(v.PlanID.String())
	if res.OK() {
		return true
	}
	httpx.LoggerFrom(r.Context()).Warn("plafond de propositions de séances atteint", slog.String("care_plan_id", v.PlanID.String()))
	h.lim.plan.Deny(w, r, res.RetryAfter(), "Vous avez demandé beaucoup de propositions. Réessayez dans un moment ou appelez le cabinet.")
	return false
}

func readSelfInput(r *http.Request) service.SelfInput {
	return service.SelfInput{
		Count:       strings.TrimSpace(r.PostFormValue("count")),
		Weekdays:    r.PostForm["weekdays"],
		WindowStart: strings.TrimSpace(r.PostFormValue("window_start")),
		WindowEnd:   strings.TrimSpace(r.PostFormValue("window_end")),
	}
}

// planData prépare la donnée commune à la page et au fragment.
func (h *publicHandlers) planData(r *http.Request, v service.SelfView, in service.SelfInput) SeriesPlanData {
	token := r.PathValue("token")
	return SeriesPlanData{
		CSRF: httpx.CSRFToken(r.Context()), Action: seriesURL(token) + "/plan", BackURL: seriesURL(token),
		Remaining: v.Remaining, Count: in.Count, Weekdays: planWeekdayChoices(in.Weekdays),
		WindowStart: in.WindowStart, WindowEnd: in.WindowEnd, Instructions: v.Instructions, AckRequired: v.AckRequired,
	}
}

func setLines(d *SeriesPlanData, starts []time.Time) {
	d.Lines = d.Lines[:0]
	for i, s := range starts {
		d.Lines = append(d.Lines, SeriesPlanLine{Index: i, Start: s, Value: s.UTC().Format(time.RFC3339)})
	}
}

// blockedPage : refus neutre du garde-fou d'absences (T054), identique aux autres parcours publics.
func (h *publicHandlers) blockedPage(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	phone := ""
	if p, err := h.svc.CarePlanByToken(r.Context(), token); err == nil {
		phone = p.Practice.Phone
	}
	renderBookingBlocked(h.rd, w, r, phone, seriesURL(token), "Retour à mes séances")
}

func (h *publicHandlers) renderPlan(w http.ResponseWriter, r *http.Request, status int, d SeriesPlanData) {
	w.Header().Add("Vary", "HX-Request")
	if httpx.IsHTMX(r) {
		h.rd.Partial(w, r, "public_series_plan_proposal", d)
		return
	}
	h.rd.PageStatus(w, r, status, "public_series_plan", d)
}

// planForm affiche le formulaire des préférences, préremplies depuis la série.
func (h *publicHandlers) planForm(w http.ResponseWriter, r *http.Request) {
	v, ok := h.planContext(w, r)
	if !ok {
		return
	}
	in := service.SelfInput{Count: strconv.Itoa(v.Remaining), Weekdays: v.Weekdays, WindowStart: v.WindowStart, WindowEnd: v.WindowEnd}
	h.rd.Page(w, r, "public_series_plan", h.planData(r, v, in))
}

// planPropose calcule la proposition ; rien n'est créé.
func (h *publicHandlers) planPropose(w http.ResponseWriter, r *http.Request) {
	v, ok := h.planContext(w, r)
	if !ok || !h.planQuota(w, r, v) {
		return
	}
	in := readSelfInput(r)
	d := h.planData(r, v, in)
	prop, err := h.svc.ProposeSelf(r.Context(), r.PathValue("token"), in)
	switch {
	case errors.Is(err, domain.ErrOnlineBookingBlocked):
		h.blockedPage(w, r)
		return
	case err != nil:
		if msgs := validation(err); msgs != nil {
			d.Errors = msgs
			h.renderPlan(w, r, http.StatusUnprocessableEntity, d)
			return
		}
		h.fail(w, r, err)
		return
	}
	setLines(&d, prop.Lines)
	switch {
	case len(prop.Lines) == 0:
		d.Errors = []string{"Aucun créneau disponible avec ces préférences. Élargissez les jours ou la plage horaire, ou appelez le cabinet."}
	case len(prop.Lines) < prop.Wanted:
		d.Notice = fmt.Sprintf("Seulement %d séance(s) sur %d ont pu être placées. Vous pouvez les valider et planifier le reste plus tard.", len(prop.Lines), prop.Wanted)
	}
	h.renderPlan(w, r, http.StatusOK, d)
}

// planAdjust retire une séance (« remove ») ou demande le créneau suivant d'une séance (« next »).
// Sans JS, la page entière est rendue ; avec htmx, seul le fragment de proposition.
func (h *publicHandlers) planAdjust(w http.ResponseWriter, r *http.Request) {
	v, ok := h.planContext(w, r)
	if !ok {
		return
	}
	in := readSelfInput(r)
	d := h.planData(r, v, in)
	d.Ack = r.PostFormValue("instructions_ack") == "on"
	starts, err := service.ParseSelfStarts(r.PostForm["line"])
	if err != nil {
		d.Errors = validation(err)
		h.renderPlan(w, r, http.StatusUnprocessableEntity, d)
		return
	}
	if len(starts) > v.Remaining {
		starts = starts[:v.Remaining]
	}
	switch {
	case r.PostFormValue("remove") != "":
		i, err := strconv.Atoi(r.PostFormValue("remove"))
		if err == nil && i >= 0 && i < len(starts) {
			starts = append(starts[:i:i], starts[i+1:]...)
		}
	case r.PostFormValue("next") != "":
		i, err := strconv.Atoi(r.PostFormValue("next"))
		if err != nil || i < 0 || i >= len(starts) {
			break
		}
		if !h.planQuota(w, r, v) {
			return
		}
		prop, found, err := h.svc.NextSelf(r.Context(), r.PathValue("token"), in, starts, i)
		switch {
		case errors.Is(err, domain.ErrOnlineBookingBlocked):
			h.blockedPage(w, r)
			return
		case err != nil:
			if msgs := validation(err); msgs != nil {
				d.Errors = msgs
				break
			}
			h.fail(w, r, err)
			return
		default:
			starts = prop.Lines
			if !found {
				d.Notice = "Aucun autre créneau disponible pour cette séance : retirez-la ou gardez celle-ci."
			}
		}
	}
	setLines(&d, starts)
	if len(starts) == 0 && len(d.Errors) == 0 {
		d.Notice = "Toutes les séances ont été retirées. Relancez une proposition pour en choisir d'autres."
	}
	h.renderPlan(w, r, http.StatusOK, d)
}

// planConfirm crée toutes les séances proposées, ou aucune.
func (h *publicHandlers) planConfirm(w http.ResponseWriter, r *http.Request) {
	v, ok := h.planContext(w, r)
	if !ok {
		return
	}
	token := r.PathValue("token")
	in := readSelfInput(r)
	d := h.planData(r, v, in)
	d.Ack = r.PostFormValue("instructions_ack") == "on"
	starts, err := service.ParseSelfStarts(r.PostForm["line"])
	if err == nil {
		var n int
		n, err = h.svc.ConfirmSelf(r.Context(), token, service.SelfConfirm{Starts: starts, InstructionsAck: d.Ack})
		if err == nil {
			h.rd.SetFlash(w, "success", fmt.Sprintf("%d séance(s) planifiée(s). Un message récapitulatif vous est envoyé.", n))
			httpx.Redirect(w, r, seriesURL(token))
			return
		}
	}
	var conflict *service.LineConflict
	switch {
	case errors.Is(err, domain.ErrOnlineBookingBlocked):
		h.blockedPage(w, r)
	case errors.As(err, &conflict):
		// Rien n'a été créé : nouvelle proposition pour le même nombre de séances.
		in.Count = strconv.Itoa(len(starts))
		d.Count = in.Count
		d.Notice = "Un créneau vient d'être pris : aucune séance n'a été enregistrée. Voici une nouvelle proposition."
		if prop, perr := h.svc.ProposeSelf(r.Context(), token, in); perr == nil {
			setLines(&d, prop.Lines)
		} else if msgs := validation(perr); msgs != nil {
			d.Errors = msgs
		} else {
			h.fail(w, r, perr)
			return
		}
		h.renderPlan(w, r, http.StatusConflict, d)
	default:
		if msgs := validation(err); msgs != nil {
			d.Errors = msgs
			setLines(&d, starts)
			h.renderPlan(w, r, http.StatusUnprocessableEntity, d)
			return
		}
		h.fail(w, r, err)
	}
}
