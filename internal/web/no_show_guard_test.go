package web_test

import (
	"context"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"doctolibre/internal/service"
	"doctolibre/internal/store"
	"doctolibre/internal/store/sqlcgen"
)

// Le patient connu avec une absence récente et un seuil de 1 est refusé après le
// code, avec un message neutre et le téléphone du cabinet ; le message ne parle pas d'absence.
func TestPublicBookingBlockedByNoShows(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	e := newPublicEnv(t)
	q := sqlcgen.New(e.db)
	if _, err := e.db.Exec(ctx, "UPDATE practice SET phone = '01 23 45 67 89' WHERE id = $1", e.pc.ID); err != nil {
		t.Fatal(err)
	}

	// Patient existant (créé par une première réservation) avec une absence récente.
	const start = "2026-10-20T07:00:00Z"
	first := service.VerifyInput{
		PractitionerID: e.pr.ID, TypeID: e.typ.ID, Start: time.Date(2026, 10, 27, 8, 0, 0, 0, time.UTC), Channel: "sms",
		Patient: service.PatientDetails{FirstName: "Alice", LastName: "Durand", Phone: "06 11 11 11 11", SMSOptIn: true},
	}
	v, err := e.svc.StartVerification(ctx, first)
	if err != nil {
		t.Fatal(err)
	}
	res, err := e.svc.Book(ctx, service.BookInput{VerifyInput: first, ChallengeID: v.ChallengeID, Code: e.otp.code("+33611111111")})
	if err != nil {
		t.Fatal(err)
	}
	missed := time.Date(2026, 10, 5, 9, 0, 0, 0, time.UTC)
	ap, err := store.CreateAppointment(ctx, q, store.NewAppointment{
		ID: uuid.New(), PracticeID: e.pc.ID, PractitionerID: e.pr.ID, PatientID: res.Appointment.PatientID, AppointmentTypeID: e.typ.ID,
		Start: missed, End: missed.Add(30 * time.Minute), Source: "staff",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.db.Exec(ctx, "UPDATE appointment SET status = 'no_show' WHERE id = $1", ap.ID); err != nil {
		t.Fatal(err)
	}
	if err := q.SetPracticeNoShowThreshold(ctx, sqlcgen.SetPracticeNoShowThresholdParams{
		ID: e.pc.ID, NoShowBlockThreshold: pgtype.Int2{Int16: 1, Valid: true},
	}); err != nil {
		t.Fatal(err)
	}
	before := e.appointments(t)

	c := newClient(t, e.h, "192.0.2.60")
	detailsURL := e.base() + "/book/details?type=" + e.typ.ID.String() + "&start=" + url.QueryEscape(start)
	c.do("GET", detailsURL, nil)
	rec := c.do("POST", detailsURL, detailsForm(e.typ.ID.String(), start))
	m := challengeRe.FindStringSubmatch(rec.Body.String())
	if m == nil {
		t.Fatalf("challenge_id absent: %d", rec.Code)
	}
	confirm := detailsForm(e.typ.ID.String(), start)
	confirm.Set("challenge_id", m[1])
	confirm.Set("code", e.otp.code("+33611111111"))
	rec = c.do("POST", e.base()+"/book/confirm", confirm)
	mustContain(t, rec, 403, "Cette réservation en ligne n&#39;est pas possible.", "01 23 45 67 89")
	if strings.Contains(rec.Body.String(), "Le formulaire contient des erreurs") {
		t.Error("le refus ne doit pas apparaître comme une erreur de formulaire")
	}
	body := strings.ToLower(rec.Body.String())
	for _, w := range []string{"absence", "manqué", "no-show"} {
		if strings.Contains(body, w) {
			t.Errorf("le message ne doit pas mentionner %q", w)
		}
	}
	if e.appointments(t) != before {
		t.Error("rendez-vous créé malgré le refus")
	}
}
