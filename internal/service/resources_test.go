package service_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"doctolibre/internal/domain"
	"doctolibre/internal/service"
	"doctolibre/internal/store"
	"doctolibre/internal/store/sqlcgen"
	"doctolibre/internal/testutil"
)

// resourceEnv : deux praticiens du même cabinet, mêmes plages, et un motif qui exige une ressource.
type resourceEnv struct {
	*bookingEnv
	other    *bookingEnv // même service, second praticien
	resource sqlcgen.Resource
}

func newResourceEnv(t *testing.T) *resourceEnv {
	t.Helper()
	e := newBookingEnv(t, thursday)
	pr2 := testutil.NewPractitioner(t, e.db, e.pc.ID)
	addRule(t, e.db, pr2.ID, 1, 9, 12, nil)
	o := *e
	o.pr = pr2
	ctx := context.Background()
	q := sqlcgen.New(e.db)
	res, err := q.CreateResource(ctx, sqlcgen.CreateResourceParams{PracticeID: e.pc.ID, Name: "Box 1"})
	if err != nil {
		t.Fatal(err)
	}
	if err := q.SetTypeResource(ctx, sqlcgen.SetTypeResourceParams{AppointmentTypeID: e.typ.ID, ResourceID: res.ID}); err != nil {
		t.Fatal(err)
	}
	return &resourceEnv{bookingEnv: e, other: &o, resource: res}
}

func (r *resourceEnv) slotFree(t *testing.T, e *bookingEnv, start time.Time) bool {
	t.Helper()
	err := e.svc.CheckSlot(context.Background(), e.pr.ID, e.typ.ID, start)
	if err != nil && !errors.Is(err, domain.ErrSlotTaken) {
		t.Fatal(err)
	}
	return err == nil
}

func TestResourceConcurrentBookingTwoPractitioners(t *testing.T) {
	t.Parallel()
	r := newResourceEnv(t)
	a := r.start(t, tuesday9, patient("Alice", "Durand", "06 11 11 11 11"), "+33611111111")
	b := r.other.start(t, tuesday9, patient("Bruno", "Martin", "06 22 22 22 22"), "+33622222222")

	var wg sync.WaitGroup
	gate := make(chan struct{})
	errs := make([]error, 2)
	for i, in := range []service.BookInput{a, b} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-gate
			_, errs[i] = r.svc.Book(context.Background(), in)
		}()
	}
	close(gate)
	wg.Wait()

	ok, taken := 0, 0
	for _, err := range errs {
		switch {
		case err == nil:
			ok++
		case errors.Is(err, domain.ErrSlotTaken):
			taken++
		default:
			t.Errorf("erreur inattendue: %v", err)
		}
	}
	if ok != 1 || taken != 1 {
		t.Fatalf("succès=%d pris=%d, attendu 1 et 1 (%v)", ok, taken, errs)
	}
	if n := r.count(t, "SELECT count(*) FROM appointment WHERE status = 'booked'"); n != 1 {
		t.Errorf("%d rendez-vous en base, attendu 1", n)
	}
	if n := r.count(t, "SELECT count(*) FROM appointment_resource WHERE active"); n != 1 {
		t.Errorf("%d occupations actives, attendu 1", n)
	}
}

func TestResourceTakenHidesSlotsAndCancelFrees(t *testing.T) {
	t.Parallel()
	r := newResourceEnv(t)
	_, token := r.bookAt(t, tuesday9, "Alice", "06 11 11 11 11", "+33611111111")

	if r.slotFree(t, r.other, tuesday9) {
		t.Error("le créneau de 9 h est proposé alors que la ressource est prise")
	}
	slots, err := r.other.svc.AvailableSlots(context.Background(), r.other.pr.ID, r.typ.ID, tuesday9, tuesday9.Add(3*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range slots {
		if s.Start.Equal(tuesday9) {
			t.Error("créneau de 9 h dans la liste de l'autre praticien")
		}
	}
	if !r.slotFree(t, r.other, tuesday9.Add(30*time.Minute)) {
		t.Error("9 h 30 devrait rester libre")
	}

	if err := r.svc.CancelByPatient(context.Background(), token); err != nil {
		t.Fatal(err)
	}
	if !r.slotFree(t, r.other, tuesday9) {
		t.Error("l'annulation n'a pas libéré la ressource")
	}
	if n := r.count(t, "SELECT count(*) FROM appointment_resource WHERE active"); n != 0 {
		t.Errorf("%d occupations actives après annulation", n)
	}
}

func TestResourceRescheduleFreesOldSlot(t *testing.T) {
	t.Parallel()
	r := newResourceEnv(t)
	_, token := r.bookAt(t, tuesday9, "Alice", "06 11 11 11 11", "+33611111111")
	later := tuesday9.Add(2 * time.Hour)
	if err := r.svc.RescheduleByPatient(context.Background(), token, later); err != nil {
		t.Fatal(err)
	}
	if !r.slotFree(t, r.other, tuesday9) {
		t.Error("l'ancien créneau devrait être libre")
	}
	if r.slotFree(t, r.other, later) {
		t.Error("le nouveau créneau devrait être pris")
	}
}

func TestResourceStaffPathsTranslateToSlotTaken(t *testing.T) {
	t.Parallel()
	r := newResourceEnv(t)
	ctx := context.Background()
	pat := testutil.NewPatient(t, r.db, r.pc.ID)
	mk := func(prID [16]byte, start time.Time) (sqlcgen.Appointment, error) {
		return store.CreateAppointment(ctx, sqlcgen.New(r.db), store.NewAppointment{
			PracticeID: r.pc.ID, PractitionerID: prID, PatientID: pat.ID, AppointmentTypeID: r.typ.ID,
			Start: start, End: start.Add(30 * time.Minute), Source: "staff",
		})
	}
	first, err := mk(r.pr.ID, tuesday9)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := mk(r.other.pr.ID, tuesday9.Add(15*time.Minute)); !errors.Is(err, domain.ErrSlotTaken) {
		t.Fatalf("création staff en conflit = %v, attendu ErrSlotTaken", err)
	}
	second, err := mk(r.other.pr.ID, tuesday9.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	// Déplacement staff de l'autre praticien sur la ressource prise.
	if _, err := store.MoveAppointment(ctx, sqlcgen.New(r.db), r.pc.ID, second.ID, tuesday9, tuesday9.Add(30*time.Minute), 0, 0); !errors.Is(err, domain.ErrSlotTaken) {
		t.Fatalf("déplacement en conflit = %v, attendu ErrSlotTaken", err)
	}
	// Libérer le premier puis déplacer : passe, et la ressource suit.
	if _, err := sqlcgen.New(r.db).CancelAppointment(ctx, sqlcgen.CancelAppointmentParams{ID: first.ID, PracticeID: r.pc.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.MoveAppointment(ctx, sqlcgen.New(r.db), r.pc.ID, second.ID, tuesday9, tuesday9.Add(30*time.Minute), 0, 0); err != nil {
		t.Fatalf("déplacement après libération: %v", err)
	}
	// done / no_show gardent la ressource occupée.
	if _, err := r.db.Exec(ctx, "UPDATE appointment SET status = 'done' WHERE id = $1", second.ID); err != nil {
		t.Fatal(err)
	}
	if r.slotFree(t, r.bookingEnv, tuesday9) {
		t.Error("un RDV honoré doit garder la ressource")
	}
}

func TestNoResourceOrInactiveResourceUnchanged(t *testing.T) {
	t.Parallel()
	r := newResourceEnv(t)
	ctx := context.Background()
	r.bookAt(t, tuesday9, "Alice", "06 11 11 11 11", "+33611111111")
	// Ressource désactivée : plus exigée pour les nouveaux RDV.
	if _, err := sqlcgen.New(r.db).UpdateResource(ctx, sqlcgen.UpdateResourceParams{ID: r.resource.ID, PracticeID: r.pc.ID, Name: "Box 1", Active: false}); err != nil {
		t.Fatal(err)
	}
	if !r.slotFree(t, r.other, tuesday9) {
		t.Error("ressource inactive : le créneau devrait être libre")
	}
	// Motif sans ressource : deux praticiens, même heure.
	if err := sqlcgen.New(r.db).ClearTypeResource(ctx, r.typ.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := r.other.svc.Book(ctx, r.other.start(t, tuesday9, patient("Bruno", "Martin", "06 22 22 22 22"), "+33622222222")); err != nil {
		t.Fatalf("motif sans ressource: %v", err)
	}
	if n := r.count(t, "SELECT count(*) FROM appointment_resource WHERE appointment_id IN (SELECT id FROM appointment WHERE practitioner_id = $1)", r.other.pr.ID); n != 0 {
		t.Errorf("%d occupations pour un motif sans ressource", n)
	}
}

func TestResourceCRUDTenancy(t *testing.T) {
	t.Parallel()
	r := newResourceEnv(t)
	ctx := context.Background()
	svc := &service.Practice{Pool: r.db}
	other := testutil.NewPractice(t, r.db)
	foreign := service.Actor{PracticeID: other.ID}
	mine := service.Actor{PracticeID: r.pc.ID}

	if _, err := svc.CreateResource(ctx, mine, "  "); err == nil {
		t.Error("nom vide accepté")
	}
	// Un motif ne peut pas viser la ressource d'un autre cabinet.
	foreignRes, err := svc.CreateResource(ctx, foreign, "Box étranger")
	if err != nil {
		t.Fatal(err)
	}
	err = svc.UpdateAppointmentType(ctx, mine, r.typ.ID, service.AppointmentTypeInput{
		Name: "Séance", DurationMin: "30", OnlineBookable: true, ResourceID: foreignRes.ID.String()})
	var ve domain.ValidationErrors
	if !errors.As(err, &ve) && !errors.As(err, new(domain.ErrValidation)) {
		t.Fatalf("ressource d'un autre cabinet = %v, attendu une erreur de validation", err)
	}
	if err := svc.SetResourceActive(ctx, foreign, r.resource.ID, false); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("désactivation inter-cabinets = %v, attendu ErrNotFound", err)
	}
	// Lier puis délier via le formulaire du motif.
	if err := svc.UpdateAppointmentType(ctx, mine, r.typ.ID, service.AppointmentTypeInput{Name: "Séance", DurationMin: "30", OnlineBookable: true}); err != nil {
		t.Fatal(err)
	}
	links, err := svc.TypeResources(ctx, mine)
	if err != nil || len(links) != 0 {
		t.Fatalf("liaisons = %v, err = %v", links, err)
	}
	if err := svc.UpdateAppointmentType(ctx, mine, r.typ.ID, service.AppointmentTypeInput{
		Name: "Séance", DurationMin: "30", OnlineBookable: true, ResourceID: r.resource.ID.String()}); err != nil {
		t.Fatal(err)
	}
	if links, _ = svc.TypeResources(ctx, mine); links[r.typ.ID] != r.resource.ID {
		t.Errorf("liaison = %v", links)
	}
}
