package web_test

import (
	"context"
	"doctolibre/internal/testutil"
	"io"
	"log/slog"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"doctolibre/internal/config"
	"doctolibre/internal/service"
	"doctolibre/internal/store"
	"doctolibre/internal/store/sqlcgen"
	"doctolibre/internal/web"
)

const waitlistBaseURL = "https://rdv.test"

var offerLinkRe = regexp.MustCompile(`https://rdv\.test/w/([A-Za-z0-9_-]+)`)

// withWaitlist monte la liste d'attente sur l'environnement public.
func withWaitlist(t *testing.T, e *publicEnv) *service.Waitlist {
	t.Helper()
	e.svc.BaseURL = waitlistBaseURL
	wl := &service.Waitlist{Pool: e.db, Clock: e.clock, OTP: e.otp, BaseURL: waitlistBaseURL, ManageKey: service.NewManageKey(testutil.ManageSecret)}
	e.h = web.Routes(web.Deps{
		Config: config.Config{Env: config.EnvDev}, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), DB: e.db,
		Booking: e.svc, Waitlist: wl,
	})
	return wl
}

func waitlistForm(typeID string) url.Values {
	return url.Values{
		"type": {typeID}, "earliest": {"2026-10-15"}, "latest": {"2026-10-31"}, "weekdays": {"1"},
		"first_name": {"Dora"}, "last_name": {"Web"}, "phone": {"06 44 44 44 44"}, "email": {"dora@example.test"},
		"channel": {"sms"}, "sms_opt_in": {"on"}, "email_opt_in": {"on"}, "consent": {"on"},
	}
}

func TestPublicWaitlistJoinJourney(t *testing.T) {
	t.Parallel()
	e := newPublicEnv(t)
	withWaitlist(t, e)
	c := newClient(t, e.h, "192.0.2.60")
	tid := e.typ.ID.String()

	// La grille propose l'inscription.
	rec := c.do("GET", e.base()+"/book?type="+tid+"&week=2026-43", nil)
	mustContain(t, rec, 200, "Aucun créneau ne me convient", e.base()+"/waitlist?type="+tid)

	rec = c.do("GET", e.base()+"/waitlist?type="+tid, nil)
	mustContain(t, rec, 200, `name="earliest"`, `name="weekdays"`, `name="csrf_token"`, `name="website"`, `for="phone"`, "Mardi")

	// Sans consentement : refusé.
	f := waitlistForm(tid)
	f.Del("consent")
	mustContain(t, c.do("POST", e.base()+"/waitlist", f), 422, "Vous devez accepter")

	// Fenêtre invalide : erreur de validation, aucun code envoyé.
	f = waitlistForm(tid)
	f.Set("latest", "2026-10-01")
	before := e.otp.count()
	mustContain(t, c.do("POST", e.base()+"/waitlist", f), 422, "doit suivre la première")
	if e.otp.count() != before {
		t.Error("code envoyé pour une saisie invalide")
	}

	// Envoi du code, puis confirmation.
	f = waitlistForm(tid)
	rec = c.do("POST", e.base()+"/waitlist", f)
	mustContain(t, rec, 200, `name="code"`, "par SMS au")
	m := challengeRe.FindStringSubmatch(rec.Body.String())
	if m == nil {
		t.Fatal("challenge_id absent")
	}
	code := e.otp.code("+33644444444")
	if strings.Contains(rec.Body.String(), code) {
		t.Error("le code ne doit jamais figurer dans la page")
	}
	f.Set("challenge_id", m[1])
	f.Set("code", "000000")
	if code == "000000" {
		f.Set("code", "111111")
	}
	mustContain(t, c.do("POST", e.base()+"/waitlist/confirm", f), 422, "Le code saisi est incorrect")
	f.Set("code", code)
	rec = c.do("POST", e.base()+"/waitlist/confirm", f)
	if rec.Code != 303 || rec.Header().Get("Location") != e.base() {
		t.Fatalf("confirmation: %d %q\n%s", rec.Code, rec.Header().Get("Location"), rec.Body.String())
	}
	var n int
	if err := e.db.QueryRow(context.Background(), "SELECT count(*) FROM waitlist_entry WHERE status = 'active' AND verified_channel = 'sms'").Scan(&n); err != nil || n != 1 {
		t.Fatalf("inscriptions actives = %d, %v", n, err)
	}
}

func TestPublicWaitlistOfferPage(t *testing.T) {
	t.Parallel()
	e := newPublicEnv(t)
	wl := withWaitlist(t, e)
	ctx := context.Background()
	c := newClient(t, e.h, "192.0.2.61")

	// Alice réserve mardi 9 h ; deux patients attendent.
	alice := bookOnline(t, e, "Alice", "06 11 11 11 11", "+33611111111")
	actor := staffActor(t, e)
	for _, p := range []service.PatientDetails{
		{FirstName: "Bob", LastName: "Attente", Email: "bob@example.test", SMSOptIn: true, EmailOptIn: true},
		{FirstName: "Carl", LastName: "Attente", Email: "carl@example.test", SMSOptIn: true, EmailOptIn: true},
	} {
		if _, err := wl.AddByStaff(ctx, actor, service.StaffAddInput{Patient: p, Window: service.WindowInput{TypeID: e.typ.ID, Earliest: "2026-10-15", Latest: "2026-10-31"}}); err != nil {
			t.Fatal(err)
		}
	}
	rec := c.do("POST", alice+"/cancel", url.Values{})
	if rec.Code != 303 {
		t.Fatalf("annulation: %d %s", rec.Code, rec.Body.String())
	}
	rows, err := e.db.Query(ctx, "SELECT payload->>'offer_url' FROM outbox_message WHERE template = 'waitlist' ORDER BY recipient")
	if err != nil {
		t.Fatal(err)
	}
	var tokens []string
	for rows.Next() {
		var link string
		if err := rows.Scan(&link); err != nil {
			t.Fatal(err)
		}
		m := offerLinkRe.FindStringSubmatch(link)
		if m == nil {
			t.Fatalf("lien = %q", link)
		}
		tokens = append(tokens, m[1])
	}
	rows.Close()
	if len(tokens) != 2 {
		t.Fatalf("propositions = %d, want 2", len(tokens))
	}

	// Page de la proposition : lecture seule, no-store, bouton de réservation.
	rec = c.do("GET", "/w/"+tokens[0], nil)
	mustContain(t, rec, 200, "Un créneau s'est libéré", "mardi 20 octobre 2026 à 09h00", "Réserver ce créneau", `name="csrf_token"`)
	if rec.Header().Get("Cache-Control") != "no-store" || rec.Header().Get("Referrer-Policy") != "no-referrer" {
		t.Error("la page de proposition doit être no-store et no-referrer")
	}
	if e.appointments(t) != 1 { // seulement celui d Alice
		t.Fatal("un GET ne doit rien réserver")
	}
	mustContain(t, c.do("GET", "/w/inconnu", nil), 404)

	// Premier clic : réservé, redirection vers le lien de gestion. Second : déjà pris.
	rec = c.do("POST", "/w/"+tokens[0], url.Values{})
	if rec.Code != 303 || !strings.HasPrefix(rec.Header().Get("Location"), "/r/") {
		t.Fatalf("premier clic: %d %q\n%s", rec.Code, rec.Header().Get("Location"), rec.Body.String())
	}
	mustContain(t, c.do("GET", rec.Header().Get("Location"), nil), 200, "Rendez-vous confirmé", "mardi 20 octobre 2026 à 09h00")
	mustContain(t, c.do("POST", "/w/"+tokens[1], url.Values{}), 409, "vient d'être pris")
	mustContain(t, c.do("GET", "/w/"+tokens[1], nil), 200, "vient d'être pris")
	if e.appointments(t) != 2 { // Alice (annulée) + le gagnant
		t.Errorf("rendez-vous = %d, want 2", e.appointments(t))
	}
}

// staffActor crée un utilisateur du cabinet.
func staffActor(t *testing.T, e *publicEnv) service.Actor {
	t.Helper()
	var id uuid.UUID
	err := e.db.QueryRow(context.Background(),
		"INSERT INTO app_user (practice_id, email, password_hash, role) VALUES ($1, $2, 'x', 'admin') RETURNING id",
		e.pc.ID, uuid.NewString()+"@example.test").Scan(&id)
	if err != nil {
		t.Fatal(err)
	}
	return service.Actor{UserID: id, PracticeID: e.pc.ID}
}

// bookOnline réserve mardi 20 octobre 9 h et retourne le chemin du lien de gestion.
func bookOnline(t *testing.T, e *publicEnv, first, phone, e164 string) string {
	t.Helper()
	in := service.VerifyInput{
		PractitionerID: e.pr.ID, TypeID: e.typ.ID, Start: time.Date(2026, 10, 20, 7, 0, 0, 0, time.UTC),
		Patient: service.PatientDetails{FirstName: first, LastName: "Test", Phone: phone, SMSOptIn: true, EmailOptIn: true}, Channel: "sms",
	}
	v, err := e.svc.StartVerification(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	res, err := e.svc.Book(context.Background(), service.BookInput{VerifyInput: in, ChallengeID: v.ChallengeID, Code: e.otp.code(e164)})
	if err != nil {
		t.Fatal(err)
	}
	return "/r/" + res.Token
}

// Un patient au-dessus du seuil d'absences ne s'inscrit pas en ligne : refus à part
// (pas de « erreurs du formulaire »), avec le téléphone du cabinet, aucune inscription.
func TestPublicWaitlistJoinBlockedByNoShows(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	e := newPublicEnv(t)
	withWaitlist(t, e)
	if _, err := e.db.Exec(ctx, "UPDATE practice SET phone = '01 23 45 67 89', no_show_block_threshold = 1 WHERE id = $1", e.pc.ID); err != nil {
		t.Fatal(err)
	}
	bookOnline(t, e, "Dora", "06 44 44 44 44", "+33644444444")
	if _, err := e.db.Exec(ctx, "UPDATE appointment SET status = 'no_show' WHERE practice_id = $1", e.pc.ID); err != nil {
		t.Fatal(err)
	}
	c := newClient(t, e.h, "192.0.2.62")
	tid := e.typ.ID.String()
	c.do("GET", e.base()+"/waitlist?type="+tid, nil)
	first := waitlistForm(tid)
	first.Set("last_name", "Test")
	rec := c.do("POST", e.base()+"/waitlist", first)
	m := challengeRe.FindStringSubmatch(rec.Body.String())
	if m == nil {
		t.Fatalf("challenge_id absent: %d", rec.Code)
	}
	f := waitlistForm(tid)
	f.Set("last_name", "Test") // même patient que la réservation précédente
	f.Set("challenge_id", m[1])
	f.Set("code", e.otp.code("+33644444444"))
	rec = c.do("POST", e.base()+"/waitlist/confirm", f)
	assertBookingBlocked(t, rec, "01 23 45 67 89", "tel:0123456789", e.base())
	if strings.Contains(rec.Body.String(), "Le formulaire contient des erreurs") {
		t.Error("le refus ne doit pas apparaître comme une erreur de formulaire")
	}
	var n int
	if err := e.db.QueryRow(ctx, "SELECT count(*) FROM waitlist_entry").Scan(&n); err != nil || n != 0 {
		t.Errorf("inscriptions = %d, %v", n, err)
	}
}

// Le patient proposé qui a atteint le seuil d'absences ne prend pas le créneau via
// /w/{token} : page de refus (403) avec téléphone et retour vers le praticien, puis
// sans téléphone si le cabinet n'en a pas ; aucun rendez-vous créé.
func TestPublicWaitlistClaimBlockedByNoShows(t *testing.T) {
	t.Parallel()
	e := newPublicEnv(t)
	wl := withWaitlist(t, e)
	ctx := context.Background()
	c := newClient(t, e.h, "192.0.2.63")

	alice := bookOnline(t, e, "Alice", "06 11 11 11 11", "+33611111111")
	actor := staffActor(t, e)
	if _, err := wl.AddByStaff(ctx, actor, service.StaffAddInput{
		Patient: service.PatientDetails{FirstName: "Bob", LastName: "Attente", Email: "bob@example.test", SMSOptIn: true, EmailOptIn: true},
		Window:  service.WindowInput{TypeID: e.typ.ID, Earliest: "2026-10-15", Latest: "2026-10-31"},
	}); err != nil {
		t.Fatal(err)
	}
	if rec := c.do("POST", alice+"/cancel", url.Values{}); rec.Code != 303 {
		t.Fatalf("annulation: %d %s", rec.Code, rec.Body.String())
	}
	var link string
	if err := e.db.QueryRow(ctx, "SELECT payload->>'offer_url' FROM outbox_message WHERE template = 'waitlist'").Scan(&link); err != nil {
		t.Fatal(err)
	}
	m := offerLinkRe.FindStringSubmatch(link)
	if m == nil {
		t.Fatalf("lien = %q", link)
	}
	// Une absence récente pour le patient en attente, seuil à 1.
	q := sqlcgen.New(e.db)
	var patientID uuid.UUID
	if err := e.db.QueryRow(ctx, "SELECT patient_id FROM waitlist_entry").Scan(&patientID); err != nil {
		t.Fatal(err)
	}
	missed := time.Date(2026, 10, 5, 9, 0, 0, 0, time.UTC)
	ap, err := store.CreateAppointment(ctx, q, store.NewAppointment{
		ID: uuid.New(), PracticeID: e.pc.ID, PractitionerID: e.pr.ID, PatientID: patientID, AppointmentTypeID: e.typ.ID,
		Start: missed, End: missed.Add(30 * time.Minute), Source: "staff",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.db.Exec(ctx, "UPDATE appointment SET status = 'no_show' WHERE id = $1", ap.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.db.Exec(ctx, "UPDATE practice SET phone = '01 23 45 67 89', no_show_block_threshold = 1 WHERE id = $1", e.pc.ID); err != nil {
		t.Fatal(err)
	}
	before := e.appointments(t)

	assertBookingBlocked(t, c.do("POST", "/w/"+m[1], url.Values{}), "01 23 45 67 89", "tel:0123456789", e.base())
	if _, err := e.db.Exec(ctx, "UPDATE practice SET phone = '' WHERE id = $1", e.pc.ID); err != nil {
		t.Fatal(err)
	}
	assertBookingBlocked(t, c.do("POST", "/w/"+m[1], url.Values{}), "", "", e.base())
	if e.appointments(t) != before {
		t.Error("rendez-vous créé malgré le refus")
	}
}
