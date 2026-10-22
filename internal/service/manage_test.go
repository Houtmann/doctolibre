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

type recordSink struct {
	mu    sync.Mutex
	kinds []string
}

func (r *recordSink) Emit(_ context.Context, e service.Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.kinds = append(r.kinds, e.Kind)
}

func (r *recordSink) got() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.kinds...)
}

// bookAt réserve un rendez-vous et retourne son jeton.
func (e *bookingEnv) bookAt(t *testing.T, start time.Time, first, phone, e164 string) (service.BookResult, string) {
	t.Helper()
	res, err := e.svc.Book(context.Background(), e.start(t, start, patient(first, "Test", phone), e164))
	if err != nil {
		t.Fatalf("Book: %v", err)
	}
	return res, res.Token
}

func (e *bookingEnv) status(t *testing.T, token string) (status, cancelledBy string, start time.Time) {
	t.Helper()
	c, err := e.svc.Confirmation(context.Background(), token)
	if err != nil {
		t.Fatal(err)
	}
	return c.Appointment.Status, c.Appointment.CancelledBy.String, c.Start
}

func TestCancelByPatientWithinNotice(t *testing.T) {
	t.Parallel()
	e := newBookingEnv(t, thursday)
	sink := &recordSink{}
	e.svc.Events = sink
	_, token := e.bookAt(t, tuesday9, "Alice", "06 11 11 11 11", "+33611111111")

	c, err := e.svc.Confirmation(context.Background(), token)
	if err != nil || !c.Changeable {
		t.Fatalf("Changeable = %v, err = %v", c.Changeable, err)
	}
	if err := e.svc.CancelByPatient(context.Background(), token); err != nil {
		t.Fatalf("CancelByPatient: %v", err)
	}
	st, by, _ := e.status(t, token)
	if st != "cancelled" || by != "patient" {
		t.Errorf("status = %q, cancelled_by = %q", st, by)
	}
	if err := e.svc.CancelByPatient(context.Background(), token); !errors.Is(err, service.ErrChangeTooLate) {
		t.Errorf("seconde annulation = %v, want ErrChangeTooLate", err)
	}
	// Le créneau est de nouveau réservable.
	if err := e.svc.CheckSlot(context.Background(), e.pr.ID, e.typ.ID, tuesday9); err != nil {
		t.Errorf("créneau libéré: %v", err)
	}
	want := []string{service.EventAppointmentCreated, service.EventAppointmentCancelled}
	if got := sink.got(); len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("événements = %v, want %v", got, want)
	}
}

func TestCancelAndRescheduleRefusedOutsideNotice(t *testing.T) {
	t.Parallel()
	e := newBookingEnv(t, thursday)
	_, token := e.bookAt(t, tuesday9, "Alice", "06 11 11 11 11", "+33611111111")

	// Délai par défaut : 24 h. À 23 h du rendez-vous, c'est refusé.
	e.clock.Advance(tuesday9.Sub(thursday) - 23*time.Hour)
	c, err := e.svc.Confirmation(context.Background(), token)
	if err != nil || c.Changeable {
		t.Fatalf("Changeable = %v, err = %v", c.Changeable, err)
	}
	if err := e.svc.CancelByPatient(context.Background(), token); !errors.Is(err, service.ErrChangeTooLate) {
		t.Errorf("CancelByPatient = %v, want ErrChangeTooLate", err)
	}
	if err := e.svc.RescheduleByPatient(context.Background(), token, tuesday9.Add(time.Hour)); !errors.Is(err, service.ErrChangeTooLate) {
		t.Errorf("RescheduleByPatient = %v, want ErrChangeTooLate", err)
	}
	if st, _, start := e.status(t, token); st != "booked" || !start.Equal(tuesday9) {
		t.Errorf("rendez-vous modifié malgré le refus: %s %v", st, start)
	}

	// Pile à 24 h : accepté.
	e.clock.Advance(-time.Hour)
	if err := e.svc.CancelByPatient(context.Background(), token); err != nil {
		t.Errorf("annulation à 24 h pile: %v", err)
	}
}

func TestCancelUnknownToken(t *testing.T) {
	t.Parallel()
	e := newBookingEnv(t, thursday)
	for _, tok := range []string{"", "inconnu", string(make([]byte, 200))} {
		if err := e.svc.CancelByPatient(context.Background(), tok); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("CancelByPatient(%.10q) = %v, want ErrNotFound", tok, err)
		}
		if err := e.svc.RescheduleByPatient(context.Background(), tok, tuesday9); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("RescheduleByPatient(%.10q) = %v, want ErrNotFound", tok, err)
		}
	}
}

func TestRescheduleByPatient(t *testing.T) {
	t.Parallel()
	e := newBookingEnv(t, thursday)
	sink := &recordSink{}
	e.svc.Events = sink
	_, token := e.bookAt(t, tuesday9, "Alice", "06 11 11 11 11", "+33611111111")

	// Le rendez-vous ne bloque pas ses propres créneaux : 9 h 15 (qui le chevauche) est proposé.
	slots, err := e.svc.RescheduleSlots(context.Background(), token, tuesday9, tuesday9.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	found, overlap := false, false
	for _, s := range slots {
		found = found || s.Start.Equal(tuesday9)
		overlap = overlap || s.Start.Equal(tuesday9.Add(15*time.Minute))
	}
	if found {
		t.Error("l'horaire actuel ne doit pas être proposé au déplacement")
	}
	if !overlap {
		t.Error("9 h 15 (qui chevauche le rendez-vous) devrait être proposé")
	}

	target := tuesday9.Add(90 * time.Minute) // 10 h 30
	if err := e.svc.RescheduleByPatient(context.Background(), token, target); err != nil {
		t.Fatalf("RescheduleByPatient: %v", err)
	}
	st, _, start := e.status(t, token)
	if st != "booked" || !start.Equal(target) {
		t.Errorf("après déplacement: %s %v, want booked %v", st, start, target)
	}
	if n := e.count(t, "SELECT count(*) FROM appointment"); n != 1 {
		t.Errorf("%d rendez-vous, want 1 (déplacement en place)", n)
	}
	// L'ancien créneau est libre.
	if err := e.svc.CheckSlot(context.Background(), e.pr.ID, e.typ.ID, tuesday9); err != nil {
		t.Errorf("ancien créneau non libéré: %v", err)
	}
	if got := sink.got(); len(got) != 2 || got[1] != service.EventAppointmentMoved {
		t.Errorf("événements = %v", got)
	}
}

func TestRescheduleAtomicWhenSlotTaken(t *testing.T) {
	t.Parallel()
	e := newBookingEnv(t, thursday)
	_, tokenA := e.bookAt(t, tuesday9, "Alice", "06 11 11 11 11", "+33611111111")
	// Bruno prend 10 h pendant qu'Alice hésite.
	taken := tuesday9.Add(time.Hour)
	e.bookAt(t, taken, "Bruno", "06 22 22 22 22", "+33622222222")

	if err := e.svc.RescheduleByPatient(context.Background(), tokenA, taken); !errors.Is(err, domain.ErrSlotTaken) {
		t.Fatalf("RescheduleByPatient = %v, want ErrSlotTaken", err)
	}
	// Créneau forgé (hors grille) : refusé aussi.
	if err := e.svc.RescheduleByPatient(context.Background(), tokenA, tuesday9.Add(7*time.Minute)); !errors.Is(err, domain.ErrSlotTaken) {
		t.Fatalf("créneau forgé = %v, want ErrSlotTaken", err)
	}
	st, _, start := e.status(t, tokenA)
	if st != "booked" || !start.Equal(tuesday9) {
		t.Errorf("l'ancien rendez-vous doit être conservé: %s %v", st, start)
	}
	if n := e.count(t, "SELECT count(*) FROM appointment WHERE status = 'booked'"); n != 2 {
		t.Errorf("%d rendez-vous actifs, want 2", n)
	}
}

func TestRescheduleToSameSlotIsNoop(t *testing.T) {
	t.Parallel()
	e := newBookingEnv(t, thursday)
	sink := &recordSink{}
	e.svc.Events = sink
	_, token := e.bookAt(t, tuesday9, "Alice", "06 11 11 11 11", "+33611111111")
	const q = "SELECT count(*) FROM outbox_message WHERE practice_id = $1"
	const qs = "SELECT count(*) FROM outbox_message WHERE practice_id = $1 AND status = 'cancelled'"
	before, beforeCancelled := e.count(t, q, e.pc.ID), e.count(t, qs, e.pc.ID)

	if err := e.svc.RescheduleByPatient(context.Background(), token, tuesday9); err != nil {
		t.Fatalf("RescheduleByPatient (même créneau): %v", err)
	}
	if n := e.count(t, q, e.pc.ID); n != before {
		t.Errorf("messages = %d, want %d (aucun message enfilé)", n, before)
	}
	if n := e.count(t, qs, e.pc.ID); n != beforeCancelled {
		t.Errorf("messages annulés = %d, want %d (rappels conservés)", n, beforeCancelled)
	}
	if got := sink.got(); len(got) != 1 {
		t.Errorf("événements = %v, want la seule réservation", got)
	}
	if _, _, start := e.status(t, token); !start.Equal(tuesday9) {
		t.Errorf("début = %v, want %v", start, tuesday9)
	}
}
