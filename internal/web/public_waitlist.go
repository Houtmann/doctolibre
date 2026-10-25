package web

import (
	"errors"
	"fmt"
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

// waitlistDefaultSpanDays : durée proposée par défaut à la fenêtre souhaitée.
const waitlistDefaultSpanDays = 30

type waitlistHandlers struct {
	svc     *service.Waitlist
	booking *service.Booking
	rd      *Renderer
	logger  *slog.Logger
}

// waitlistRoutes enregistre l'inscription en ligne à la liste d'attente (avec code
// de vérification) et le lien de réservation d'un créneau libéré. Le lien /w/{jeton}
// est un secret de 256 bits, mais pas de sondage gratuit : limité par IP.
func (d Deps) waitlistRoutes(mux *http.ServeMux, rd *Renderer, logger *slog.Logger) {
	h := &waitlistHandlers{svc: d.Waitlist, booking: d.Booking, rd: rd, logger: logger}
	search, book, offer := d.pub.search.Middleware(), d.pub.book.Middleware(), d.pub.offer.Middleware()
	const base = "/p/{practice}/{practitioner}/waitlist"
	mux.Handle("GET "+base, httpx.Chain(http.HandlerFunc(h.form), search))
	mux.Handle("POST "+base, httpx.Chain(http.HandlerFunc(h.submit), book))
	mux.Handle("POST "+base+"/confirm", httpx.Chain(http.HandlerFunc(h.confirm), book))
	mux.Handle("GET /w/{token}", httpx.Chain(http.HandlerFunc(h.offer), offer))
	mux.Handle("POST /w/{token}", httpx.Chain(http.HandlerFunc(h.claim), offer))
	// Désinscription (T097) : GET n'écrit rien, POST désinscrit. Même limiteur que /w/{jeton}.
	mux.Handle("GET /wl/{token}", httpx.Chain(http.HandlerFunc(h.leaveForm), offer))
	mux.Handle("POST /wl/{token}", httpx.Chain(http.HandlerFunc(h.leave), offer))
}

func (h *waitlistHandlers) fail(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, domain.ErrNotFound):
		h.rd.Error(w, r, http.StatusNotFound, "")
	case errors.Is(err, service.ErrBookingClosed):
		h.rd.Error(w, r, http.StatusServiceUnavailable, bookingClosedMessage(err))
	default:
		httpx.LoggerFrom(r.Context()).Error("liste d'attente en échec", slog.String("err", err.Error()))
		h.rd.Error(w, r, http.StatusInternalServerError, "")
	}
}

// WaitlistValues est la saisie conservée du formulaire d'inscription.
type WaitlistValues struct {
	BookingForm
	Earliest, Latest, TimeFrom, TimeTo string
	Weekdays                           []string
}

// WaitlistFormData est la donnée de la page public_waitlist.
type WaitlistFormData struct {
	Channels               service.Channels // canaux proposés pour recevoir le code (T086)
	Practice, Practitioner string
	TypeName               string
	TypeID                 string
	Form                   WaitlistValues
	Weekdays               []WeekdayChoice
	Today                  string
	Action, BackURL        string
	Errors                 []string
}

// WaitlistVerifyData est la donnée de la page public_waitlist_verify.
type WaitlistVerifyData struct {
	Practice, Practitioner string
	TypeName               string
	TypeID                 string
	Form                   WaitlistValues
	ChallengeID            string
	Channel                string
	Destination            string
	Action, ResendURL      string
	BackURL                string
	Errors                 []string
}

func readWaitlistForm(r *http.Request) WaitlistValues {
	return WaitlistValues{
		BookingForm: readBookingForm(r),
		Earliest:    formValue(r, "earliest"), Latest: formValue(r, "latest"),
		TimeFrom: formValue(r, "time_from"), TimeTo: formValue(r, "time_to"),
		Weekdays: r.PostForm["weekdays"],
	}
}

func (v WaitlistValues) window(practitioner, typ uuid.UUID) service.WindowInput {
	return service.WindowInput{
		PractitionerID: practitioner, TypeID: typ, Earliest: v.Earliest, Latest: v.Latest,
		Weekdays: v.Weekdays, From: v.TimeFrom, To: v.TimeTo,
	}
}

func weekdayChoices(chosen []string) []WeekdayChoice {
	set := map[string]bool{}
	for _, c := range chosen {
		set[c] = true
	}
	out := make([]WeekdayChoice, 0, 7)
	for i := range 7 {
		val := fmt.Sprint(i)
		out = append(out, WeekdayChoice{Value: val, Label: service.Capitalize(service.WeekdayName(i)), Checked: set[val]})
	}
	return out
}

// target résout cabinet, praticien et motif de la requête ; ok=false si la réponse est écrite.
func (h *waitlistHandlers) target(w http.ResponseWriter, r *http.Request) (t service.Target, ok bool) {
	typeID, err := uuid.Parse(r.FormValue("type"))
	if err != nil {
		httpx.Redirect(w, r, basePath(r))
		return t, false
	}
	t, err = h.booking.Target(r.Context(), r.PathValue("practice"), r.PathValue("practitioner"), typeID)
	if err != nil {
		h.fail(w, r, err)
		return t, false
	}
	if t.Type.AtHome { // pas de liste d'attente pour les visites à domicile
		h.rd.Error(w, r, http.StatusNotFound, "")
		return t, false
	}
	return t, true
}

func (h *waitlistHandlers) renderForm(w http.ResponseWriter, r *http.Request, status int, t service.Target, v WaitlistValues, errs []string) {
	base := basePath(r)
	w.Header().Set("Cache-Control", "no-store")
	v.Channel = availableChannel(v.Channel, t.Channels)
	h.rd.PageStatus(w, r, status, "public_waitlist", WaitlistFormData{
		Channels: t.Channels, Practice: t.Practice.Name, Practitioner: practitionerView(t.Practice.Slug, t.Practitioner).Name,
		TypeName: t.Type.Name, TypeID: t.Type.ID.String(), Form: v, Weekdays: weekdayChoices(v.Weekdays),
		Today:  h.booking.Now().In(t.Loc).Format("2006-01-02"),
		Action: base + "/waitlist", BackURL: base + "/book?" + url.Values{"type": {t.Type.ID.String()}}.Encode(),
		Errors: errs,
	})
}

func (h *waitlistHandlers) renderVerify(w http.ResponseWriter, r *http.Request, status int, t service.Target, v WaitlistValues, challenge uuid.UUID, errs []string) {
	base := basePath(r)
	dest := v.Phone
	if n, err := domain.NormalizePhone(dest); err == nil {
		dest = n
	}
	if v.Channel == "email" {
		dest = v.Email
	}
	w.Header().Set("Cache-Control", "no-store")
	h.rd.PageStatus(w, r, status, "public_waitlist_verify", WaitlistVerifyData{
		Practice: t.Practice.Name, Practitioner: practitionerView(t.Practice.Slug, t.Practitioner).Name,
		TypeName: t.Type.Name, TypeID: t.Type.ID.String(), Form: v, ChallengeID: challenge.String(),
		Channel: v.Channel, Destination: maskRecipient(dest),
		Action: base + "/waitlist/confirm", ResendURL: base + "/waitlist",
		BackURL: base + "/book?" + url.Values{"type": {t.Type.ID.String()}}.Encode(), Errors: errs,
	})
}

func (h *waitlistHandlers) form(w http.ResponseWriter, r *http.Request) {
	t, ok := h.target(w, r)
	if !ok {
		return
	}
	today := h.booking.Now().In(t.Loc)
	h.renderForm(w, r, http.StatusOK, t, WaitlistValues{
		BookingForm: BookingForm{Channel: "email", SMSOptIn: true, EmailOptIn: true},
		Earliest:    today.Format("2006-01-02"), Latest: today.AddDate(0, 0, waitlistDefaultSpanDays).Format("2006-01-02"),
	}, nil)
}

func (h *waitlistHandlers) submit(w http.ResponseWriter, r *http.Request) {
	t, ok := h.target(w, r)
	if !ok {
		return
	}
	v := readWaitlistForm(r)
	v.Channel = availableChannel(v.Channel, t.Channels)
	if !v.Consent {
		h.renderForm(w, r, http.StatusUnprocessableEntity, t, v, []string{"Vous devez accepter l'utilisation de vos coordonnées pour être prévenu(e) d'un créneau libéré."})
		return
	}
	if formValue(r, "website") != "" { // piège à robots : on fait semblant, sans rien envoyer
		h.renderVerify(w, r, http.StatusOK, t, v, uuid.New(), nil)
		return
	}
	ver, err := h.svc.StartVerification(r.Context(), service.WaitlistVerifyInput{
		Window: v.window(t.Practitioner.ID, t.Type.ID), Patient: v.patient(), Channel: v.Channel,
	})
	switch {
	case err == nil:
		h.renderVerify(w, r, http.StatusOK, t, v, ver.ChallengeID, nil)
	case errors.Is(err, service.ErrOTPRateLimited):
		msg := "Trop de codes ont été demandés pour ce contact. Réessayez dans une heure"
		if t.Practice.Phone != "" {
			msg += " ou appelez le cabinet au " + t.Practice.Phone
		}
		h.renderForm(w, r, http.StatusTooManyRequests, t, v, []string{msg + "."})
	default:
		if msgs := validation(err); msgs != nil {
			h.renderForm(w, r, http.StatusUnprocessableEntity, t, v, msgs)
			return
		}
		h.fail(w, r, err)
	}
}

func (h *waitlistHandlers) confirm(w http.ResponseWriter, r *http.Request) {
	t, ok := h.target(w, r)
	if !ok {
		return
	}
	v := readWaitlistForm(r)
	challenge, cerr := uuid.Parse(formValue(r, "challenge_id"))
	if cerr != nil {
		h.renderForm(w, r, http.StatusUnprocessableEntity, t, v, []string{"La vérification a expiré. Validez le formulaire pour recevoir un nouveau code."})
		return
	}
	entry, err := h.svc.Join(r.Context(), service.WaitlistJoinInput{
		WaitlistVerifyInput: service.WaitlistVerifyInput{Window: v.window(t.Practitioner.ID, t.Type.ID), Patient: v.patient(), Channel: v.Channel},
		ChallengeID:         challenge,
		Code:                strings.Join(strings.Fields(r.PostFormValue("code")), ""),
	})
	switch {
	case err == nil:
		httpx.LoggerFrom(r.Context()).Info("inscription en liste d'attente",
			slog.String("waitlist_entry_id", entry.ID.String()), slog.String("practice_id", entry.PracticeID.String()))
		h.rd.SetFlash(w, "success", "Vous êtes inscrit(e) sur la liste d'attente. Nous vous préviendrons si un créneau correspondant se libère.")
		httpx.Redirect(w, r, basePath(r))
	case errors.Is(err, service.ErrOTPWrong):
		h.renderVerify(w, r, http.StatusUnprocessableEntity, t, v, challenge, []string{"Le code saisi est incorrect. Vérifiez-le et réessayez."})
	case errors.Is(err, domain.ErrOnlineBookingBlocked):
		renderBookingBlocked(h.rd, w, r, t.Practice.Phone, basePath(r), "Retour à la page du praticien")
	case errors.Is(err, service.ErrOTPExpired):
		h.renderForm(w, r, http.StatusUnprocessableEntity, t, v,
			[]string{"Ce code a expiré, a déjà servi ou le nombre d'essais est dépassé. Validez le formulaire pour recevoir un nouveau code."})
	default:
		if msgs := validation(err); msgs != nil {
			h.renderForm(w, r, http.StatusUnprocessableEntity, t, v, msgs)
			return
		}
		h.fail(w, r, err)
	}
}

// --- Proposition d'un créneau libéré ---

// OfferData est la donnée de la page public_waitlist_offer.
type OfferData struct {
	Practice     string
	Phone        string
	Practitioner string
	Type         string
	Duration     time.Duration
	FirstName    string
	Start        time.Time
	ExpiresAt    time.Time
	Available    bool
	Reason       string // expired | taken | claimed
	Action       string
}

func (h *waitlistHandlers) renderOffer(w http.ResponseWriter, r *http.Request, status int, v service.OfferView, token string) {
	noStore(w)
	h.rd.PageStatus(w, r, status, "public_waitlist_offer", OfferData{
		Practice: v.Practice.Name, Phone: v.Practice.Phone, Practitioner: practitionerView(v.Practice.Slug, v.Practitioner).Name,
		Type: v.Type.Name, Duration: v.End.Sub(v.Start), FirstName: v.FirstName, Start: v.Start, ExpiresAt: v.ExpiresAt,
		Available: v.Available, Reason: v.Reason, Action: "/w/" + url.PathEscape(token),
	})
}

func (h *waitlistHandlers) offer(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	v, err := h.svc.Offer(r.Context(), token)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	h.renderOffer(w, r, http.StatusOK, v, token)
}

func (h *waitlistHandlers) claim(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	res, err := h.svc.Claim(r.Context(), token)
	switch {
	case err == nil:
		httpx.LoggerFrom(r.Context()).Info("créneau obtenu via la liste d'attente",
			slog.String("appointment_id", res.Appointment.ID.String()), slog.String("practice_id", res.Appointment.PracticeID.String()))
		httpx.Redirect(w, r, manageURL(res.Token))
		return
	case errors.Is(err, domain.ErrOnlineBookingBlocked):
		phone, back, backText := "", "", ""
		if v, oerr := h.svc.Offer(r.Context(), token); oerr == nil {
			phone = v.Practice.Phone
			back, backText = practitionerView(v.Practice.Slug, v.Practitioner).URL, "Retour à la page du praticien"
		}
		renderBookingBlocked(h.rd, w, r, phone, back, backText)
	case errors.Is(err, domain.ErrSlotTaken), errors.Is(err, service.ErrOfferExpired):
		v, oerr := h.svc.Offer(r.Context(), token)
		if oerr != nil {
			h.fail(w, r, oerr)
			return
		}
		v.Available = false
		if errors.Is(err, domain.ErrSlotTaken) {
			v.Reason = "taken"
		}
		status := http.StatusConflict
		if v.Reason == "expired" || v.Reason == "claimed" {
			status = http.StatusGone
		}
		h.renderOffer(w, r, status, v, token)
	default:
		h.fail(w, r, err)
	}
}

// --- Désinscription (lien /wl/{jeton}) ---

// LeaveData est la donnée de la page public_waitlist_leave.
type LeaveData struct {
	State        string // confirm | done | gone
	Practice     string
	Phone        string
	Practitioner string
	Type         string
	Window       string
	Action       string
}

// renderGone : page neutre pour un lien inconnu, déjà utilisé ou périmé (aucune
// distinction, pour ne rien révéler sur l'existence d'une inscription).
func (h *waitlistHandlers) renderGone(w http.ResponseWriter, r *http.Request) {
	noStore(w)
	h.rd.PageStatus(w, r, http.StatusNotFound, "public_waitlist_leave", LeaveData{State: "gone"})
}

func (h *waitlistHandlers) leaveForm(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	v, err := h.svc.LeaveView(r.Context(), token)
	if errors.Is(err, domain.ErrNotFound) {
		h.renderGone(w, r)
		return
	}
	if err != nil {
		h.fail(w, r, err)
		return
	}
	noStore(w)
	h.rd.PageStatus(w, r, http.StatusOK, "public_waitlist_leave", LeaveData{
		State: "confirm", Practice: v.Practice.Name, Phone: v.Practice.Phone, Practitioner: v.Practitioner,
		Type: v.Type, Window: v.Window, Action: "/wl/" + url.PathEscape(token),
	})
}

func (h *waitlistHandlers) leave(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	// Le nom du cabinet est lu avant l'écriture : après, le lien ne résout plus rien.
	v, verr := h.svc.LeaveView(r.Context(), token)
	err := h.svc.Leave(r.Context(), token)
	switch {
	case errors.Is(err, domain.ErrNotFound):
		h.renderGone(w, r)
	case err != nil:
		h.fail(w, r, err)
	default:
		noStore(w)
		d := LeaveData{State: "done"}
		if verr == nil {
			d.Practice, d.Phone = v.Practice.Name, v.Practice.Phone
		}
		h.rd.PageStatus(w, r, http.StatusOK, "public_waitlist_leave", d)
	}
}
