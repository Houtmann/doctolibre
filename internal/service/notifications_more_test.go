package service_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"doctolibre/internal/domain"
	"doctolibre/internal/service"
)

func TestReminderSkippedWhenBookedAfterSendTime(t *testing.T) {
	t.Parallel()
	// Réservation lundi 19 h pour mardi 9 h : l'heure du rappel (18 h) est passée ;
	// le patient vient de recevoir sa confirmation, pas de rappel dans la foulée.
	e := newBookingEnv(t, time.Date(2026, 10, 19, 19, 0, 0, 0, paris))
	bookOnline(t, e, tuesday9)
	e.clock.Advance(10 * time.Minute)
	if n, err := e.reminders().RunOnce(context.Background()); err != nil || n != 0 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	if got := len(only(outMsgs(t, e.db, e.pc.ID), "reminder")); got != 0 {
		t.Errorf("%d rappels, attendu 0", got)
	}
}

func TestReminderRespectsSMSOptOutAtSendTime(t *testing.T) {
	t.Parallel()
	e := newBookingEnv(t, thursday)
	bookOnline(t, e, tuesday9)
	// Le patient se désinscrit des SMS après sa réservation, avant l'envoi du rappel.
	mustExec(t, e.db, "UPDATE patient SET sms_opt_in = false WHERE practice_id = $1", e.pc.ID)
	e.clock.Advance(time.Date(2026, 10, 19, 18, 5, 0, 0, paris).Sub(thursday))
	if n, err := e.reminders().RunOnce(context.Background()); err != nil || n != 0 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	if got := len(only(outMsgs(t, e.db, e.pc.ID), "reminder")); got != 0 {
		t.Errorf("%d rappels malgré l'opt-out", got)
	}
}

func TestReminderQuietHours(t *testing.T) {
	t.Parallel()
	e := newBookingEnv(t, thursday)
	bookOnline(t, e, tuesday9)
	mustExec(t, e.db, "UPDATE practice SET reminder_day_before_hour = 21 WHERE id = $1", e.pc.ID)
	e.clock.Advance(time.Date(2026, 10, 19, 21, 5, 0, 0, paris).Sub(thursday))
	if n, err := e.reminders().RunOnce(context.Background()); err != nil || n != 1 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	r := only(outMsgs(t, e.db, e.pc.ID), "reminder")[0]
	// Pas de SMS entre 20 h et 8 h : reporté à 8 h le lendemain (1 h avant le rendez-vous).
	if want := time.Date(2026, 10, 20, 8, 0, 0, 0, paris); !r.RunAt.Equal(want) {
		t.Errorf("run_at = %v, attendu %v", r.RunAt, want)
	}
}

func TestReminderTwoHoursBefore(t *testing.T) {
	t.Parallel()
	e := newBookingEnv(t, thursday)
	bookOnline(t, e, tuesday9)
	mustExec(t, e.db, "UPDATE practice SET reminder_day_before = false, reminder_two_hours = true WHERE id = $1", e.pc.ID)
	// H-2 = 7 h pour 9 h : le SMS est décalé à 8 h (reste plus de 30 min avant le RDV).
	e.clock.Advance(time.Date(2026, 10, 20, 7, 5, 0, 0, paris).Sub(thursday))
	if n, err := e.reminders().RunOnce(context.Background()); err != nil || n != 1 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	r := only(outMsgs(t, e.db, e.pc.ID), "reminder")[0]
	if !strings.HasPrefix(r.DedupKey, "reminder-2h:") {
		t.Errorf("dedup_key = %q", r.DedupKey)
	}
	if want := time.Date(2026, 10, 20, 8, 0, 0, 0, paris); !r.RunAt.Equal(want) {
		t.Errorf("run_at = %v, attendu %v", r.RunAt, want)
	}
}

func TestPatientCancelCancelsPendingReminders(t *testing.T) {
	t.Parallel()
	e := newBookingEnv(t, thursday)
	res := bookOnline(t, e, tuesday9)
	// Rappel à minuit (33 h avant : l'annulation reste possible, délai de 24 h).
	mustExec(t, e.db, "UPDATE practice SET reminder_day_before_hour = 0 WHERE id = $1", e.pc.ID)
	e.clock.Advance(time.Date(2026, 10, 19, 0, 5, 0, 0, paris).Sub(thursday))
	if n, err := e.reminders().RunOnce(context.Background()); err != nil || n != 1 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	if err := e.svc.CancelByPatient(context.Background(), res.Token); err != nil {
		t.Fatal(err)
	}
	for _, m := range outMsgs(t, e.db, e.pc.ID) {
		if m.Template == "cancellation" {
			t.Errorf("message d'annulation envoyé à un patient qui a annulé lui-même")
		}
		if m.Template == "reminder" && m.Status != "cancelled" {
			t.Errorf("rappel %s après annulation, attendu cancelled", m.Status)
		}
	}
	// Un tour ultérieur n'enfile rien pour un rendez-vous annulé.
	e.clock.Advance(18 * time.Hour)
	if n, err := e.reminders().RunOnce(context.Background()); err != nil || n != 0 {
		t.Errorf("rappel pour un RDV annulé : n=%d err=%v", n, err)
	}
}

func TestPatientRescheduleCancelsOldRemindersAndNotifies(t *testing.T) {
	t.Parallel()
	e := newBookingEnv(t, thursday)
	res := bookOnline(t, e, tuesday9)
	mustExec(t, e.db, "UPDATE practice SET reminder_day_before_hour = 0 WHERE id = $1", e.pc.ID)
	e.clock.Advance(time.Date(2026, 10, 19, 0, 5, 0, 0, paris).Sub(thursday))
	if _, err := e.reminders().RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	// Déplacement au dimanche 25 octobre 9 h (plage 9-10 h).
	sunday := time.Date(2026, 10, 25, 9, 0, 0, 0, paris)
	if err := e.svc.RescheduleByPatient(context.Background(), res.Token, sunday); err != nil {
		t.Fatal(err)
	}
	var resched, oldReminders int
	for _, m := range outMsgs(t, e.db, e.pc.ID) {
		switch {
		case m.Template == "reschedule":
			resched++
			if b := m.body(t); !strings.Contains(b, "25/10") || !strings.Contains(b, baseURL+"/r/"+res.Token) {
				t.Errorf("message de déplacement : %q", b)
			}
		case m.Template == "reminder" && m.Status == "cancelled":
			oldReminders++
		case m.Template == "reminder":
			t.Errorf("ancien rappel toujours %s", m.Status)
		}
	}
	if resched != 1 || oldReminders != 1 {
		t.Errorf("déplacement=%d anciens rappels annulés=%d, attendu 1 et 1", resched, oldReminders)
	}
}

func TestReminderAcrossDST(t *testing.T) {
	t.Parallel()
	// Dimanche 25 octobre 2026 : fin de l'heure d'été. Le rendez-vous de dimanche 9 h
	// est rappelé samedi 18 h (heure d'été, UTC+2), pas 24 h avant.
	e := newBookingEnv(t, thursday)
	sunday := time.Date(2026, 10, 25, 9, 0, 0, 0, paris)
	bookOnline(t, e, sunday)
	e.clock.Advance(time.Date(2026, 10, 24, 17, 55, 0, 0, paris).Sub(thursday))
	if n, err := e.reminders().RunOnce(context.Background()); err != nil || n != 0 {
		t.Fatalf("trop tôt : n=%d err=%v", n, err)
	}
	e.clock.Advance(10 * time.Minute)
	if n, err := e.reminders().RunOnce(context.Background()); err != nil || n != 1 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	r := only(outMsgs(t, e.db, e.pc.ID), "reminder")[0]
	if want := time.Date(2026, 10, 24, 16, 0, 0, 0, time.UTC); !r.RunAt.Equal(want) {
		t.Errorf("run_at = %v, attendu %v (18 h CEST)", r.RunAt, want)
	}
}

func TestConfirmationDisabled(t *testing.T) {
	t.Parallel()
	e := newBookingEnv(t, thursday)
	mustExec(t, e.db, "UPDATE practice SET notify_confirmation_channel = 'none' WHERE id = $1", e.pc.ID)
	bookOnline(t, e, tuesday9)
	if n := len(outMsgs(t, e.db, e.pc.ID)); n != 0 {
		t.Errorf("%d messages avec la confirmation désactivée", n)
	}
}

// --- Rendez-vous saisis par le cabinet ---

func TestStaffAppointmentNotifications(t *testing.T) {
	t.Parallel()
	f := newCalFixture(t)
	f.cal.BaseURL = baseURL
	ctx := context.Background()
	in := f.quick("2026-10-19", "09:00", false)
	in.NewEmail = "lea@example.org"
	appt, err := f.cal.Create(ctx, f.act, in)
	if err != nil {
		t.Fatal(err)
	}
	ms := outMsgs(t, f.db, f.act.PracticeID)
	if len(ms) != 1 || ms[0].Kind != "email" || ms[0].Template != "confirmation" || ms[0].Recipient != "lea@example.org" {
		t.Fatalf("confirmation staff : %+v", ms)
	}
	if !strings.Contains(ms[0].Payload, baseURL+"/r/") {
		t.Errorf("pas de lien de gestion dans la confirmation : %s", ms[0].Payload)
	}

	// Déplacement par le cabinet : message de déplacement, confirmation en attente annulée.
	if err := f.cal.Move(ctx, f.act, appt.ID, service.MoveInput{Date: "2026-10-20", Time: "09:00"}); err != nil {
		t.Fatal(err)
	}
	if got := only(outMsgs(t, f.db, f.act.PracticeID), "reschedule"); len(got) != 1 || got[0].Kind != "email" {
		t.Fatalf("déplacement : %+v", got)
	}

	// Annulation par le cabinet : message d'annulation, messages en attente annulés.
	if err := f.cal.Cancel(ctx, f.act, appt.ID, "motif interne confidentiel"); err != nil {
		t.Fatal(err)
	}
	all := outMsgs(t, f.db, f.act.PracticeID)
	got := only(all, "cancellation")
	if len(got) != 1 || got[0].Kind != "email" || got[0].Status != "pending" {
		t.Fatalf("annulation : %+v", got)
	}
	for _, m := range all {
		if m.Template != "cancellation" && m.Status == "pending" {
			t.Errorf("message %s encore pending après annulation", m.Template)
		}
	}
	if strings.Contains(got[0].Payload, "confidentiel") {
		t.Error("le motif interne d'annulation ne doit pas figurer dans le message")
	}
}

func TestStaffAppointmentFallsBackToSMSWithoutEmail(t *testing.T) {
	t.Parallel()
	f := newCalFixture(t)
	if _, err := f.cal.Create(context.Background(), f.act, f.quick("2026-10-19", "09:00", false)); err != nil {
		t.Fatal(err)
	}
	ms := outMsgs(t, f.db, f.act.PracticeID)
	if len(ms) != 1 || ms[0].Kind != "sms" || ms[0].Recipient != "+33612345678" {
		t.Fatalf("messages = %+v, attendu 1 SMS (pas d'email)", ms)
	}
}

func TestSetStatusNoShowCancelsPending(t *testing.T) {
	t.Parallel()
	f := newCalFixture(t)
	ctx := context.Background()
	appt, err := f.cal.Create(ctx, f.act, f.quick("2026-10-19", "09:00", false))
	if err != nil {
		t.Fatal(err)
	}
	if err := f.cal.SetStatus(ctx, f.act, appt.ID, "no_show"); err != nil {
		t.Fatal(err)
	}
	for _, m := range outMsgs(t, f.db, f.act.PracticeID) {
		if m.Status == "pending" {
			t.Errorf("message %s encore pending pour un RDV absent", m.Template)
		}
	}
}

// --- Réglages ---

func TestNotificationSettings(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	ctx := context.Background()
	in, err := f.svc.Notifications(ctx, f.act)
	if err != nil {
		t.Fatal(err)
	}
	// Valeurs par défaut (docs/sms.md) : confirmation email, rappel J-1 SMS, H-2 désactivé.
	if in.ConfirmationChannel != "email" || in.ReminderChannel != "sms" || !in.ReminderDayBefore || in.ReminderHour != "18" || in.ReminderTwoHours {
		t.Errorf("défauts = %+v", in)
	}
	bad := in
	bad.ReminderChannel, bad.ReminderHour = "pigeon", "24"
	err = f.svc.UpdateNotifications(ctx, f.act, bad)
	var verr domain.ValidationErrors
	if !errors.As(err, &verr) || len(verr) != 2 {
		t.Fatalf("erreurs = %v, attendu 2 erreurs de validation", err)
	}
	in.ReminderTwoHours, in.Signature = true, "Cabinet Dupont"
	if err := f.svc.UpdateNotifications(ctx, f.act, in); err != nil {
		t.Fatal(err)
	}
	got, err := f.svc.Notifications(ctx, f.act)
	if err != nil || !got.ReminderTwoHours || got.Signature != "Cabinet Dupont" {
		t.Errorf("relecture = %+v, %v", got, err)
	}
}

func TestEstimateSMS(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	ctx := context.Background()
	pr, err := f.svc.Practitioner(ctx, f.act)
	if err != nil {
		t.Fatal(err)
	}
	now := f.svc.Clock.Now()
	// 8 RDV sur 4 semaines : 4 patients joignables par SMS seul, 4 par SMS et email.
	for i := range 8 {
		var patID uuid.UUID
		email := any(nil)
		if i%2 == 1 {
			email = "p" + itoa(int64(i)) + "@example.org"
		}
		if err := f.db.QueryRow(ctx,
			"INSERT INTO patient (practice_id, first_name, last_name, phone, email) VALUES ($1, 'P', 'Q', '+33600000000', $2) RETURNING id",
			f.act.PracticeID, email).Scan(&patID); err != nil {
			t.Fatal(err)
		}
		start := now.Add(-time.Duration(i+1) * 24 * time.Hour)
		mustExec(t, f.db, `INSERT INTO appointment (practice_id, practitioner_id, patient_id, appointment_type_id, during, during_blocked, status, source)
			SELECT $1, $2, $3, id, tstzrange($4::timestamptz, $4::timestamptz + interval '30 minutes', '[)'),
			       tstzrange($4::timestamptz, $4::timestamptz + interval '30 minutes', '[)'), 'done', 'staff'
			FROM appointment_type WHERE practice_id = $1 LIMIT 1`, f.act.PracticeID, pr.ID, patID, start)
	}
	in, err := f.svc.Notifications(ctx, f.act)
	if err != nil {
		t.Fatal(err)
	}
	in.ReminderTwoHours = true
	est, err := f.svc.EstimateSMS(ctx, f.act, in)
	if err != nil {
		t.Fatal(err)
	}
	// 8 RDV / 4 semaines = ~8,7 par mois. Confirmation par email : SMS seulement pour les 4
	// sans email (~4,3). Rappels J-1 et H-2 par SMS : 2 x 8 (~17,3).
	if est.Appointments != 9 || est.Confirmation != 4 || est.Reminders != 17 || est.PerMonth != 21 {
		t.Errorf("estimation = %+v", est)
	}
	in.ReminderChannel = "none"
	if est, _ := f.svc.EstimateSMS(ctx, f.act, in); est.Reminders != 0 {
		t.Errorf("rappels désactivés : %+v", est)
	}
}
