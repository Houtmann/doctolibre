package web_test

import (
	"context"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"doctolibre/internal/service"
)

var tuesday9h = time.Date(2026, 10, 20, 7, 0, 0, 0, time.UTC) // 9 h à Paris (CEST)

// bookDirect réserve via le service et retourne le chemin du lien de gestion.
func (e *publicEnv) bookDirect(t *testing.T, start time.Time, first, phone, e164 string) string {
	t.Helper()
	in := service.VerifyInput{
		PractitionerID: e.pr.ID, TypeID: e.typ.ID, Start: start, Channel: "sms",
		Patient: service.PatientDetails{FirstName: first, LastName: "Test", Phone: phone, SMSOptIn: true, EmailOptIn: true},
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

func TestPatientCancelFlow(t *testing.T) {
	t.Parallel()
	e := newPublicEnv(t)
	c := newClient(t, e.h, "192.0.2.60")
	manage := e.bookDirect(t, tuesday9h, "Alice", "06 11 11 11 11", "+33611111111")

	rec := c.do("GET", manage, nil)
	mustContain(t, rec, 200, "Déplacer le rendez-vous", "Annuler le rendez-vous", manage+"/cancel", manage+"/reschedule")

	rec = c.do("GET", manage+"/cancel", nil)
	mustContain(t, rec, 200, "Annuler le rendez-vous ?", `name="csrf_token"`, `action="`+manage+`/cancel"`)

	// Sans jeton CSRF, la requête est refusée.
	if rec = c.doWith("POST", manage+"/cancel"); rec.Code != 403 {
		t.Errorf("POST sans CSRF = %d, want 403", rec.Code)
	}

	rec = c.do("POST", manage+"/cancel", url.Values{})
	if rec.Code != 303 || rec.Header().Get("Location") != manage {
		t.Fatalf("annulation: %d %q", rec.Code, rec.Header().Get("Location"))
	}
	rec = c.do("GET", manage, nil)
	mustContain(t, rec, 200, "Rendez-vous annulé", "Votre rendez-vous est annulé.", "Reprendre rendez-vous", `href="`+e.base()+`"`)
	if tm := regexp.MustCompile(`(?s)<title>(.*?)</title>`).FindStringSubmatch(rec.Body.String()); tm == nil || !strings.Contains(tm[1], "annulé") || strings.Contains(tm[1], "confirmé") {
		t.Errorf("<title> d'un RDV annulé = %v", tm)
	}
	if strings.Contains(rec.Body.String(), "Annuler le rendez-vous") {
		t.Error("un rendez-vous annulé ne propose plus d'action")
	}
	if rec = c.do("GET", manage+"/ics", nil); rec.Code != 404 {
		t.Errorf("ICS d'un RDV annulé = %d, want 404", rec.Code)
	}
}

func TestPatientCancelRefusedOutsideNotice(t *testing.T) {
	t.Parallel()
	e := newPublicEnv(t)
	c := newClient(t, e.h, "192.0.2.61")
	manage := e.bookDirect(t, tuesday9h, "Alice", "06 11 11 11 11", "+33611111111")
	// La veille à 8 h UTC : 23 h avant, délai de 24 h non respecté.
	if _, err := e.db.Exec(context.Background(), "UPDATE practice SET phone = '01 23 45 67 89' WHERE id = $1", e.pc.ID); err != nil {
		t.Fatal(err)
	}
	e.clock.Advance(tuesday9h.Sub(time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC)) - 23*time.Hour)

	rec := c.do("GET", manage, nil)
	mustContain(t, rec, 200, "ne peut plus être déplacé ni annulé en ligne", "01 23 45 67 89")
	if strings.Contains(rec.Body.String(), manage+"/cancel") {
		t.Error("le lien d'annulation ne doit pas être proposé hors délai")
	}
	rec = c.do("POST", manage+"/cancel", url.Values{})
	if rec.Code != 303 {
		t.Fatalf("POST hors délai = %d", rec.Code)
	}
	rec = c.do("GET", rec.Header().Get("Location"), nil)
	mustContain(t, rec, 200, "ne peut plus être modifié en ligne", "01 23 45 67 89")
	if rec = c.do("GET", manage+"/reschedule", nil); rec.Code != 303 {
		t.Errorf("GET reschedule hors délai = %d, want 303", rec.Code)
	}
	var status string
	if err := e.db.QueryRow(context.Background(), "SELECT status FROM appointment").Scan(&status); err != nil || status != "booked" {
		t.Errorf("status = %q, err = %v", status, err)
	}
}

func TestPatientRescheduleFlow(t *testing.T) {
	t.Parallel()
	e := newPublicEnv(t)
	c := newClient(t, e.h, "192.0.2.62")
	manage := e.bookDirect(t, tuesday9h, "Alice", "06 11 11 11 11", "+33611111111")
	e.bookDirect(t, tuesday9h.Add(time.Hour), "Bruno", "06 22 22 22 22", "+33622222222") // 10 h pris

	grid := manage + "/reschedule?week=2026-43"
	rec := c.do("GET", grid, nil)
	mustContain(t, rec, 200, "Choisir un nouveau créneau", `id="slot-grid"`, "mardi 20 octobre 2026", "09h00", "09h30")
	if strings.Contains(rec.Body.String(), "start=2026-10-20T08%3A00%3A00Z") {
		t.Error("le créneau de 10 h, pris, est proposé")
	}
	mustContain(t, c.doWith("GET", grid, "HX-Request", "true"), 200, `id="slot-grid"`)

	newStart := "2026-10-20T07:30:00Z" // 9 h 30
	rec = c.do("GET", manage+"/reschedule/confirm?start="+url.QueryEscape(newStart), nil)
	mustContain(t, rec, 200, "Confirmer le déplacement", "mardi 20 octobre 2026 à 09h30", `name="csrf_token"`, newStart)

	// Créneau pris entre-temps : refus et conservation de l'ancien rendez-vous.
	rec = c.do("POST", manage+"/reschedule", url.Values{"start": {"2026-10-20T08:00:00Z"}})
	if rec.Code != 303 || rec.Header().Get("Location") != manage+"/reschedule" {
		t.Fatalf("créneau pris: %d %q", rec.Code, rec.Header().Get("Location"))
	}
	rec = c.do("GET", manage+"/reschedule", nil)
	mustContain(t, rec, 200, "être pris", "conservé")
	rec = c.do("GET", manage, nil)
	mustContain(t, rec, 200, "mardi 20 octobre 2026 à 09h00")

	rec = c.do("POST", manage+"/reschedule", url.Values{"start": {newStart}})
	if rec.Code != 303 || rec.Header().Get("Location") != manage {
		t.Fatalf("déplacement: %d %q", rec.Code, rec.Header().Get("Location"))
	}
	rec = c.do("GET", manage, nil)
	mustContain(t, rec, 200, "Votre rendez-vous est déplacé.", "mardi 20 octobre 2026 à 09h30")
}

func TestPatientManageUnknownToken(t *testing.T) {
	t.Parallel()
	e := newPublicEnv(t)
	c := newClient(t, e.h, "192.0.2.63")
	for _, p := range []string{"/r/inconnu", "/r/inconnu/cancel", "/r/inconnu/reschedule", "/r/inconnu/reschedule/confirm?start=2026-10-20T07%3A00%3A00Z"} {
		if rec := c.do("GET", p, nil); rec.Code != 404 {
			t.Errorf("GET %s = %d, want 404", p, rec.Code)
		}
	}
	for _, p := range []string{"/r/inconnu/cancel", "/r/inconnu/reschedule"} {
		if rec := c.do("POST", p, url.Values{"start": {"2026-10-20T07:00:00Z"}}); rec.Code != 404 {
			t.Errorf("POST %s = %d, want 404", p, rec.Code)
		}
	}
}
