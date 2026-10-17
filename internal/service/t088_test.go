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
)

// Le déplacement par le patient d'un RDV à domicile conserve le trajet dans during_blocked.
func TestRescheduleByPatientKeepsHomeTravel(t *testing.T) {
	t.Parallel()
	e := newHomeEnv(t, true, []string{"75011"})
	ctx := context.Background()
	p := patient("Alice", "Durand", "06 11 11 11 11")
	p.Address = "12 rue de la Paix, 75011 Paris"
	res, err := e.svc.Book(ctx, e.start(t, tuesday9, p, "+33611111111"))
	if err != nil {
		t.Fatalf("Book: %v", err)
	}
	target := tuesday9.Add(2 * time.Hour) // 11 h
	if err := e.svc.RescheduleByPatient(ctx, res.Token, target); err != nil {
		t.Fatalf("RescheduleByPatient: %v", err)
	}
	var lo, hi time.Time
	if err := e.db.QueryRow(ctx, "SELECT lower(during_blocked), upper(during_blocked) FROM appointment WHERE id = $1", res.Appointment.ID).Scan(&lo, &hi); err != nil {
		t.Fatal(err)
	}
	if !lo.Equal(target.Add(-15*time.Minute)) || !hi.Equal(target.Add(45*time.Minute)) {
		t.Errorf("during_blocked = %v–%v, attendu soin ± 15 min de trajet", lo, hi)
	}
	// 10 h 30 : son trajet (10 h 15–11 h 15) touche le soin déplacé (11 h).
	if err := e.svc.CheckSlot(ctx, e.pr.ID, e.typ.ID, tuesday9.Add(90*time.Minute)); !errors.Is(err, domain.ErrSlotTaken) {
		t.Errorf("RDV dans le trajet = %v, attendu ErrSlotTaken", err)
	}
}

type countSink struct{ kinds []string }

func (c *countSink) Emit(_ context.Context, ev service.Event) { c.kinds = append(c.kinds, ev.Kind) }

// Le garde-fou d'absences (T054) s'applique à Join et Claim, pas à AddByStaff ;
// Claim émet l'événement de création et appelle OnCreated.
func TestWaitlistNoShowGuard(t *testing.T) {
	t.Parallel()
	e := newBookingEnv(t, thursday)
	ctx := context.Background()
	wl := e.waitlist()
	sink := &countSink{}
	wl.Events = sink
	var created []string
	wl.OnCreated = func(s string) { created = append(created, s) }

	// Carl : patient inscrit par le cabinet, puis ayant manqué un RDV, sous un seuil de 1.
	_, token := e.bookAt(t, tuesday9, "Alice", "06 11 11 11 11", "+33611111111")
	if _, err := wl.AddByStaff(ctx, e.actor(t), service.StaffAddInput{Patient: emailPatient("Carl", "carl@example.test"), Window: e.window()}); err != nil {
		t.Fatal(err)
	}
	var carl uuid.UUID
	if err := e.db.QueryRow(ctx, "SELECT patient_id FROM waitlist_entry WHERE practice_id = $1", e.pc.ID).Scan(&carl); err != nil {
		t.Fatal(err)
	}
	// Un autre patient sans absence pour vérifier le cas passant.
	if err := e.svc.CancelByPatient(ctx, token); err != nil {
		t.Fatal(err)
	}
	offer := e.offerTokens(t)["carl@example.test"]
	if offer == "" {
		t.Fatal("pas d'offre pour Carl")
	}
	e.addNoShow(t, carl, thursday.AddDate(0, 0, -10))
	one := int16(1)
	e.setThreshold(t, &one)

	if _, err := wl.Claim(ctx, offer); !errors.Is(err, domain.ErrOnlineBookingBlocked) {
		t.Fatalf("Claim = %v, attendu ErrOnlineBookingBlocked", err)
	}
	if n := e.count(t, "SELECT count(*) FROM appointment WHERE patient_id = $1 AND status = 'booked'", carl); n != 0 {
		t.Errorf("RDV créé malgré le refus : %d", n)
	}
	if len(sink.kinds) != 0 || len(created) != 0 {
		t.Errorf("événement/métrique émis malgré le refus : %v %v", sink.kinds, created)
	}

	// Inscription en ligne : refusée pour Carl (même téléphone/email retrouvé), acceptée par le cabinet.
	in := service.WaitlistVerifyInput{
		Window:  service.WindowInput{PractitionerID: e.pr.ID, TypeID: e.typ.ID, Earliest: "2026-10-15", Latest: "2026-10-31"},
		Patient: emailPatient("Carl", "carl@example.test"), Channel: "email",
	}
	v, err := wl.StartVerification(ctx, in)
	if err != nil {
		t.Fatalf("StartVerification: %v", err)
	}
	if _, err := wl.Join(ctx, service.WaitlistJoinInput{WaitlistVerifyInput: in, ChallengeID: v.ChallengeID, Code: e.otp.code("carl@example.test")}); !errors.Is(err, domain.ErrOnlineBookingBlocked) {
		t.Fatalf("Join = %v, attendu ErrOnlineBookingBlocked", err)
	}
	if _, err := wl.AddByStaff(ctx, e.actor(t), service.StaffAddInput{Patient: emailPatient("Carl", "carl@example.test"), Window: e.window()}); err != nil {
		t.Errorf("AddByStaff ne doit jamais être bloqué : %v", err)
	}

	// Seuil levé : Claim réussit et émet l'événement + la métrique.
	e.setThreshold(t, nil)
	if _, err := wl.Claim(ctx, offer); err != nil {
		t.Fatalf("Claim après levée du seuil : %v", err)
	}
	if len(sink.kinds) != 1 || sink.kinds[0] != service.EventAppointmentCreated || len(created) != 1 || created[0] != "online" {
		t.Errorf("événements = %v, OnCreated = %v", sink.kinds, created)
	}
}

// Un patient inscrit deux fois (fenêtres compatibles) ne reçoit qu'une offre par créneau libéré.
func TestWaitlistOneOfferPerPatient(t *testing.T) {
	t.Parallel()
	e := newBookingEnv(t, thursday)
	ctx := context.Background()
	wl := e.waitlist()
	_, token := e.bookAt(t, tuesday9, "Alice", "06 11 11 11 11", "+33611111111")
	for _, p := range []service.PatientDetails{emailPatient("Bob", "bob@example.test"), emailPatient("Bob", "bob@example.test"), emailPatient("Carl", "carl@example.test")} {
		e.clock.Advance(time.Minute)
		if _, err := wl.AddByStaff(ctx, e.actor(t), service.StaffAddInput{Patient: p, Window: e.window()}); err != nil {
			t.Fatal(err)
		}
	}
	if n := e.count(t, "SELECT count(*) FROM waitlist_entry WHERE practice_id = $1", e.pc.ID); n != 3 {
		t.Fatalf("inscriptions = %d, attendu 3", n)
	}
	if err := e.svc.CancelByPatient(ctx, token); err != nil {
		t.Fatal(err)
	}
	if n := e.count(t, "SELECT count(*) FROM waitlist_offer WHERE practice_id = $1", e.pc.ID); n != 2 {
		t.Errorf("offres = %d, attendu 2 (une par patient)", n)
	}
	if n := e.waitlistMessages(t); n != 2 {
		t.Errorf("messages = %d, attendu 2", n)
	}
}

// Un RDV annulé entre la lecture et l'écriture n'est jamais ressuscité.
func TestSetAppointmentStatusNeverRevivesCancelled(t *testing.T) {
	t.Parallel()
	e := newBookingEnv(t, thursday)
	ctx := context.Background()
	res, _ := e.bookAt(t, tuesday9, "Alice", "06 11 11 11 11", "+33611111111")
	mustExec(t, e.db, "UPDATE appointment SET status = 'cancelled', cancelled_at = now() WHERE id = $1", res.Appointment.ID)
	_, err := sqlcgen.New(e.db).SetAppointmentStatus(ctx, sqlcgen.SetAppointmentStatusParams{
		ID: res.Appointment.ID, PracticeID: e.pc.ID, Status: "done",
	})
	if err == nil {
		t.Fatal("SetAppointmentStatus a écrasé un RDV annulé")
	}
	if n := e.count(t, "SELECT count(*) FROM appointment WHERE id = $1 AND status = 'cancelled'", res.Appointment.ID); n != 1 {
		t.Errorf("statut modifié")
	}
}
