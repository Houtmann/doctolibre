package web_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"doctolibre/internal/store/sqlcgen"
)

// T096 : « Modifier mes coordonnées » ramène au formulaire pré-rempli sans rien envoyer.
func TestPublicVerifyEditDetails(t *testing.T) {
	t.Parallel()
	e := newPublicEnv(t)
	c := newClient(t, e.h, "192.0.2.70")
	tid := e.typ.ID.String()
	const start = "2026-10-20T07:00:00Z"

	f := detailsForm(tid, start)
	rec := c.do("POST", e.base()+"/book/details", f)
	mustContain(t, rec, 200, `name="code"`, "Modifier mes coordonnées", e.base()+"/book/edit")
	if e.otp.count() != 1 {
		t.Fatalf("codes envoyés = %d, attendu 1", e.otp.count())
	}
	sent := e.otp.count()

	rec = c.do("POST", e.base()+"/book/edit", f)
	mustContain(t, rec, 200, `name="first_name"`, `value="Alice"`, `value="alice@example.test"`, `name="website"`)
	body := rec.Body.String()
	if strings.Contains(body, `name="code"`) || strings.Contains(body, "alert-error") {
		t.Error("le formulaire ne doit montrer ni champ de code ni erreur")
	}
	if e.otp.count() != sent {
		t.Error("aucun code ne doit être renvoyé")
	}
	if e.appointments(t) != 0 {
		t.Error("aucun RDV ne doit être créé")
	}

	// Créneau forgé : même comportement que le GET (redirection), aucun envoi.
	bad := detailsForm(tid, "2026-10-21T07:00:00Z")
	if rec := c.do("POST", e.base()+"/book/edit", bad); rec.Code != http.StatusSeeOther {
		t.Errorf("créneau forgé: %d, redirection attendue", rec.Code)
	}
	bad = detailsForm("pas-un-uuid", start)
	if rec := c.do("POST", e.base()+"/book/edit", bad); rec.Code != http.StatusSeeOther {
		t.Errorf("motif illisible: %d, redirection attendue", rec.Code)
	}

	// Sans jeton CSRF : refusé.
	req := httptest.NewRequest("POST", e.base()+"/book/edit", strings.NewReader(f.Encode()))
	req.RemoteAddr = "192.0.2.71:4321"
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()
	e.h.ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Errorf("POST sans CSRF: %d, attendu 403", rr.Code)
	}
}

func TestPublicWaitlistVerifyEdit(t *testing.T) {
	t.Parallel()
	e := newPublicEnv(t)
	withWaitlist(t, e)
	c := newClient(t, e.h, "192.0.2.72")
	tid := e.typ.ID.String()

	f := waitlistForm(tid)
	mustContain(t, c.do("POST", e.base()+"/waitlist", f), 200, `name="code"`, "Modifier mes coordonnées", e.base()+"/waitlist/edit")
	sent := e.otp.count()

	rec := c.do("POST", e.base()+"/waitlist/edit", f)
	mustContain(t, rec, 200, `name="earliest"`, `value="2026-10-15"`, `value="Dora"`)
	body := rec.Body.String()
	if strings.Contains(body, `name="code"`) || strings.Contains(body, "alert-error") {
		t.Error("le formulaire ne doit montrer ni champ de code ni erreur")
	}
	if e.otp.count() != sent {
		t.Error("aucun code ne doit être renvoyé")
	}
	var n int
	if err := e.db.QueryRow(context.Background(), "SELECT count(*) FROM waitlist_entry").Scan(&n); err != nil || n != 0 {
		t.Errorf("inscriptions = %d, %v", n, err)
	}

	req := httptest.NewRequest("POST", e.base()+"/waitlist/edit", strings.NewReader(f.Encode()))
	req.RemoteAddr = "192.0.2.73:4321"
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()
	e.h.ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Errorf("POST sans CSRF: %d, attendu 403", rr.Code)
	}
}

func TestPublicDetailsShowsHomeVisitZones(t *testing.T) {
	t.Parallel()
	e := newPublicEnv(t)
	ctx := context.Background()
	q := sqlcgen.New(e.db)
	mins := func(m int) pgtype.Interval {
		return pgtype.Interval{Microseconds: int64(m) * time.Minute.Microseconds(), Valid: true}
	}
	home, err := q.CreateAppointmentType(ctx, sqlcgen.CreateAppointmentTypeParams{
		PracticeID: e.pc.ID, Name: "Séance à domicile", Duration: mins(30), BufferBefore: mins(0), BufferAfter: mins(0),
		OnlineBookable: true, AtHome: true, Color: "#b45309",
	})
	if err != nil {
		t.Fatal(err)
	}
	const start = "2026-10-20T07:00:00Z"
	target := func(id string) string {
		return e.base() + "/book/details?type=" + id + "&start=" + url.QueryEscape(start)
	}
	c := newClient(t, e.h, "192.0.2.74")

	// Liste vide : l'option n'est pas réservable en ligne (404), en tout cas aucune aide de zones.
	if err := q.SetPracticeHomeVisit(ctx, sqlcgen.SetPracticeHomeVisitParams{ID: e.pc.ID, HomeVisitOnline: true, HomeVisitZones: []string{}}); err != nil {
		t.Fatal(err)
	}
	rec := c.do("GET", target(home.ID.String()), nil)
	if strings.Contains(rec.Body.String(), "Zones desservies") {
		t.Error("zones affichées alors que la liste est vide")
	}

	if err := q.SetPracticeHomeVisit(ctx, sqlcgen.SetPracticeHomeVisitParams{
		ID: e.pc.ID, HomeVisitOnline: true, HomeVisitZones: []string{"75011", "Montreuil"},
	}); err != nil {
		t.Fatal(err)
	}
	mustContain(t, c.do("GET", target(home.ID.String()), nil), 200, "Zones desservies : 75011, Montreuil.")
	// Motif au cabinet : jamais d'aide de zones.
	rec = c.do("GET", target(e.typ.ID.String()), nil)
	if strings.Contains(rec.Body.String(), "Zones desservies") {
		t.Error("zones affichées pour un motif au cabinet")
	}
}
