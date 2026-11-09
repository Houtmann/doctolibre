package service_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"doctolibre/internal/domain"
	"doctolibre/internal/service"
)

var (
	rebookA = time.Date(2026, 10, 27, 9, 0, 0, 0, paris)
	rebookB = time.Date(2026, 10, 27, 10, 0, 0, 0, paris)
)

// cancelled réserve puis fait annuler par le patient ; retourne le jeton du RDV annulé.
func (e *bookingEnv) cancelled(t *testing.T) string {
	t.Helper()
	_, token := e.bookAt(t, tuesday9, "Alice", "06 11 11 11 11", "+33611111111")
	if err := e.svc.CancelByPatient(context.Background(), token); err != nil {
		t.Fatal(err)
	}
	return token
}

func TestRebookByPatient(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	e := newBookingEnv(t, thursday)
	sink := &recordSink{}
	e.svc.Events = sink
	token := e.cancelled(t)

	c, err := e.svc.Confirmation(ctx, token)
	if err != nil || !c.Rebookable {
		t.Fatalf("Rebookable = %v, err = %v", c.Rebookable, err)
	}
	slots, err := e.svc.RebookSlots(ctx, token, rebookA, rebookA.Add(time.Second))
	if err != nil || len(slots) != 1 || !slots[0].Start.Equal(rebookA) {
		t.Fatalf("RebookSlots = %v, %v", slots, err)
	}
	res, err := e.svc.RebookByPatient(ctx, token, rebookA, false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Token == "" || res.Token == token {
		t.Errorf("le nouveau RDV doit avoir son propre jeton (%q)", res.Token)
	}
	if res.Appointment.PatientID != c.Appointment.PatientID || res.Appointment.PractitionerID != e.pr.ID ||
		res.Appointment.AppointmentTypeID != e.typ.ID || res.Appointment.Source != "online" || res.Appointment.Status != "booked" {
		t.Errorf("RDV créé inattendu : %+v", res.Appointment)
	}
	if got := res.Appointment.VerifiedChannel.String; got != "sms" {
		t.Errorf("verified_channel = %q, want sms", got)
	}
	if !res.Appointment.During.Lower.Time.Equal(rebookA) {
		t.Errorf("début = %v", res.Appointment.During.Lower.Time)
	}
	// Le nouveau lien fonctionne, l'ancien reste celui du RDV annulé.
	if st, _, _ := e.status(t, res.Token); st != "booked" {
		t.Errorf("statut du nouveau RDV = %q", st)
	}
	if st, _, _ := e.status(t, token); st != "cancelled" {
		t.Errorf("statut de l'ancien RDV = %q", st)
	}
	// Message de confirmation normal, dans la transaction ; rien pour l'OTP.
	if n := len(only(outMsgs(t, e.db, e.pc.ID), service.TemplateConfirmation)); n < 2 {
		t.Errorf("%d confirmation(s) enfilée(s), attendu 2 (RDV initial + reprise)", n)
	}
	if got := sink.got(); len(got) == 0 || got[len(got)-1] != service.EventAppointmentCreated {
		t.Errorf("événements = %v", got)
	}
	// Le bouton ne propose plus la reprise directe.
	if c, _ := e.svc.Confirmation(ctx, token); c.Rebookable {
		t.Error("Rebookable alors que le RDV est déjà repris")
	}
}

func TestRebookNotCancelledRefused(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	e := newBookingEnv(t, thursday)
	_, token := e.bookAt(t, tuesday9, "Alice", "06 11 11 11 11", "+33611111111")
	if _, err := e.svc.RebookByPatient(ctx, token, rebookA, false); !errors.Is(err, service.ErrRebookClosed) {
		t.Errorf("RDV non annulé : %v, want ErrRebookClosed", err)
	}
	if _, err := e.svc.RebookSlots(ctx, token, rebookA, rebookA.Add(time.Hour)); !errors.Is(err, service.ErrRebookClosed) {
		t.Errorf("RebookSlots sur RDV non annulé : %v", err)
	}
	if _, err := e.svc.RebookByPatient(ctx, "inconnu", rebookA, false); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("jeton inconnu : %v, want ErrNotFound", err)
	}
	if n := e.count(t, "SELECT count(*) FROM appointment"); n != 1 {
		t.Errorf("%d RDV, attendu 1", n)
	}
}

func TestRebookWindow30Days(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	e := newBookingEnv(t, thursday)
	token := e.cancelled(t) // annulé à `thursday`
	// Exactement 30 jours après : encore permis (mardi 17 novembre 2026, 9 h à Paris en CET).
	e.clock.Advance(30 * 24 * time.Hour)
	start := time.Date(2026, 11, 17, 9, 0, 0, 0, paris)
	if c, err := e.svc.Confirmation(ctx, token); err != nil || !c.Rebookable {
		t.Fatalf("à J+30 : Rebookable = %v, %v", c.Rebookable, err)
	}
	if _, err := e.svc.RebookSlots(ctx, token, start, start.Add(time.Second)); err != nil {
		t.Fatalf("à J+30 : %v", err)
	}
	// Une seconde de plus : refusé, partout.
	e.clock.Advance(time.Second)
	if c, _ := e.svc.Confirmation(ctx, token); c.Rebookable {
		t.Error("Rebookable après 30 jours")
	}
	if _, err := e.svc.RebookSlots(ctx, token, start, start.Add(time.Second)); !errors.Is(err, service.ErrRebookClosed) {
		t.Errorf("RebookSlots après 30 j : %v", err)
	}
	if _, err := e.svc.RebookByPatient(ctx, token, start, false); !errors.Is(err, service.ErrRebookClosed) {
		t.Errorf("RebookByPatient après 30 j : %v", err)
	}
	if n := e.count(t, "SELECT count(*) FROM appointment WHERE status = 'booked'"); n != 0 {
		t.Errorf("%d RDV réservé(s)", n)
	}
}

func TestRebookBlockedByNoShows(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	e := newBookingEnv(t, thursday)
	token := e.cancelled(t)
	c, err := e.svc.Confirmation(ctx, token)
	if err != nil {
		t.Fatal(err)
	}
	one := int16(1)
	e.setThreshold(t, &one)
	e.addNoShow(t, c.Appointment.PatientID, thursday.AddDate(0, 0, -10))

	if c, _ := e.svc.Confirmation(ctx, token); c.Rebookable {
		t.Error("Rebookable alors que le patient est bloqué")
	}
	if _, err := e.svc.RebookSlots(ctx, token, rebookA, rebookA.Add(time.Second)); !errors.Is(err, domain.ErrOnlineBookingBlocked) {
		t.Errorf("RebookSlots : %v, want ErrOnlineBookingBlocked", err)
	}
	if _, err := e.svc.RebookByPatient(ctx, token, rebookA, false); !errors.Is(err, domain.ErrOnlineBookingBlocked) {
		t.Errorf("RebookByPatient : %v, want ErrOnlineBookingBlocked", err)
	}
	if n := e.count(t, "SELECT count(*) FROM appointment WHERE status = 'booked'"); n != 0 {
		t.Errorf("%d RDV réservé(s)", n)
	}
}

func TestRebookTypeUnavailable(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	for _, set := range []string{"online_bookable = false", "active = false"} {
		e := newBookingEnv(t, thursday)
		token := e.cancelled(t)
		mustExec(t, e.db, "UPDATE appointment_type SET "+set+" WHERE id = $1", e.typ.ID)
		if c, _ := e.svc.Confirmation(ctx, token); c.Rebookable {
			t.Errorf("%s : Rebookable", set)
		}
		if _, err := e.svc.RebookByPatient(ctx, token, rebookA, false); !errors.Is(err, service.ErrRebookClosed) {
			t.Errorf("%s : %v, want ErrRebookClosed", set, err)
		}
		if _, err := e.svc.RebookSlots(ctx, token, rebookA, rebookA.Add(time.Second)); !errors.Is(err, service.ErrRebookClosed) {
			t.Errorf("%s : RebookSlots %v", set, err)
		}
	}
}

func TestRebookReplayCreatesNoDuplicate(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	e := newBookingEnv(t, thursday)
	token := e.cancelled(t)
	if _, err := e.svc.RebookByPatient(ctx, token, rebookA, false); err != nil {
		t.Fatal(err)
	}
	// POST rejoué, même créneau puis autre créneau : refusé, aucun doublon.
	for _, s := range []time.Time{rebookA, rebookB} {
		if _, err := e.svc.RebookByPatient(ctx, token, s, false); !errors.Is(err, service.ErrAlreadyRebooked) {
			t.Errorf("rejeu %v : %v, want ErrAlreadyRebooked", s, err)
		}
	}
	if n := e.count(t, "SELECT count(*) FROM appointment"); n != 2 {
		t.Errorf("%d RDV, attendu 2 (annulé + repris)", n)
	}
	if n := e.count(t, "SELECT count(*) FROM appointment WHERE rebooked_appointment_id IS NOT NULL"); n != 1 {
		t.Errorf("%d lien(s) de reprise, attendu 1", n)
	}
}

// Deux reprises simultanées du même RDV annulé, sur deux créneaux différents (donc sans
// aide de la contrainte EXCLUDE) : une seule aboutit.
func TestRebookConcurrentOnlyOne(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	e := newBookingEnv(t, thursday)
	token := e.cancelled(t)

	starts := []time.Time{rebookA, rebookB, rebookA.Add(90 * time.Minute), rebookA.Add(30 * time.Minute)}
	errs := make([]error, len(starts))
	gate := make(chan struct{})
	var wg sync.WaitGroup
	for i, s := range starts {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-gate
			_, errs[i] = e.svc.RebookByPatient(ctx, token, s, false)
		}()
	}
	close(gate)
	wg.Wait()
	ok := 0
	for _, err := range errs {
		switch {
		case err == nil:
			ok++
		case errors.Is(err, service.ErrAlreadyRebooked):
		default:
			t.Errorf("erreur inattendue : %v", err)
		}
	}
	if ok != 1 {
		t.Errorf("%d reprises abouties, attendu 1 (%v)", ok, errs)
	}
	if n := e.count(t, "SELECT count(*) FROM appointment WHERE status = 'booked'"); n != 1 {
		t.Errorf("%d RDV réservés, attendu 1", n)
	}
	// La garantie vient de la base : un second UPDATE conditionnel ne touche aucune ligne.
	tag, err := e.db.Exec(ctx, `UPDATE appointment SET rebooked_appointment_id = id
		WHERE status = 'cancelled' AND rebooked_appointment_id IS NULL`)
	if err != nil || tag.RowsAffected() != 0 {
		t.Errorf("UPDATE conditionnel après reprise : %v, %v", tag, err)
	}
}

func TestRebookSlotTaken(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	e := newBookingEnv(t, thursday)
	token := e.cancelled(t)
	if _, err := e.svc.Book(ctx, e.start(t, rebookA, patient("Bruno", "Martin", "06 22 22 22 22"), "+33622222222")); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.RebookByPatient(ctx, token, rebookA, false); !errors.Is(err, domain.ErrSlotTaken) {
		t.Fatalf("créneau pris : %v, want ErrSlotTaken", err)
	}
	// L'échec n'a rien consommé : le patient peut reprendre sur un autre créneau.
	if c, _ := e.svc.Confirmation(ctx, token); !c.Rebookable {
		t.Error("la reprise a été consommée par un échec")
	}
	if _, err := e.svc.RebookByPatient(ctx, token, rebookB, false); err != nil {
		t.Fatalf("autre créneau : %v", err)
	}
}

func TestRebookRequiresInstructionsAck(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	e := newBookingEnv(t, thursday)
	token := e.cancelled(t)
	mustExec(t, e.db, "UPDATE appointment_type SET patient_instructions = 'Venir en tenue souple.', instructions_ack_required = true WHERE id = $1", e.typ.ID)
	var ve domain.ErrValidation
	if _, err := e.svc.RebookByPatient(ctx, token, rebookA, false); !errors.As(err, &ve) || ve.Field != "instructions_ack" {
		t.Fatalf("sans case : %v, want ErrValidation(instructions_ack)", err)
	}
	if n := e.count(t, "SELECT count(*) FROM appointment WHERE status = 'booked'"); n != 0 {
		t.Errorf("%d RDV créé(s) sans confirmation des consignes", n)
	}
	if _, err := e.svc.RebookByPatient(ctx, token, rebookA, true); err != nil {
		t.Fatalf("avec case : %v", err)
	}
}
