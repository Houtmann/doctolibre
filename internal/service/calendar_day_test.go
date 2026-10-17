package service_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"doctolibre/internal/domain"
	"doctolibre/internal/service"
	"doctolibre/internal/store/sqlcgen"
	"doctolibre/internal/testutil"
)

// calAt retourne une copie du service d'agenda avec une autre horloge.
func (f calFixture) calAt(t time.Time) *service.Calendar {
	c := *f.cal
	c.Clock = testutil.NewFixedClock(t)
	return &c
}

func TestMyDayListsOrderedWithoutCancelledAndSummary(t *testing.T) {
	t.Parallel()
	f := newCalFixture(t)
	ctx := context.Background()
	var ids []uuid.UUID
	for _, hm := range []string{"10:00", "09:00", "11:00"} { // créés dans le désordre
		ap, err := f.cal.Create(ctx, f.act, f.quick("2026-10-20", hm, false))
		if err != nil {
			t.Fatalf("création %s: %v", hm, err)
		}
		ids = append(ids, ap.ID)
	}
	if err := f.cal.Cancel(ctx, f.act, ids[2], ""); err != nil { // 11:00 annulé
		t.Fatal(err)
	}
	if err := f.cal.SetStatus(ctx, f.act, ids[1], "done"); err != nil { // 09:00 fait
		t.Fatal(err)
	}
	v, err := f.cal.Day(ctx, f.act, uuid.Nil, "2026-10-20")
	if err != nil {
		t.Fatal(err)
	}
	if len(v.Appts) != 2 || v.Appts[0].ID != ids[1] || v.Appts[1].ID != ids[0] {
		t.Fatalf("liste: %+v", v.Appts)
	}
	if v.Appts[0].Phone == "" || v.Appts[0].Patient != "Léa Durand" || v.Appts[0].StatusName != "Honoré" {
		t.Errorf("champs: %+v", v.Appts[0])
	}
	if v.Summary.Done != 1 || v.Summary.Upcoming != 1 || v.Summary.NoShow != 0 {
		t.Errorf("récapitulatif: %+v", v.Summary)
	}
	if v.PrevDate != "2026-10-19" || v.NextDate != "2026-10-21" {
		t.Errorf("navigation: %s %s", v.PrevDate, v.NextDate)
	}
	if _, err := f.cal.Day(ctx, f.act, uuid.Nil, "pas-une-date"); len(domain.FieldErrors(err)) == 0 {
		t.Errorf("date invalide acceptée: %v", err)
	}
}

func TestMyDayTodayInPracticeTimezoneAndDST(t *testing.T) {
	t.Parallel()
	f := newCalFixture(t)
	ctx := context.Background()
	// 25 octobre 2026 : retour à l'heure d'hiver, journée de 25 h. 00:30 CEST = 22:30 UTC la veille.
	first, err := f.cal.Create(ctx, f.act, f.quick("2026-10-25", "00:30", true))
	if err != nil {
		t.Fatal(err)
	}
	last, err := f.cal.Create(ctx, f.act, f.quick("2026-10-25", "23:30", true))
	if err != nil {
		t.Fatal(err)
	}
	for _, hm := range [][2]string{{"2026-10-24", "23:30"}, {"2026-10-26", "01:00"}} { // voisins : exclus
		if _, err := f.cal.Create(ctx, f.act, f.quick(hm[0], hm[1], true)); err != nil {
			t.Fatal(err)
		}
	}

	// À 00:30 heure de Paris le 25 (22:30 UTC le 24), « aujourd'hui » est le 25, pas le 24.
	cal := f.calAt(time.Date(2026, 10, 24, 22, 30, 0, 0, time.UTC))
	v, err := cal.Day(ctx, f.act, uuid.Nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if v.Date != "2026-10-25" || !v.IsToday || v.PrevDate != "2026-10-24" || v.NextDate != "2026-10-26" {
		t.Errorf("jour courant: %+v", v)
	}
	if len(v.Appts) != 2 || v.Appts[0].ID != first.ID || v.Appts[1].ID != last.ID {
		t.Fatalf("RDV du 25 (journée de 25 h): %+v", v.Appts)
	}
	if h := v.Appts[1].Start.Hour(); h != 23 {
		t.Errorf("heure murale du dernier RDV: %d", h)
	}

	// 28 mars 2027 : passage à l'heure d'été, journée de 23 h.
	for _, hm := range []string{"00:30", "23:30"} {
		if _, err := f.cal.Create(ctx, f.act, f.quick("2027-03-28", hm, true)); err != nil {
			t.Fatal(err)
		}
	}
	v, err = f.cal.Day(ctx, f.act, uuid.Nil, "2027-03-28")
	if err != nil {
		t.Fatal(err)
	}
	if len(v.Appts) != 2 || v.NextDate != "2027-03-29" || v.PrevDate != "2027-03-27" {
		t.Errorf("journée de 23 h: %+v", v)
	}
	// 00:30 à Paris le 28 mars (CET, 23:30 UTC le 27) est déjà « aujourd'hui » le 28.
	cal = f.calAt(time.Date(2027, 3, 27, 23, 30, 0, 0, time.UTC))
	if v, err = cal.Day(ctx, f.act, uuid.Nil, ""); err != nil || v.Date != "2027-03-28" {
		t.Errorf("aujourd'hui le jour du changement d'heure: %v %v", v.Date, err)
	}
}

func TestSetStatusRecordsWhoAndWhen(t *testing.T) {
	t.Parallel()
	f := newCalFixture(t)
	ctx := context.Background()
	ap, err := f.cal.Create(ctx, f.act, f.quick("2026-10-20", "09:00", false))
	if err != nil {
		t.Fatal(err)
	}
	q := sqlcgen.New(f.db)
	before, err := q.GetAppointment(ctx, sqlcgen.GetAppointmentParams{ID: ap.ID, PracticeID: f.act.PracticeID})
	if err != nil || before.StatusChangedBy.Valid || before.StatusChangedAt.Valid {
		t.Fatalf("avant: %+v %v", before, err)
	}
	if err := f.cal.SetStatus(ctx, f.act, ap.ID, "no_show"); err != nil {
		t.Fatal(err)
	}
	after, err := q.GetAppointment(ctx, sqlcgen.GetAppointmentParams{ID: ap.ID, PracticeID: f.act.PracticeID})
	if err != nil {
		t.Fatal(err)
	}
	if !after.StatusChangedBy.Valid || uuid.UUID(after.StatusChangedBy.Bytes) != f.act.UserID {
		t.Errorf("status_changed_by: %+v", after.StatusChangedBy)
	}
	if !after.StatusChangedAt.Valid || !after.StatusChangedAt.Time.Equal(f.cal.Clock.Now()) {
		t.Errorf("status_changed_at: %+v", after.StatusChangedAt)
	}
	// Annuler le pointage repasse à « booked ».
	if err := f.cal.SetStatus(ctx, f.act, ap.ID, "booked"); err != nil {
		t.Fatal(err)
	}
	v, err := f.cal.Day(ctx, f.act, uuid.Nil, "2026-10-20")
	if err != nil || len(v.Appts) != 1 || v.Appts[0].Status != "booked" {
		t.Errorf("après annulation du pointage: %+v %v", v.Appts, err)
	}
}

func TestMyDayForbiddenForOtherPractitioner(t *testing.T) {
	t.Parallel()
	f := newCalFixture(t)
	other := f.act
	other.Role = domain.RolePractitioner
	other.PractitionerID = uuid.New()
	if _, err := f.cal.Day(context.Background(), other, uuid.New(), "2026-10-20"); !errors.Is(err, domain.ErrForbidden) {
		t.Errorf("praticien tiers: %v", err)
	}
}
