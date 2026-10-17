package web_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"doctolibre/internal/service"
	"doctolibre/internal/testutil"
)

// nextTuesday9h : 9 h à Paris le 27/10 (heure d'hiver depuis le 25/10).
var nextTuesday9h = time.Date(2026, 10, 27, 8, 0, 0, 0, time.UTC)

// seriesEnv réserve trois mardis (20/10 à 9 h et 10 h, 27/10 à 9 h) et les rattache à une série
// dont le lien /s/{jeton} est celui que dérive le service.
type seriesEnv struct {
	*publicEnv
	token   string
	manage  []string // chemins /r/{jeton} des trois séances
	apptIDs []string
}

func newSeriesEnv(t *testing.T) seriesEnv {
	t.Helper()
	e := newPublicEnv(t)
	s := seriesEnv{publicEnv: e}
	for _, d := range []time.Time{tuesday9h, tuesday9h.Add(time.Hour), nextTuesday9h} {
		s.manage = append(s.manage, e.bookDirect(t, d, "Alice", "06 11 11 11 11", "+33611111111"))
	}
	ctx := context.Background()
	var planID, patientID string
	if err := e.db.QueryRow(ctx, "SELECT patient_id::text FROM appointment LIMIT 1").Scan(&patientID); err != nil {
		t.Fatal(err)
	}
	if err := e.db.QueryRow(ctx, `INSERT INTO care_plan (practice_id, patient_id, practitioner_id, appointment_type_id, label, sessions_prescribed, frequency_per_week, start_date)
		VALUES ($1, $2, $3, $4, 'Texte du cabinet', 10, 1, '2026-10-20') RETURNING id::text`, e.pc.ID, patientID, e.pr.ID, e.typ.ID).Scan(&planID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.db.Exec(ctx, "UPDATE appointment SET care_plan_id = $1::uuid", planID); err != nil {
		t.Fatal(err)
	}
	var id [16]byte
	if err := e.db.QueryRow(ctx, "SELECT id FROM care_plan WHERE id = $1::uuid", planID).Scan(&id); err != nil {
		t.Fatal(err)
	}
	key := service.NewManageKey(testutil.ManageSecret)
	s.token = key.PlanToken(id, 1)
	if _, err := e.db.Exec(ctx, "UPDATE care_plan SET token_hash = $1 WHERE id = $2::uuid", service.ManageTokenHash(s.token), planID); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestSeriesPageListsSessionsWithManageLinks(t *testing.T) {
	t.Parallel()
	s := newSeriesEnv(t)
	c := newClient(t, s.h, "192.0.2.70")
	// La 2e séance est annulée : plus de lien, mais elle reste listée.
	if _, err := s.db.Exec(context.Background(), `UPDATE appointment SET status = 'cancelled', cancelled_at = now()
		WHERE lower(during) = $1`, tuesday9h.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	rec := c.do("GET", "/s/"+s.token, nil)
	mustContain(t, rec, 200, "Vos séances", "Alice", s.manage[0], s.manage[2], "/s/"+s.token+"/ics", "Annulé")
	if body := rec.Body.String(); strings.Contains(body, s.manage[1]) {
		t.Error("la séance annulée a un lien de gestion")
	} else if strings.Contains(body, "Texte du cabinet") {
		t.Error("le libellé libre de la série ne doit pas être exposé")
	}
	if rec.Header().Get("Cache-Control") != "no-store" || rec.Header().Get("Referrer-Policy") != "no-referrer" {
		t.Errorf("en-têtes : %v", rec.Header())
	}
	// Le lien de gestion d'une séance fonctionne.
	mustContain(t, c.do("GET", s.manage[0], nil), 200, "Déplacer le rendez-vous")
}

func TestSeriesICSHoldsUpcomingNonCancelledSessionsOnly(t *testing.T) {
	t.Parallel()
	s := newSeriesEnv(t)
	c := newClient(t, s.h, "192.0.2.71")
	ctx := context.Background()
	if _, err := s.db.Exec(ctx, "UPDATE appointment SET status = 'cancelled', cancelled_at = now() WHERE lower(during) = $1", tuesday9h.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	// La 1re séance est passée (honorée).
	if _, err := s.db.Exec(ctx, "UPDATE appointment SET status = 'done' WHERE lower(during) = $1", tuesday9h); err != nil {
		t.Fatal(err)
	}
	rec := c.do("GET", "/s/"+s.token+"/ics", nil)
	if rec.Code != 200 || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/calendar") {
		t.Fatalf("ICS : %d %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	if n := strings.Count(rec.Body.String(), "BEGIN:VEVENT"); n != 1 {
		t.Errorf("%d événements, attendu 1 (séance à venir non annulée)\n%s", n, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "DTSTART:20261027T080000Z") {
		t.Errorf("mauvaise séance dans l'ICS\n%s", rec.Body.String())
	}
	if rec.Header().Get("Cache-Control") != "no-store" || rec.Header().Get("Referrer-Policy") != "no-referrer" {
		t.Errorf("en-têtes : %v", rec.Header())
	}
}

func TestSeriesLinkRejectsForgedOrRevokedTokens(t *testing.T) {
	t.Parallel()
	s := newSeriesEnv(t)
	c := newClient(t, s.h, "192.0.2.72")
	for _, p := range []string{"/s/inconnu", "/s/" + s.token + "x", "/s/inconnu/ics", "/s/" + strings.TrimPrefix(s.manage[0], "/r/")} {
		if rec := c.do("GET", p, nil); rec.Code != 404 {
			t.Errorf("GET %s = %d, attendu 404", p, rec.Code)
		}
	}
	if _, err := s.db.Exec(context.Background(), "UPDATE care_plan SET token_version = token_version + 1, token_hash = NULL"); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"/s/" + s.token, "/s/" + s.token + "/ics"} {
		if rec := c.do("GET", p, nil); rec.Code != 404 {
			t.Errorf("GET %s après révocation = %d, attendu 404", p, rec.Code)
		}
	}
}

// /s/ partage le seau « manage » de /r/ : des sondages sur l'un épuisent l'autre.
func TestSeriesLinkSharesManageRateLimit(t *testing.T) {
	t.Parallel()
	s := newSeriesEnv(t)
	c := newClient(t, s.h, "192.0.2.73")
	got429 := false
	for i := 0; i < 200 && !got429; i++ {
		got429 = c.do("GET", "/r/inconnu", nil).Code == 429
	}
	if !got429 {
		t.Fatal("aucun 429 sur /r/ : limiteur absent ?")
	}
	if rec := c.do("GET", "/s/"+s.token, nil); rec.Code != 429 {
		t.Errorf("GET /s/ après épuisement du seau = %d, attendu 429", rec.Code)
	}
}
