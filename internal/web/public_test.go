package web_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"doctolibre/internal/config"
	"doctolibre/internal/service"
	"doctolibre/internal/store/sqlcgen"
	"doctolibre/internal/testutil"
	"doctolibre/internal/web"
)

// otpCapture mémorise les codes envoyés (par destinataire) au lieu de les enfiler.
type otpCapture struct {
	mu    sync.Mutex
	codes map[string]string
	sent  int
}

func (o *otpCapture) SendOTP(_ context.Context, _ pgx.Tx, _ uuid.UUID, _, _, recipient, code string) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.codes == nil {
		o.codes = map[string]string{}
	}
	o.codes[recipient] = code
	o.sent++
	return nil
}

func (o *otpCapture) code(recipient string) string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.codes[recipient]
}

func (o *otpCapture) count() int {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.sent
}

type publicEnv struct {
	h     http.Handler
	db    *pgxpool.Pool
	otp   *otpCapture
	pc    sqlcgen.Practice
	pr    sqlcgen.Practitioner
	typ   sqlcgen.AppointmentType
	svc   *service.Booking
	clock *testutil.FixedClock
}

func (e *publicEnv) base() string { return "/p/" + e.pc.Slug + "/" + e.pr.Slug }

// newPublicEnv monte le site public : mardi 9 h–12 h (Paris), horloge au jeudi 15/10/2026.
func newPublicEnv(t *testing.T) *publicEnv {
	t.Helper()
	db := testutil.DB(t)
	pc := testutil.NewPractice(t, db)
	pr := testutil.NewPractitioner(t, db, pc.ID)
	typ := testutil.NewAppointmentType(t, db, pc.ID)
	if _, err := sqlcgen.New(db).CreateAvailabilityRule(context.Background(), sqlcgen.CreateAvailabilityRuleParams{
		PractitionerID: pr.ID, Weekday: 1,
		StartTime: pgtype.Time{Microseconds: 9 * time.Hour.Microseconds(), Valid: true},
		EndTime:   pgtype.Time{Microseconds: 12 * time.Hour.Microseconds(), Valid: true},
		ValidFrom: pgtype.Date{Time: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), Valid: true},
	}); err != nil {
		t.Fatal(err)
	}
	otp := &otpCapture{}
	clock := testutil.NewFixedClock(time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC))
	svc := &service.Booking{Pool: db, Clock: clock, OTP: otp}
	h := web.Routes(web.Deps{
		Config:  config.Config{Env: config.EnvDev},
		Logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
		DB:      db,
		Booking: svc,
	})
	return &publicEnv{h: h, db: db, otp: otp, pc: pc, pr: pr, typ: typ, svc: svc, clock: clock}
}

func (e *publicEnv) appointments(t *testing.T) int {
	t.Helper()
	var n int
	if err := e.db.QueryRow(context.Background(), "SELECT count(*) FROM appointment").Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

var challengeRe = regexp.MustCompile(`name="challenge_id" value="([0-9a-f-]{36})"`)

func detailsForm(typeID, start string) url.Values {
	return url.Values{
		"type": {typeID}, "start": {start},
		"first_name": {"Alice"}, "last_name": {"Durand"}, "phone": {"06 11 11 11 11"}, "email": {"alice@example.test"},
		"channel": {"sms"}, "sms_opt_in": {"on"}, "email_opt_in": {"on"}, "consent": {"on"},
	}
}

func TestPublicBookingFullJourney(t *testing.T) {
	t.Parallel()
	e := newPublicEnv(t)
	c := newClient(t, e.h, "192.0.2.50")
	const start = "2026-10-20T07:00:00Z" // mardi 9 h 00 à Paris (CEST)

	// 1. Cabinet.
	rec := c.do("GET", "/p/"+e.pc.Slug, nil)
	mustContain(t, rec, 200, e.pc.Name, e.base(), "Camille Martin")

	// 2. Praticien et motifs.
	rec = c.do("GET", e.base(), nil)
	mustContain(t, rec, 200, "Séance", "/book?type="+e.typ.ID.String(), "30 min")

	// 3. Grille de la semaine 43, page complète puis partial htmx.
	gridURL := e.base() + "/book?type=" + e.typ.ID.String() + "&week=2026-43"
	rec = c.do("GET", gridURL, nil)
	mustContain(t, rec, 200, "<!doctype html>", `id="slot-grid"`, "mardi 20 octobre 2026", "09h00", "11h30",
		"/book/details?start=2026-10-20T07%3A00%3A00Z&amp;type="+e.typ.ID.String(), "Semaine suivante", "Semaine précédente")
	if strings.Contains(rec.Body.String(), "12h00") {
		t.Error("le créneau de 12 h dépasse la plage et ne doit pas être proposé")
	}
	hx := c.doWith("GET", gridURL, "HX-Request", "true")
	mustContain(t, hx, 200, `id="slot-grid"`, "09h00")
	if strings.Contains(hx.Body.String(), "<html") {
		t.Error("une requête htmx doit recevoir le partial seul")
	}
	if !strings.Contains(hx.Header().Get("Vary"), "HX-Request") {
		t.Error("Vary: HX-Request manquant")
	}

	// 4. Formulaire.
	detailsURL := e.base() + "/book/details?type=" + e.typ.ID.String() + "&start=" + url.QueryEscape(start)
	rec = c.do("GET", detailsURL, nil)
	mustContain(t, rec, 200, `name="first_name"`, `name="csrf_token"`, `name="website"`, `for="phone"`, "mardi 20 octobre 2026 à 09h00")
	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Error("la page de coordonnées ne doit pas être mise en cache")
	}

	// 5. Envoi du code.
	form := detailsForm(e.typ.ID.String(), start)
	rec = c.do("POST", detailsURL, form)
	mustContain(t, rec, 200, `name="code"`, "par SMS au", "33•", "11</strong>")
	m := challengeRe.FindStringSubmatch(rec.Body.String())
	if m == nil {
		t.Fatal("challenge_id absent de la page de vérification")
	}
	code := e.otp.code("+33611111111")
	if len(code) != 6 {
		t.Fatalf("code envoyé = %q", code)
	}
	if strings.Contains(rec.Body.String(), code) {
		t.Error("le code ne doit jamais figurer dans la page")
	}

	// 6. Mauvais code, puis bon code.
	confirm := detailsForm(e.typ.ID.String(), start)
	confirm.Set("challenge_id", m[1])
	confirm.Set("code", "000000")
	if code == "000000" {
		confirm.Set("code", "111111")
	}
	rec = c.do("POST", e.base()+"/book/confirm", confirm)
	mustContain(t, rec, 422, "Le code saisi est incorrect")
	if e.appointments(t) != 0 {
		t.Fatal("rendez-vous créé avec un mauvais code")
	}
	confirm.Set("code", code[:3]+" "+code[3:]) // espaces tolérés
	rec = c.do("POST", e.base()+"/book/confirm", confirm)
	if rec.Code != 303 || !strings.HasPrefix(rec.Header().Get("Location"), "/r/") {
		t.Fatalf("confirmation: %d %q\n%s", rec.Code, rec.Header().Get("Location"), rec.Body.String())
	}
	manage := rec.Header().Get("Location")

	// 7. Page de confirmation et .ics.
	rec = c.do("GET", manage, nil)
	mustContain(t, rec, 200, "Rendez-vous confirmé", "Alice", "mardi 20 octobre 2026 à 09h00", "Ajouter à mon agenda", manage+"/ics")
	if rec.Header().Get("Cache-Control") != "no-store" || rec.Header().Get("Referrer-Policy") != "no-referrer" {
		t.Error("la page du lien de gestion doit être no-store et no-referrer")
	}
	rec = c.do("GET", manage+"/ics", nil)
	mustContain(t, rec, 200, "BEGIN:VCALENDAR", "DTSTART:20261020T070000Z", "DTEND:20261020T073000Z", "SUMMARY:Séance - "+e.pc.Name)
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/calendar") {
		t.Errorf("Content-Type = %q", ct)
	}
	if strings.Contains(rec.Body.String(), "Durand") || strings.Contains(rec.Body.String(), "Alice") {
		t.Error("le fichier .ics ne doit contenir aucun nom de patient")
	}

	// 8. Le créneau n'est plus proposé ; y accéder ramène à la grille avec un message.
	rec = c.do("GET", gridURL, nil)
	if strings.Contains(rec.Body.String(), "start=2026-10-20T07%3A00%3A00Z") {
		t.Error("le créneau réservé est encore proposé")
	}
	rec = c.do("GET", detailsURL, nil)
	if rec.Code != 303 || !strings.Contains(rec.Header().Get("Location"), "/book?type=") {
		t.Fatalf("créneau pris: %d %q", rec.Code, rec.Header().Get("Location"))
	}
	rec = c.do("GET", rec.Header().Get("Location"), nil)
	mustContain(t, rec, 200, "plus disponible")
}

func TestPublicBookingForgedSlotRefused(t *testing.T) {
	t.Parallel()
	e := newPublicEnv(t)
	c := newClient(t, e.h, "192.0.2.51")
	tid := e.typ.ID.String()
	forged := []string{
		"2026-10-20T07:07:00Z", // hors grille
		"2026-10-21T07:00:00Z", // mercredi, aucune plage
		"2026-10-20T05:00:00Z", // avant la plage
		"2026-10-20T10:00:00Z", // 12 h à Paris : la plage est finie
		"2026-10-13T07:00:00Z", // passé
		"2027-10-19T07:00:00Z", // au-delà de l'horizon
		"n'importe quoi",
	}
	for _, start := range forged {
		q := "?type=" + tid + "&start=" + url.QueryEscape(start)
		if rec := c.do("GET", e.base()+"/book/details"+q, nil); rec.Code != 303 {
			t.Errorf("GET details %q : %d, redirection attendue", start, rec.Code)
		}
		if rec := c.do("POST", e.base()+"/book/details"+q, detailsForm(tid, start)); rec.Code != 303 {
			t.Errorf("POST details %q : %d, redirection attendue", start, rec.Code)
		}
		f := detailsForm(tid, start)
		f.Set("challenge_id", uuid.NewString())
		f.Set("code", "123456")
		if rec := c.do("POST", e.base()+"/book/confirm", f); rec.Code != 303 {
			t.Errorf("POST confirm %q : %d, redirection attendue", start, rec.Code)
		}
	}
	if e.otp.count() != 0 {
		t.Errorf("%d codes envoyés pour des créneaux forgés", e.otp.count())
	}
	if e.appointments(t) != 0 {
		t.Error("un rendez-vous a été créé sur un créneau forgé")
	}
	// Motif inconnu ou d'un autre cabinet : 404 ; sans motif : retour à la page du praticien.
	if rec := c.do("GET", e.base()+"/book?type="+uuid.NewString(), nil); rec.Code != 404 {
		t.Errorf("motif inconnu: %d", rec.Code)
	}
	if rec := c.do("GET", e.base()+"/book", nil); rec.Code != 303 {
		t.Errorf("sans motif: %d", rec.Code)
	}
}

func TestPublicBookingFormValidationAndHoneypot(t *testing.T) {
	t.Parallel()
	e := newPublicEnv(t)
	c := newClient(t, e.h, "192.0.2.52")
	const start = "2026-10-20T07:00:00Z"
	tid := e.typ.ID.String()
	target := e.base() + "/book/details?type=" + tid + "&start=" + url.QueryEscape(start)

	f := detailsForm(tid, start)
	f.Set("phone", "123")
	f.Set("first_name", "")
	rec := c.do("POST", target, f)
	mustContain(t, rec, 422, "Le formulaire contient des erreurs", "Le prénom est requis", "pas valide (exemple", `value="Durand"`)

	f = detailsForm(tid, start)
	f.Del("consent")
	mustContain(t, c.do("POST", target, f), 422, "utilisation de vos coordonnées")

	f = detailsForm(tid, start)
	f.Set("website", "http://spam.example")
	mustContain(t, c.do("POST", target, f), 200, `name="code"`)
	if e.otp.count() != 0 {
		t.Error("le piège à robots ne doit déclencher aucun envoi")
	}

	// Sans jeton CSRF, la requête est refusée avant d'atteindre le handler.
	body := detailsForm(tid, start)
	req := httptest.NewRequest("POST", target, strings.NewReader(body.Encode()))
	req.RemoteAddr = "192.0.2.99:4321"
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec = httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("POST sans CSRF: %d, attendu 403", rec.Code)
	}
}

func TestPublicBookingOTPLimits(t *testing.T) {
	t.Parallel()
	e := newPublicEnv(t)
	c := newClient(t, e.h, "192.0.2.53")
	const start = "2026-10-20T07:00:00Z"
	tid := e.typ.ID.String()
	target := e.base() + "/book/details?type=" + tid + "&start=" + url.QueryEscape(start)
	for i := range service.OTPPerHour {
		if rec := c.do("POST", target, detailsForm(tid, start)); rec.Code != 200 {
			t.Fatalf("demande %d: %d", i+1, rec.Code)
		}
	}
	rec := c.do("POST", target, detailsForm(tid, start))
	mustContain(t, rec, http.StatusTooManyRequests, "Trop de codes")
	if e.otp.count() != service.OTPPerHour {
		t.Errorf("%d codes envoyés, attendu %d", e.otp.count(), service.OTPPerHour)
	}
}

func TestPublicBookingIPRateLimit(t *testing.T) {
	t.Parallel()
	e := newPublicEnv(t)
	c := newClient(t, e.h, "192.0.2.54")
	gridURL := e.base() + "/book?type=" + e.typ.ID.String()
	limited := false
	for range 15 {
		if c.do("GET", gridURL, nil).Code == http.StatusTooManyRequests {
			limited = true
			break
		}
	}
	if !limited {
		t.Error("au-delà de 10 recherches rapprochées, la même IP doit être limitée (429)")
	}
	// Une autre IP n'est pas touchée.
	other := newClient(t, e.h, "192.0.2.55")
	if rec := other.do("GET", gridURL, nil); rec.Code != 200 {
		t.Errorf("autre IP: %d", rec.Code)
	}
}

func TestPublicNotFound(t *testing.T) {
	t.Parallel()
	e := newPublicEnv(t)
	c := newClient(t, e.h, "192.0.2.56")
	for _, p := range []string{
		"/p/inconnu", "/p/" + e.pc.Slug + "/inconnu", "/p/inconnu/" + e.pr.Slug,
		"/r/jeton-inconnu", "/r/jeton-inconnu/ics",
	} {
		if rec := c.do("GET", p, nil); rec.Code != 404 {
			t.Errorf("GET %s : %d, attendu 404", p, rec.Code)
		}
	}
	// Praticien désactivé : introuvable.
	if _, err := e.db.Exec(context.Background(), "UPDATE practitioner SET active = false WHERE id = $1", e.pr.ID); err != nil {
		t.Fatal(err)
	}
	if rec := c.do("GET", e.base(), nil); rec.Code != 404 {
		t.Errorf("praticien inactif: %d", rec.Code)
	}
}

func TestPublicBookingWeekNavigation(t *testing.T) {
	t.Parallel()
	e := newPublicEnv(t)
	c := newClient(t, e.h, "192.0.2.57")
	g := e.base() + "/book?type=" + e.typ.ID.String()
	// Semaine passée ou illisible : ramenée à la semaine courante (42, sans créneau, mardi passé).
	for _, w := range []string{"2020-01", "n'importe quoi", "2026-99", ""} {
		rec := c.do("GET", g+"&week="+url.QueryEscape(w), nil)
		mustContain(t, rec, 200, "Semaine du lundi 12 octobre 2026 au dimanche 18 octobre 2026")
		if strings.Contains(rec.Body.String(), "Semaine précédente") {
			t.Errorf("semaine %q : pas de navigation vers le passé", w)
		}
	}
	// Bien au-delà de l'horizon (60 jours) : ramenée à la dernière semaine réservable, sans « suivante ».
	rec := c.do("GET", g+"&week=2030-10", nil)
	if rec.Code != 200 || strings.Contains(rec.Body.String(), "Semaine suivante") {
		t.Errorf("hors horizon: %d", rec.Code)
	}
}

// doWith envoie une requête sans corps avec des en-têtes en plus (couples nom, valeur).
func (c *client) doWith(method, target string, headers ...string) *httptest.ResponseRecorder {
	c.t.Helper()
	req := httptest.NewRequest(method, target, nil)
	req.RemoteAddr = c.addr + ":4321"
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	for _, ck := range c.cookies {
		req.AddCookie(ck)
	}
	rec := httptest.NewRecorder()
	c.h.ServeHTTP(rec, req)
	return rec
}
