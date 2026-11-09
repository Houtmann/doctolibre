package web_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"doctolibre/internal/store"
	"doctolibre/internal/store/sqlcgen"
)

const rebookStart = "2026-10-27T08:00:00Z" // mardi 9 h à Paris (CET depuis le 25/10)

// cancelledLink réserve (directement) puis annule par le lien ; retourne le chemin /r/{jeton}.
func (e *publicEnv) cancelledLink(t *testing.T, c *client, phone, e164 string) string {
	t.Helper()
	manage := e.bookDirect(t, tuesday9h, "Alice", phone, e164)
	if rec := c.do("POST", manage+"/cancel", url.Values{}); rec.Code != 303 {
		t.Fatalf("annulation: %d", rec.Code)
	}
	return manage
}

func (e *publicEnv) bookedCount(t *testing.T) int {
	t.Helper()
	var n int
	if err := e.db.QueryRow(context.Background(), "SELECT count(*) FROM appointment WHERE status = 'booked'").Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestPatientRebookFlow(t *testing.T) {
	t.Parallel()
	e := newPublicEnv(t)
	c := newClient(t, e.h, "192.0.2.80")
	manage := e.cancelledLink(t, c, "06 11 11 11 11", "+33611111111")

	// Le bouton de la page du RDV annulé pointe vers la reprise directe.
	mustContain(t, c.do("GET", manage, nil), 200, "Reprendre rendez-vous", `href="`+manage+`/rebook"`)

	// Grille : semaine du 26 octobre, créneaux vers l'écran de confirmation (sans ressaisie).
	rec := c.do("GET", manage+"/rebook?week=2026-44", nil)
	mustContain(t, rec, 200, "Reprendre rendez-vous", manage+"/rebook/confirm?start=2026-10-27T08%3A00%3A00Z")
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q", got)
	}

	rec = c.do("GET", manage+"/rebook/confirm?start="+url.QueryEscape(rebookStart), nil)
	mustContain(t, rec, 200, "Confirmer le nouveau rendez-vous", `name="csrf_token"`, `action="`+manage+`/rebook"`, `name="start" value="`+rebookStart+`"`)
	if strings.Contains(rec.Body.String(), `name="phone"`) {
		t.Error("l'écran de reprise ne doit pas redemander les coordonnées")
	}

	// Sans CSRF : refusé.
	if rec = c.doWith("POST", manage+"/rebook"); rec.Code != 403 {
		t.Errorf("POST sans CSRF = %d, want 403", rec.Code)
	}

	rec = c.do("POST", manage+"/rebook", url.Values{"start": {rebookStart}})
	loc := rec.Header().Get("Location")
	if rec.Code != 303 || !strings.HasPrefix(loc, "/r/") || loc == manage {
		t.Fatalf("reprise: %d %q", rec.Code, loc)
	}
	mustContain(t, c.do("GET", loc, nil), 200, "Rendez-vous confirmé", "Votre nouveau rendez-vous est confirmé.", "mardi 27 octobre 2026 à 09h00")
	if n := e.bookedCount(t); n != 1 {
		t.Errorf("%d RDV réservés, attendu 1", n)
	}

	// POST rejoué : page « déjà repris », aucun doublon ; le bouton de l'ancien RDV repasse au repli.
	rec = c.do("POST", manage+"/rebook", url.Values{"start": {rebookStart}})
	mustContain(t, rec, 403, "Rendez-vous déjà repris", `href="`+e.base()+`"`)
	if n := e.bookedCount(t); n != 1 {
		t.Errorf("%d RDV réservés après rejeu, attendu 1", n)
	}
	rec = c.do("POST", manage+"/rebook", url.Values{"start": {"2026-10-27T09:00:00Z"}})
	mustContain(t, rec, 403, "Rendez-vous déjà repris")
	if n := e.bookedCount(t); n != 1 {
		t.Errorf("%d RDV réservés après second rejeu, attendu 1", n)
	}
	rec = c.do("GET", manage, nil)
	mustContain(t, rec, 200, `href="`+e.base()+`/book?type=`+e.typ.ID.String()+`"`)
	if strings.Contains(rec.Body.String(), manage+"/rebook") {
		t.Error("lien de reprise directe alors que le RDV est déjà repris")
	}
}

func TestPatientRebookRefusals(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	e := newPublicEnv(t)
	if _, err := e.db.Exec(ctx, "UPDATE practice SET phone = '01 23 45 67 89' WHERE id = $1", e.pc.ID); err != nil {
		t.Fatal(err)
	}
	c := newClient(t, e.h, "192.0.2.81")

	// RDV non annulé : refus, retour vers la page du praticien.
	live := e.bookDirect(t, tuesday9h.Add(time.Hour), "Bob", "06 22 22 22 22", "+33622222222")
	for name, rec := range map[string]*httptest.ResponseRecorder{
		"GET grille":       c.do("GET", live+"/rebook", nil),
		"GET confirmation": c.do("GET", live+"/rebook/confirm?start="+url.QueryEscape(rebookStart), nil),
		"POST":             c.do("POST", live+"/rebook", url.Values{"start": {rebookStart}}),
	} {
		if rec.Code != 403 || !strings.Contains(rec.Body.String(), "Reprise en ligne impossible") ||
			!strings.Contains(rec.Body.String(), `href="`+e.base()+`"`) {
			t.Errorf("RDV non annulé, %s : %d", name, rec.Code)
		}
	}

	// Jeton inconnu : 404 neutre.
	if rec := c.do("GET", "/r/inconnu/rebook", nil); rec.Code != 404 {
		t.Errorf("jeton inconnu = %d", rec.Code)
	}

	// Annulé depuis plus de 30 jours (horloge injectée) : refusé, repli vers la grille classique.
	manage := e.cancelledLink(t, c, "06 11 11 11 11", "+33611111111")
	e.clock.Advance(30*24*time.Hour + time.Minute)
	rec := c.do("GET", manage+"/rebook", nil)
	mustContain(t, rec, 403, "Reprise en ligne impossible", `href="`+e.base()+`"`, "01 23 45 67 89")
	mustContain(t, c.do("POST", manage+"/rebook", url.Values{"start": {"2026-11-17T08:00:00Z"}}), 403, "Reprise en ligne impossible")
	rec = c.do("GET", manage, nil)
	mustContain(t, rec, 200, `href="`+e.base()+`/book?type=`+e.typ.ID.String()+`"`)
	if strings.Contains(rec.Body.String(), manage+"/rebook") {
		t.Error("lien de reprise directe hors fenêtre")
	}
	if n := e.bookedCount(t); n != 1 { // seul le RDV de Bob
		t.Errorf("%d RDV réservés, attendu 1", n)
	}
}

func TestPatientRebookTypeUnavailable(t *testing.T) {
	t.Parallel()
	e := newPublicEnv(t)
	c := newClient(t, e.h, "192.0.2.82")
	manage := e.cancelledLink(t, c, "06 11 11 11 11", "+33611111111")
	if _, err := e.db.Exec(context.Background(), "UPDATE appointment_type SET online_bookable = false WHERE id = $1", e.typ.ID); err != nil {
		t.Fatal(err)
	}
	mustContain(t, c.do("GET", manage+"/rebook", nil), 403, "Reprise en ligne impossible")
	mustContain(t, c.do("POST", manage+"/rebook", url.Values{"start": {rebookStart}}), 403, "Reprise en ligne impossible")
	if n := e.bookedCount(t); n != 0 {
		t.Errorf("%d RDV réservés", n)
	}
}

func TestPatientRebookBlockedByNoShows(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	e := newPublicEnv(t)
	c := newClient(t, e.h, "192.0.2.83")
	if _, err := e.db.Exec(ctx, "UPDATE practice SET phone = '01 23 45 67 89' WHERE id = $1", e.pc.ID); err != nil {
		t.Fatal(err)
	}
	manage := e.cancelledLink(t, c, "06 11 11 11 11", "+33611111111")
	var patientID uuid.UUID
	if err := e.db.QueryRow(ctx, "SELECT patient_id FROM appointment").Scan(&patientID); err != nil {
		t.Fatal(err)
	}
	missed := time.Date(2026, 10, 5, 9, 0, 0, 0, time.UTC)
	ap, err := store.CreateAppointment(ctx, sqlcgen.New(e.db), store.NewAppointment{
		ID: uuid.New(), PracticeID: e.pc.ID, PractitionerID: e.pr.ID, PatientID: patientID, AppointmentTypeID: e.typ.ID,
		Start: missed, End: missed.Add(30 * time.Minute), Source: "staff",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.db.Exec(ctx, "UPDATE appointment SET status = 'no_show' WHERE id = $1", ap.ID); err != nil {
		t.Fatal(err)
	}
	if err := sqlcgen.New(e.db).SetPracticeNoShowThreshold(ctx, sqlcgen.SetPracticeNoShowThresholdParams{
		ID: e.pc.ID, NoShowBlockThreshold: pgtype.Int2{Int16: 1, Valid: true},
	}); err != nil {
		t.Fatal(err)
	}
	// Le bouton retombe sur le repli, la grille et le POST affichent la page neutre de T092.
	rec := c.do("GET", manage, nil)
	if strings.Contains(rec.Body.String(), manage+"/rebook") {
		t.Error("lien de reprise directe pour un patient bloqué")
	}
	assertBookingBlocked(t, c.do("GET", manage+"/rebook", nil), "01 23 45 67 89", "tel:0123456789", e.base())
	assertBookingBlocked(t, c.do("POST", manage+"/rebook", url.Values{"start": {rebookStart}}), "01 23 45 67 89", "tel:0123456789", e.base())
	if n := e.bookedCount(t); n != 0 {
		t.Errorf("%d RDV réservés malgré le refus", n)
	}
}

func TestPatientRebookSlotTaken(t *testing.T) {
	t.Parallel()
	e := newPublicEnv(t)
	c := newClient(t, e.h, "192.0.2.84")
	manage := e.cancelledLink(t, c, "06 11 11 11 11", "+33611111111")
	// Un autre patient prend le créneau entre l'affichage et la confirmation.
	e.bookDirect(t, time.Date(2026, 10, 27, 8, 0, 0, 0, time.UTC), "Bob", "06 22 22 22 22", "+33622222222")
	rec := c.do("POST", manage+"/rebook", url.Values{"start": {rebookStart}})
	if rec.Code != 303 || rec.Header().Get("Location") != manage+"/rebook" {
		t.Fatalf("créneau pris: %d %q", rec.Code, rec.Header().Get("Location"))
	}
	mustContain(t, c.do("GET", manage+"/rebook?week=2026-44", nil), 200, "Ce créneau vient d&#39;être pris")
	if n := e.bookedCount(t); n != 1 {
		t.Errorf("%d RDV réservés, attendu 1 (Bob)", n)
	}
	// La reprise n'est pas consommée : un autre créneau passe.
	rec = c.do("POST", manage+"/rebook", url.Values{"start": {"2026-10-27T09:00:00Z"}})
	if rec.Code != 303 || rec.Header().Get("Location") == manage+"/rebook" {
		t.Errorf("autre créneau: %d %q", rec.Code, rec.Header().Get("Location"))
	}
}

func TestPatientRebookInstructionsAck(t *testing.T) {
	t.Parallel()
	e := newPublicEnv(t)
	c := newClient(t, e.h, "192.0.2.85")
	manage := e.cancelledLink(t, c, "06 11 11 11 11", "+33611111111")
	if _, err := e.db.Exec(context.Background(), "UPDATE appointment_type SET patient_instructions = 'Venir en tenue souple.', instructions_ack_required = true WHERE id = $1", e.typ.ID); err != nil {
		t.Fatal(err)
	}
	rec := c.do("GET", manage+"/rebook/confirm?start="+url.QueryEscape(rebookStart), nil)
	mustContain(t, rec, 200, "Venir en tenue souple.", `name="instructions_ack"`)

	// Sans la case : refus côté serveur, retour à l'écran de confirmation, aucun RDV.
	rec = c.do("POST", manage+"/rebook", url.Values{"start": {rebookStart}})
	if rec.Code != 303 || !strings.HasPrefix(rec.Header().Get("Location"), manage+"/rebook/confirm?") {
		t.Fatalf("sans case: %d %q", rec.Code, rec.Header().Get("Location"))
	}
	if n := e.bookedCount(t); n != 0 {
		t.Fatalf("%d RDV créé(s) sans confirmation des consignes", n)
	}
	rec = c.do("POST", manage+"/rebook", url.Values{"start": {rebookStart}, "instructions_ack": {"on"}})
	if rec.Code != 303 || !strings.HasPrefix(rec.Header().Get("Location"), "/r/") {
		t.Fatalf("avec case: %d %q", rec.Code, rec.Header().Get("Location"))
	}
	if n := e.bookedCount(t); n != 1 {
		t.Errorf("%d RDV réservés, attendu 1", n)
	}
}

// Quota horaire par praticien (T087) : une fois atteint, la reprise est refusée sans RDV
// créé, et le refus ne consomme pas la reprise (le patient peut réessayer plus tard).
func TestPatientRebookQuotaPerPractitioner(t *testing.T) {
	t.Parallel()
	e := newPublicEnv(t)
	c := newClient(t, e.h, "192.0.2.86")
	manage := e.cancelledLink(t, c, "06 11 11 11 11", "+33611111111")

	var slots []string
	for _, day := range []string{"2026-11-03", "2026-11-10", "2026-11-17", "2026-11-24", "2026-12-01"} {
		for _, hm := range []string{"08:00", "08:30", "09:00", "09:30", "10:00", "10:30"} {
			slots = append(slots, day+"T"+hm+":00Z")
		}
	}
	for i, s := range slots {
		if code := bookViaOTP(t, e, e.base(), i, s, false); code != http.StatusSeeOther {
			t.Fatalf("réservation %d/30 : %d", i+1, code)
		}
	}
	rec := c.do("POST", manage+"/rebook", url.Values{"start": {rebookStart}})
	if rec.Code != 303 || rec.Header().Get("Location") != manage {
		t.Fatalf("quota atteint: %d %q", rec.Code, rec.Header().Get("Location"))
	}
	mustContain(t, c.do("GET", manage, nil), 200, "momentanément limitées")
	if n := e.bookedCount(t); n != 30 {
		t.Errorf("%d RDV réservés, attendu 30", n)
	}
	var rebooked int
	if err := e.db.QueryRow(context.Background(), "SELECT count(*) FROM appointment WHERE rebooked_appointment_id IS NOT NULL").Scan(&rebooked); err != nil || rebooked != 0 {
		t.Errorf("lien de reprise posé malgré le refus : %d, %v", rebooked, err)
	}
}
