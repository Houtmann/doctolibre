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
	"doctolibre/internal/ics"
	"doctolibre/internal/service"
	"doctolibre/internal/store/sqlcgen"
)

// Limites de la réservation en ligne : 10 requêtes par minute et par IP et par parcours (limits.go).
const (
	publicBurst    = 10
	publicInterval = 6 * time.Second
	// maxFormValue borne les champs texte relus (le service valide ensuite).
	maxFormValue = 512
)

type publicHandlers struct {
	svc    *service.Booking
	rd     *Renderer
	logger *slog.Logger
	// waitlist : la liste d'attente est activée (lien sous la grille de créneaux).
	waitlist bool
	lim      *publicLimits
}

// publicRoutes enregistre la réservation en ligne (sans compte) et le lien du patient.
func (d Deps) publicRoutes(mux *http.ServeMux, rd *Renderer, logger *slog.Logger) {
	d.pub = d.newPublicLimits(rd, logger)
	h := &publicHandlers{svc: d.Booking, rd: rd, logger: logger, waitlist: d.Waitlist != nil, lim: d.pub}
	browse, search, book, manage := d.pub.browse.Middleware(), d.pub.search.Middleware(), d.pub.book.Middleware(), d.pub.manage.Middleware()
	const base = "/p/{practice}/{practitioner}/book"
	mux.Handle("GET /p/{practice}", httpx.Chain(http.HandlerFunc(h.practice), browse))
	mux.Handle("GET /p/{practice}/{practitioner}", httpx.Chain(http.HandlerFunc(h.practitioner), browse))
	mux.Handle("GET "+firstPath, httpx.Chain(http.HandlerFunc(h.firstAvailable), search))
	mux.Handle("GET "+base, httpx.Chain(http.HandlerFunc(h.grid), search))
	mux.Handle("GET "+base+"/details", httpx.Chain(http.HandlerFunc(h.detailsForm), search))
	// details et confirm partagent le seau « book » ; confirm est en plus limité par praticien (dans le handler).
	mux.Handle("POST "+base+"/details", httpx.Chain(http.HandlerFunc(h.detailsSubmit), book))
	mux.Handle("POST "+base+"/confirm", httpx.Chain(http.HandlerFunc(h.confirm), book))
	// Lien de gestion : limité par IP (le jeton est un secret de 256 bits, mais pas de sondage gratuit).
	// La grille de déplacement calcule des créneaux : elle partage le seau « search ».
	mux.Handle("GET /r/{token}", httpx.Chain(http.HandlerFunc(h.confirmed), manage))
	mux.Handle("GET /r/{token}/ics", httpx.Chain(http.HandlerFunc(h.calendarFile), manage))
	// Lien de série (T055) : même limiteur que /r/.
	mux.Handle("GET /s/{token}", httpx.Chain(http.HandlerFunc(h.series), manage))
	mux.Handle("GET /s/{token}/ics", httpx.Chain(http.HandlerFunc(h.seriesCalendar), manage))
	mux.Handle("GET /r/{token}/cancel", httpx.Chain(http.HandlerFunc(h.cancelForm), manage))
	mux.Handle("POST /r/{token}/cancel", httpx.Chain(http.HandlerFunc(h.cancelSubmit), manage))
	mux.Handle("GET /r/{token}/reschedule", httpx.Chain(http.HandlerFunc(h.rescheduleGrid), search))
	mux.Handle("GET /r/{token}/reschedule/confirm", httpx.Chain(http.HandlerFunc(h.rescheduleConfirm), search))
	mux.Handle("POST /r/{token}/reschedule", httpx.Chain(http.HandlerFunc(h.rescheduleSubmit), manage))
	if d.Waitlist != nil {
		d.waitlistRoutes(mux, rd, logger)
	}
}

// fail traduit une erreur non attendue en réponse HTTP (un seul endroit).
func (h *publicHandlers) fail(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, domain.ErrNotFound):
		h.rd.Error(w, r, http.StatusNotFound, "")
	case errors.Is(err, service.ErrBookingClosed):
		h.rd.Error(w, r, http.StatusServiceUnavailable, bookingClosedMessage(err))
	default:
		httpx.LoggerFrom(r.Context()).Error("réservation en ligne en échec", slog.String("err", err.Error()))
		h.rd.Error(w, r, http.StatusInternalServerError, "")
	}
}

// bookingClosedMessage : message neutre quand aucun canal ne peut envoyer le code de vérification (T086).
func bookingClosedMessage(err error) string {
	msg := "La réservation en ligne n'est pas disponible pour le moment. Merci de contacter le cabinet par téléphone"
	var c service.BookingClosedError
	if errors.As(err, &c) && c.Phone != "" {
		msg += " au " + c.Phone
	}
	return msg + "."
}

// --- Vues ---

// PublicPracticeData est la donnée de la page public_practice.
type PublicPracticeData struct {
	// Closed : aucun canal d'envoi du code actif, la réservation en ligne est fermée (T086).
	Closed        bool
	Practice      sqlcgen.Practice
	Practitioners []PractitionerView
	// FirstTypes : motifs proposés par « premier créneau disponible » (cabinet de plusieurs praticiens).
	FirstTypes []TypeView
}

// PractitionerView est un praticien affiché.
type PractitionerView struct {
	Name, Title, Bio, URL string
}

// TypeView est un motif réservable affiché.
type TypeView struct {
	ID       string
	Name     string
	Duration time.Duration
	URL      string
	AtHome   bool // visite à domicile : badge « Domicile »
}

// PublicPractitionerData est la donnée de la page public_practitioner.
type PublicPractitionerData struct {
	Closed       bool // voir PublicPracticeData.Closed
	Practice     sqlcgen.Practice
	Practitioner PractitionerView
	Types        []TypeView
}

// Summary rappelle ce qui est en cours de réservation.
type Summary struct {
	Practice     string
	Practitioner string
	Type         string
	Duration     time.Duration
	Start        time.Time // dans le fuseau du cabinet
}

// GridData est la donnée de la grille de créneaux (page et partial).
type GridData struct {
	Practice     string
	Practitioner string
	Type         TypeView
	WeekLabel    string
	Days         []GridDay
	PrevURL      string
	NextURL      string
	BackURL      string
	// WaitlistURL : inscription à la liste d'attente ; vide = fonction désactivée ou motif à domicile.
	WaitlistURL string
}

// GridDay regroupe les créneaux d'un jour.
type GridDay struct {
	Label string
	Slots []GridSlot
}

// GridSlot est un créneau cliquable.
type GridSlot struct {
	Time     time.Time
	URL      string
	Datetime string
}

// BookingForm est la saisie du formulaire de réservation.
type BookingForm struct {
	FirstName, LastName, Phone, Email, BirthDate string
	Address                                      string
	Channel                                      string
	SMSOptIn, EmailOptIn, Consent                bool
}

// DetailsData est la donnée de la page public_details.
type DetailsData struct {
	// Channels : canaux proposés pour recevoir le code (T086).
	Channels service.Channels
	AtHome   bool // visite à domicile : l'adresse est demandée
	Summary  Summary
	Form     BookingForm
	TypeID   string
	Start    string
	Action   string
	BackURL  string
	Errors   []string
}

// VerifyData est la donnée de la page public_verify.
type VerifyData struct {
	AtHome      bool
	Summary     Summary
	Form        BookingForm
	TypeID      string
	Start       string
	ChallengeID string
	Channel     string
	Destination string
	Action      string
	ResendURL   string
	BackURL     string
	Errors      []string
}

// ConfirmedData est la donnée de la page public_confirmed.
type ConfirmedData struct {
	Practice     sqlcgen.Practice
	Practitioner string
	Type         string
	Duration     time.Duration
	FirstName    string
	Start, End   time.Time
	Cancelled    bool
	ICSURL       string
	// Changeable : annulation et déplacement encore possibles (délai du cabinet respecté).
	Changeable bool
	ManageURL  string
	MinCancel  time.Duration
	AtHome     bool
	Address    string // adresse de la visite à domicile
	// RebookURL : page du praticien, pour reprendre rendez-vous après une annulation.
	RebookURL string
}

func practitionerView(practiceSlug string, p sqlcgen.Practitioner) PractitionerView {
	return PractitionerView{
		Name: strings.TrimSpace(p.FirstName + " " + p.LastName), Title: p.Title, Bio: p.Bio,
		URL: "/p/" + url.PathEscape(practiceSlug) + "/" + url.PathEscape(p.Slug),
	}
}

func typeDuration(t sqlcgen.AppointmentType) time.Duration {
	return time.Duration(t.Duration.Microseconds+int64(t.Duration.Days)*24*int64(time.Hour/time.Microsecond)) * time.Microsecond
}

func basePath(r *http.Request) string {
	return "/p/" + url.PathEscape(r.PathValue("practice")) + "/" + url.PathEscape(r.PathValue("practitioner"))
}

// --- Pages d'accueil ---

func (h *publicHandlers) practice(w http.ResponseWriter, r *http.Request) {
	p, err := h.svc.Practice(r.Context(), r.PathValue("practice"))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	data := PublicPracticeData{Practice: p.Practice, Closed: !p.Channels.Any()}
	for _, pr := range p.Practitioners {
		data.Practitioners = append(data.Practitioners, practitionerView(p.Practice.Slug, pr))
	}
	if !data.Closed {
		if data.FirstTypes, err = h.firstTypes(r, p.Practice.Slug); err != nil {
			h.fail(w, r, err)
			return
		}
	}
	h.rd.Page(w, r, "public_practice", data)
}

func (h *publicHandlers) practitioner(w http.ResponseWriter, r *http.Request) {
	p, err := h.svc.Practitioner(r.Context(), r.PathValue("practice"), r.PathValue("practitioner"))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	pv := practitionerView(p.Practice.Slug, p.Practitioner)
	data := PublicPractitionerData{Practice: p.Practice, Practitioner: pv, Closed: !p.Channels.Any()}
	for _, t := range p.Types {
		if data.Closed {
			break
		}
		data.Types = append(data.Types, TypeView{
			ID: t.ID.String(), Name: t.Name, Duration: typeDuration(t), AtHome: t.AtHome,
			URL: pv.URL + "/book?type=" + t.ID.String(),
		})
	}
	h.rd.Page(w, r, "public_practitioner", data)
}

// --- Grille de créneaux ---

// isoWeek formate une semaine au format « 2026-43 ».
func isoWeek(t time.Time) string {
	y, w := t.ISOWeek()
	return fmt.Sprintf("%04d-%02d", y, w)
}

// mondayOf retourne le lundi 00:00 (heure murale de loc) de la semaine de t.
func mondayOf(t time.Time, loc *time.Location) time.Time {
	t = t.In(loc)
	back := (int(t.Weekday()) + 6) % 7
	return time.Date(t.Year(), t.Month(), t.Day()-back, 0, 0, 0, 0, loc)
}

// parseWeek lit « 2026-43 » et retourne le lundi de cette semaine ISO ; ok=false si invalide.
func parseWeek(s string, loc *time.Location) (time.Time, bool) {
	var y, w int
	if n, err := fmt.Sscanf(s, "%d-%d", &y, &w); err != nil || n != 2 || y < 2000 || y > 2200 || w < 1 || w > 53 {
		return time.Time{}, false
	}
	jan4 := time.Date(y, time.January, 4, 12, 0, 0, 0, loc) // le 4 janvier est toujours en semaine 1
	mon := mondayOf(jan4, loc).AddDate(0, 0, (w-1)*7)
	if yy, ww := mon.ISOWeek(); yy != y || ww != w { // semaine 53 inexistante cette année-là
		return time.Time{}, false
	}
	return mon, true
}

func (h *publicHandlers) grid(w http.ResponseWriter, r *http.Request) {
	typeID, err := uuid.Parse(r.URL.Query().Get("type"))
	if err != nil {
		httpx.Redirect(w, r, basePath(r))
		return
	}
	t, err := h.svc.Target(r.Context(), r.PathValue("practice"), r.PathValue("practitioner"), typeID)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	now := h.svc.Now()
	minMon, maxMon := mondayOf(now, t.Loc), mondayOf(now.Add(t.Horizon), t.Loc)
	mon, ok := parseWeek(r.URL.Query().Get("week"), t.Loc)
	if !ok || mon.Before(minMon) {
		mon = minMon
	}
	if mon.After(maxMon) {
		mon = maxMon
	}
	next := mon.AddDate(0, 0, 7)
	slots, err := h.svc.AvailableSlots(r.Context(), t.Practitioner.ID, t.Type.ID, mon, next)
	if err != nil {
		h.fail(w, r, err)
		return
	}

	base := basePath(r)
	gridURL := func(week time.Time) string {
		return base + "/book?" + url.Values{"type": {typeID.String()}, "week": {isoWeek(week)}}.Encode()
	}
	data := GridData{
		Practice: t.Practice.Name, Practitioner: practitionerView(t.Practice.Slug, t.Practitioner).Name,
		Type:      TypeView{ID: typeID.String(), Name: t.Type.Name, Duration: typeDuration(t.Type)},
		WeekLabel: fmt.Sprintf("du %s au %s", frenchDate(mon), frenchDate(mon.AddDate(0, 0, 6))),
		BackURL:   base,
	}
	if h.waitlist && !t.Type.AtHome {
		data.WaitlistURL = base + "/waitlist?" + url.Values{"type": {typeID.String()}}.Encode()
	}
	if mon.After(minMon) {
		data.PrevURL = gridURL(mon.AddDate(0, 0, -7))
	}
	if next.Before(maxMon.AddDate(0, 0, 7)) {
		data.NextURL = gridURL(next)
	}
	for i := range 7 {
		day := mon.AddDate(0, 0, i)
		gd := GridDay{Label: frenchDate(day)}
		for _, s := range slots {
			st := s.Start.In(t.Loc)
			if st.Year() != day.Year() || st.YearDay() != day.YearDay() {
				continue
			}
			gd.Slots = append(gd.Slots, GridSlot{
				Time: st, Datetime: st.Format(time.RFC3339),
				URL: base + "/book/details?" + url.Values{"type": {typeID.String()}, "start": {s.Start.UTC().Format(time.RFC3339)}}.Encode(),
			})
		}
		if len(gd.Slots) > 0 {
			data.Days = append(data.Days, gd)
		}
	}
	// La réponse dépend de HX-Request : les caches ne doivent pas mélanger page et partial.
	w.Header().Add("Vary", "HX-Request")
	if httpx.IsHTMX(r) {
		h.rd.Partial(w, r, "booking_grid", data)
		return
	}
	h.rd.Page(w, r, "public_book", data)
}

// --- Détails, vérification, confirmation ---

// bookingTarget résout cabinet, praticien, motif et créneau de la requête.
// Une demande invalide (motif ou date illisibles, créneau hors disponibilités)
// redirige vers la grille ou la page du praticien ; ok=false si la réponse est déjà écrite.
func (h *publicHandlers) bookingTarget(w http.ResponseWriter, r *http.Request) (t service.Target, start time.Time, ok bool) {
	typeID, err := uuid.Parse(r.FormValue("type"))
	if err != nil {
		httpx.Redirect(w, r, basePath(r))
		return t, start, false
	}
	t, err = h.svc.Target(r.Context(), r.PathValue("practice"), r.PathValue("practitioner"), typeID)
	if err != nil {
		h.fail(w, r, err)
		return t, start, false
	}
	start, err = time.Parse(time.RFC3339, r.FormValue("start"))
	if err != nil {
		h.slotGone(w, r, t, "Ce créneau n'est pas valide.")
		return t, start, false
	}
	start = start.UTC()
	if err := h.svc.CheckSlot(r.Context(), t.Practitioner.ID, t.Type.ID, start); err != nil {
		if errors.Is(err, domain.ErrSlotTaken) {
			h.slotGone(w, r, t, "Ce créneau n'est plus disponible. Choisissez-en un autre.")
		} else {
			h.fail(w, r, err)
		}
		return t, start, false
	}
	return t, start, true
}

// slotGone ramène à la grille de la semaine avec un message.
func (h *publicHandlers) slotGone(w http.ResponseWriter, r *http.Request, t service.Target, msg string) {
	h.rd.SetFlash(w, "error", msg)
	httpx.Redirect(w, r, basePath(r)+"/book?"+url.Values{"type": {t.Type.ID.String()}}.Encode())
}

func (h *publicHandlers) summary(t service.Target, start time.Time) Summary {
	return Summary{
		Practice: t.Practice.Name, Practitioner: practitionerView(t.Practice.Slug, t.Practitioner).Name,
		Type: t.Type.Name, Duration: typeDuration(t.Type), Start: start.In(t.Loc),
	}
}

func formValue(r *http.Request, name string) string {
	v := strings.TrimSpace(r.PostFormValue(name))
	if len(v) > maxFormValue {
		v = v[:maxFormValue]
	}
	return v
}

func readBookingForm(r *http.Request) BookingForm {
	f := BookingForm{
		FirstName: formValue(r, "first_name"), LastName: formValue(r, "last_name"),
		Phone: formValue(r, "phone"), Email: formValue(r, "email"), BirthDate: formValue(r, "birth_date"),
		Address:  formValue(r, "address"),
		Channel:  formValue(r, "channel"),
		SMSOptIn: r.PostFormValue("sms_opt_in") != "", EmailOptIn: r.PostFormValue("email_opt_in") != "",
		Consent: r.PostFormValue("consent") != "",
	}
	f.Channel = resolveOTPChannel(f.Channel, f.Phone, f.Email)
	return f
}

// resolveOTPChannel choisit le canal du code de vérification : email par défaut
// (moins coûteux), repli sur l'autre contact si celui choisi est absent.
func resolveOTPChannel(channel, phone, email string) string {
	switch channel {
	case "email":
		if email == "" && phone != "" {
			return "sms"
		}
		return "email"
	case "sms":
		if phone == "" && email != "" {
			return "email"
		}
		return "sms"
	}
	if email != "" {
		return "email"
	}
	return "sms"
}

func (f BookingForm) patient() service.PatientDetails {
	return service.PatientDetails{
		FirstName: f.FirstName, LastName: f.LastName, Phone: f.Phone, Email: f.Email,
		BirthDate: f.BirthDate, Address: f.Address, SMSOptIn: f.SMSOptIn, EmailOptIn: f.EmailOptIn,
	}
}

func (h *publicHandlers) renderDetails(w http.ResponseWriter, r *http.Request, status int, t service.Target, start time.Time, f BookingForm, errs []string) {
	base := basePath(r)
	w.Header().Set("Cache-Control", "no-store")
	f.Channel = availableChannel(f.Channel, t.Channels)
	h.rd.PageStatus(w, r, status, "public_details", DetailsData{
		Channels: t.Channels, AtHome: t.Type.AtHome, Summary: h.summary(t, start), Form: f, TypeID: t.Type.ID.String(), Start: start.Format(time.RFC3339),
		Action:  base + "/book/details",
		BackURL: base + "/book?" + url.Values{"type": {t.Type.ID.String()}, "week": {isoWeek(start.In(t.Loc))}}.Encode(),
		Errors:  errs,
	})
}

func (h *publicHandlers) renderVerify(w http.ResponseWriter, r *http.Request, status int, t service.Target, start time.Time, f BookingForm, challenge uuid.UUID, errs []string) {
	base := basePath(r)
	dest := f.Phone
	if n, err := domain.NormalizePhone(dest); err == nil {
		dest = n
	}
	if f.Channel == "email" {
		dest = f.Email
	}
	w.Header().Set("Cache-Control", "no-store")
	h.rd.PageStatus(w, r, status, "public_verify", VerifyData{
		AtHome: t.Type.AtHome, Summary: h.summary(t, start), Form: f, TypeID: t.Type.ID.String(), Start: start.Format(time.RFC3339),
		ChallengeID: challenge.String(), Channel: f.Channel, Destination: maskRecipient(dest),
		Action: base + "/book/confirm", ResendURL: base + "/book/details",
		BackURL: base + "/book?" + url.Values{"type": {t.Type.ID.String()}, "week": {isoWeek(start.In(t.Loc))}}.Encode(),
		Errors:  errs,
	})
}

// availableChannel ramène le canal du code à un canal actif (l'autre si celui choisi est désactivé).
func availableChannel(channel string, ch service.Channels) string {
	if ch.Has(channel) {
		return channel
	}
	if ch.Email {
		return "email"
	}
	if ch.SMS {
		return "sms"
	}
	return channel
}

func (h *publicHandlers) detailsForm(w http.ResponseWriter, r *http.Request) {
	t, start, ok := h.bookingTarget(w, r)
	if !ok {
		return
	}
	h.renderDetails(w, r, http.StatusOK, t, start, BookingForm{Channel: "email", SMSOptIn: true, EmailOptIn: true}, nil)
}

func (h *publicHandlers) detailsSubmit(w http.ResponseWriter, r *http.Request) {
	t, start, ok := h.bookingTarget(w, r)
	if !ok {
		return
	}
	f := readBookingForm(r)
	f.Channel = availableChannel(f.Channel, t.Channels)
	if !f.Consent {
		h.renderDetails(w, r, http.StatusUnprocessableEntity, t, start, f, []string{"Vous devez accepter l'utilisation de vos coordonnées pour gérer ce rendez-vous."})
		return
	}
	if formValue(r, "website") != "" { // piège à robots : on fait semblant, sans rien envoyer
		h.renderVerify(w, r, http.StatusOK, t, start, f, uuid.New(), nil)
		return
	}
	v, err := h.svc.StartVerification(r.Context(), service.VerifyInput{
		PractitionerID: t.Practitioner.ID, TypeID: t.Type.ID, Start: start, Patient: f.patient(), Channel: f.Channel,
	})
	switch {
	case err == nil:
		h.renderVerify(w, r, http.StatusOK, t, start, f, v.ChallengeID, nil)
	case errors.Is(err, service.ErrOTPRateLimited):
		msg := "Trop de codes ont été demandés pour ce contact. Réessayez dans une heure"
		if t.Practice.Phone != "" {
			msg += " ou appelez le cabinet au " + t.Practice.Phone
		}
		h.renderDetails(w, r, http.StatusTooManyRequests, t, start, f, []string{msg + "."})
	case errors.Is(err, domain.ErrSlotTaken):
		h.slotGone(w, r, t, "Ce créneau vient d'être pris. Choisissez-en un autre.")
	default:
		if msgs := validation(err); msgs != nil {
			h.renderDetails(w, r, http.StatusUnprocessableEntity, t, start, f, msgs)
			return
		}
		h.fail(w, r, err)
	}
}

func (h *publicHandlers) confirm(w http.ResponseWriter, r *http.Request) {
	t, start, ok := h.bookingTarget(w, r)
	if !ok {
		return
	}
	f := readBookingForm(r)
	challenge, cerr := uuid.Parse(formValue(r, "challenge_id"))
	if cerr != nil {
		h.renderDetails(w, r, http.StatusUnprocessableEntity, t, start, f, []string{"La vérification a expiré. Validez le formulaire pour recevoir un nouveau code."})
		return
	}
	quota, ok := h.reservePractitioner(w, r, t, func(msg string) {
		h.renderDetails(w, r, http.StatusTooManyRequests, t, start, f, []string{msg})
	})
	if !ok {
		return
	}
	res, err := h.svc.Book(r.Context(), service.BookInput{
		VerifyInput: service.VerifyInput{
			PractitionerID: t.Practitioner.ID, TypeID: t.Type.ID, Start: start, Patient: f.patient(), Channel: f.Channel,
		},
		ChallengeID: challenge,
		Code:        strings.Join(strings.Fields(r.PostFormValue("code")), ""),
	})
	if err != nil {
		quota.Cancel() // seules les réservations abouties comptent (OTP faux, validation, créneau pris : non)
	}
	switch {
	case err == nil:
		httpx.LoggerFrom(r.Context()).Info("rendez-vous réservé en ligne",
			slog.String("appointment_id", res.Appointment.ID.String()), slog.String("practice_id", res.Appointment.PracticeID.String()))
		httpx.Redirect(w, r, "/r/"+url.PathEscape(res.Token))
	case errors.Is(err, service.ErrOTPWrong):
		h.renderVerify(w, r, http.StatusUnprocessableEntity, t, start, f, challenge, []string{"Le code saisi est incorrect. Vérifiez-le et réessayez."})
	case errors.Is(err, service.ErrOTPExpired):
		h.renderDetails(w, r, http.StatusUnprocessableEntity, t, start, f,
			[]string{"Ce code a expiré, a déjà servi ou le nombre d'essais est dépassé. Validez le formulaire pour recevoir un nouveau code."})
	case errors.Is(err, domain.ErrSlotTaken):
		h.slotGone(w, r, t, "Ce créneau vient d'être pris. Choisissez-en un autre.")
	case errors.Is(err, domain.ErrOnlineBookingBlocked):
		// Refus définitif : alerte à part, pas le bloc « erreurs du formulaire » (réessayer ne servirait à rien).
		renderBookingBlocked(h.rd, w, r, t.Practice.Phone, basePath(r), "Retour à la page du praticien")
	default:
		if msgs := validation(err); msgs != nil {
			h.renderDetails(w, r, http.StatusUnprocessableEntity, t, start, f, msgs)
			return
		}
		h.fail(w, r, err)
	}
}

// BookingBlockedData est la donnée de la page « réservation en ligne indisponible ».
type BookingBlockedData struct {
	Phone    string // affiché tel quel ("" = cabinet sans téléphone)
	TelHref  string // chiffres et "+" seulement, pour le lien tel:
	BackURL  string // page du praticien ou du cabinet ("" = pas de lien)
	BackText string
}

// telHref ne garde que les chiffres et le "+" d'un numéro.
func telHref(phone string) string {
	return strings.Map(func(r rune) rune {
		if r == '+' || (r >= '0' && r <= '9') {
			return r
		}
		return -1
	}, phone)
}

// renderBookingBlocked répond 403 avec la page neutre de refus de la réservation en
// ligne (décision T054 : jamais de mention des absences). Partagée par la réservation,
// l'inscription en liste d'attente et la prise d'un créneau proposé.
func renderBookingBlocked(rd *Renderer, w http.ResponseWriter, r *http.Request, phone, backURL, backText string) {
	w.Header().Set("Cache-Control", "no-store")
	rd.PageStatus(w, r, http.StatusForbidden, "public_booking_blocked", BookingBlockedData{
		Phone: phone, TelHref: telHref(phone), BackURL: backURL, BackText: backText,
	})
}

// --- Page de confirmation et fichier .ics ---

func (h *publicHandlers) confirmed(w http.ResponseWriter, r *http.Request) {
	c, err := h.svc.Confirmation(r.Context(), r.PathValue("token"))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	h.rd.Page(w, r, "public_confirmed", ConfirmedData{
		Practice: c.Practice, Practitioner: practitionerView(c.Practice.Slug, c.Practitioner).Name, Type: c.Type.Name,
		Duration: c.End.Sub(c.Start), FirstName: c.FirstName, Start: c.Start, End: c.End,
		Cancelled:  c.Appointment.Status == "cancelled",
		ICSURL:     manageURL(r.PathValue("token")) + "/ics",
		Changeable: c.Changeable, ManageURL: manageURL(r.PathValue("token")), MinCancel: c.MinCancel,
		AtHome: c.Type.AtHome, Address: c.Appointment.Address.String,
		RebookURL: practitionerView(c.Practice.Slug, c.Practitioner).URL,
	})
}

func (h *publicHandlers) calendarFile(w http.ResponseWriter, r *http.Request) {
	c, err := h.svc.Confirmation(r.Context(), r.PathValue("token"))
	if err != nil || c.Appointment.Status == "cancelled" {
		h.fail(w, r, errOrNotFound(err))
		return
	}
	desc := ""
	if c.Practice.Phone != "" {
		desc = "Cabinet : " + c.Practice.Phone
	}
	location := c.Practice.Address
	if c.Type.AtHome && c.Appointment.Address.String != "" {
		location = c.Appointment.Address.String // le praticien se déplace
	}
	ev := ics.Event{
		UID: c.Appointment.ID.String() + "@doctolibre", Start: c.Start, End: c.End, Stamp: h.svc.Now(),
		Summary: c.Type.Name + " - " + c.Practice.Name, Location: location, Description: desc,
		// Rappels natifs de l'agenda du patient (le flux praticien n'en a pas).
		Alarms: []time.Duration{24 * time.Hour, 2 * time.Hour},
	}
	w.Header().Set("Content-Type", "text/calendar; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="rendez-vous.ics"`)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	_, _ = w.Write(ev.Calendar())
}

func errOrNotFound(err error) error {
	if err != nil {
		return err
	}
	return domain.ErrNotFound
}
