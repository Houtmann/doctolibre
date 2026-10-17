package web_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"doctolibre/internal/domain"
	"doctolibre/internal/store"
	"doctolibre/internal/store/sqlcgen"
	"doctolibre/internal/testutil"
)

func TestPatientPageShowsOnlineBlockedBadge(t *testing.T) {
	e := newPatientEnv(t, domain.RolePractitioner)
	ctx := context.Background()
	p := e.newPatient(t, "Alex", "Dupont", "+33611111111")
	pr := testutil.NewPractitioner(t, e.db, e.actor.PracticeID)
	typ := testutil.NewAppointmentType(t, e.db, e.actor.PracticeID)
	start := time.Date(2026, 10, 5, 9, 0, 0, 0, time.UTC)
	ap, err := store.CreateAppointment(ctx, sqlcgen.New(e.db), store.NewAppointment{
		ID: uuid.New(), PracticeID: e.actor.PracticeID, PractitionerID: pr.ID, PatientID: p.ID, AppointmentTypeID: typ.ID,
		Start: start, End: start.Add(30 * time.Minute), Source: "staff",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.db.Exec(ctx, "UPDATE appointment SET status = 'no_show' WHERE id = $1", ap.ID); err != nil {
		t.Fatal(err)
	}
	url := "/admin/patients/" + p.ID.String()
	if page := e.c.do("GET", url, nil).Body.String(); strings.Contains(page, "Réservation en ligne bloquée") {
		t.Error("badge affiché alors que le seuil est désactivé")
	}
	if _, err := e.db.Exec(ctx, "UPDATE practice SET no_show_block_threshold = 1 WHERE id = $1", e.actor.PracticeID); err != nil {
		t.Fatal(err)
	}
	if page := e.c.do("GET", url, nil).Body.String(); !strings.Contains(page, "Réservation en ligne bloquée") {
		t.Error("badge « Réservation en ligne bloquée » absent")
	}
}
