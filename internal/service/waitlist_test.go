package service_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"doctolibre/internal/domain"
	"doctolibre/internal/service"
)

const waitlistBase = "https://rdv.test"

func (e *bookingEnv) waitlist() *service.Waitlist {
	e.svc.BaseURL = waitlistBase
	return &service.Waitlist{Pool: e.db, Clock: e.clock, OTP: e.otp, BaseURL: waitlistBase}
}

func (e *bookingEnv) actor(t *testing.T) service.Actor {
	t.Helper()
	var id uuid.UUID
	err := e.db.QueryRow(context.Background(),
		"INSERT INTO app_user (practice_id, email, password_hash, role) VALUES ($1, $2, 'x', 'admin') RETURNING id",
		e.pc.ID, uuid.NewString()+"@example.test").Scan(&id)
	if err != nil {
		t.Fatal(err)
	}
	return service.Actor{UserID: id, PracticeID: e.pc.ID}
}

func (e *bookingEnv) window() service.WindowInput {
	return service.WindowInput{TypeID: e.typ.ID, Earliest: "2026-10-15", Latest: "2026-10-31"}
}

func emailPatient(first, email string) service.PatientDetails {
	return service.PatientDetails{FirstName: first, LastName: "Attente", Email: email, SMSOptIn: true, EmailOptIn: true}
}

// offerTokens retourne le jeton du lien /w/ envoyé à chaque destinataire.
func (e *bookingEnv) offerTokens(t *testing.T) map[string]string {
	t.Helper()
	rows, err := e.db.Query(context.Background(),
		"SELECT recipient, payload->>'offer_url' FROM outbox_message WHERE practice_id = $1 AND template = 'waitlist' AND kind = 'email'", e.pc.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var rcpt, link string
		if err := rows.Scan(&rcpt, &link); err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(link, waitlistBase+"/w/") {
			t.Fatalf("lien de proposition = %q", link)
		}
		out[rcpt] = strings.TrimPrefix(link, waitlistBase+"/w/")
	}
	return out
}

func (e *bookingEnv) waitlistMessages(t *testing.T) int {
	t.Helper()
	return e.count(t, "SELECT count(*) FROM outbox_message WHERE practice_id = $1 AND template = 'waitlist'", e.pc.ID)
}

func TestWaitlistTwoNotifiedOnlyOneGetsTheSlot(t *testing.T) {
	t.Parallel()
	e := newBookingEnv(t, thursday)
	wl := e.waitlist()
	ctx := context.Background()
	_, token := e.bookAt(t, tuesday9, "Alice", "06 11 11 11 11", "+33611111111")

	for _, p := range []service.PatientDetails{emailPatient("Bob", "bob@example.test"), emailPatient("Carl", "carl@example.test")} {
		if _, err := wl.AddByStaff(ctx, e.actor(t), service.StaffAddInput{Patient: p, Window: e.window()}); err != nil {
			t.Fatalf("AddByStaff: %v", err)
		}
	}
	if err := e.svc.CancelByPatient(ctx, token); err != nil {
		t.Fatalf("CancelByPatient: %v", err)
	}
	tokens := e.offerTokens(t)
	if len(tokens) != 2 || tokens["bob@example.test"] == "" || tokens["carl@example.test"] == "" {
		t.Fatalf("propositions = %v, want une par patient en attente", tokens)
	}
	// Le jeton n'est jamais stocké en clair.
	for _, tk := range tokens {
		if n := e.count(t, "SELECT count(*) FROM waitlist_offer WHERE token_hash = $1", tk); n != 0 {
			t.Error("jeton stocké en clair")
		}
	}

	var wg sync.WaitGroup
	gate := make(chan struct{})
	errs := map[string]error{}
	var mu sync.Mutex
	for who, tk := range tokens {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-gate
			_, err := wl.Claim(ctx, tk)
			mu.Lock()
			errs[who] = err
			mu.Unlock()
		}()
	}
	close(gate)
	wg.Wait()

	var won, lost int
	for _, err := range errs {
		switch {
		case err == nil:
			won++
		case errors.Is(err, domain.ErrSlotTaken):
			lost++
		default:
			t.Errorf("Claim: erreur inattendue %v", err)
		}
	}
	if won != 1 || lost != 1 {
		t.Fatalf("gagnants = %d, perdants = %d ; errs = %v", won, lost, errs)
	}
	if n := e.count(t, "SELECT count(*) FROM appointment WHERE practitioner_id = $1 AND status = 'booked' AND lower(during) = $2", e.pr.ID, tuesday9); n != 1 {
		t.Errorf("rendez-vous sur le créneau = %d, want 1", n)
	}
	if n := e.count(t, "SELECT count(*) FROM waitlist_entry WHERE practice_id = $1 AND status = 'fulfilled'", e.pc.ID); n != 1 {
		t.Errorf("inscriptions satisfaites = %d, want 1", n)
	}
	// Le perdant voit « déjà pris », le gagnant a sa confirmation (et un lien de gestion).
	for who, err := range errs {
		v, verr := wl.Offer(ctx, tokens[who])
		if verr != nil {
			t.Fatal(verr)
		}
		if err == nil && v.Reason != "claimed" {
			t.Errorf("gagnant : Reason = %q, want claimed", v.Reason)
		}
		if err != nil && (v.Available || v.Reason != "taken") {
			t.Errorf("perdant : Available = %v, Reason = %q, want taken", v.Available, v.Reason)
		}
	}
	// Une proposition ne sert qu'une fois.
	for who, err := range errs {
		if err == nil {
			if _, again := wl.Claim(ctx, tokens[who]); !errors.Is(again, service.ErrOfferExpired) {
				t.Errorf("second Claim = %v, want ErrOfferExpired", again)
			}
		}
	}
}

func TestWaitlistFIFOAndMaxFiveNotified(t *testing.T) {
	t.Parallel()
	e := newBookingEnv(t, thursday)
	wl := e.waitlist()
	ctx := context.Background()
	_, token := e.bookAt(t, tuesday9, "Alice", "06 11 11 11 11", "+33611111111")
	names := []string{"p1", "p2", "p3", "p4", "p5", "p6", "p7"}
	for _, n := range names {
		e.clock.Advance(time.Minute) // l'ordre d'inscription (FIFO) repose sur created_at
		if _, err := wl.AddByStaff(ctx, e.actor(t), service.StaffAddInput{Patient: emailPatient(n, n+"@example.test"), Window: e.window()}); err != nil {
			t.Fatal(err)
		}
	}
	if err := e.svc.CancelByPatient(ctx, token); err != nil {
		t.Fatal(err)
	}
	tokens := e.offerTokens(t)
	if len(tokens) != 5 {
		t.Fatalf("patients prévenus = %d, want 5 (%v)", len(tokens), tokens)
	}
	for _, n := range names[:5] {
		if tokens[n+"@example.test"] == "" {
			t.Errorf("%s (inscrit dans les 5 premiers) non prévenu", n)
		}
	}
}

func TestWaitlistNoNoticeWhenFreedTooLate(t *testing.T) {
	t.Parallel()
	e := newBookingEnv(t, thursday)
	wl := e.waitlist()
	ctx := context.Background()
	_, token := e.bookAt(t, tuesday9, "Alice", "06 11 11 11 11", "+33611111111")
	if _, err := wl.AddByStaff(ctx, e.actor(t), service.StaffAddInput{Patient: emailPatient("Bob", "bob@example.test"), Window: e.window()}); err != nil {
		t.Fatal(err)
	}
	// Le délai minimal de réservation dépasse le temps restant avant le créneau (~5 jours).
	if _, err := e.db.Exec(ctx, "UPDATE practice SET min_booking_notice = interval '10 days' WHERE id = $1", e.pc.ID); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.CancelByPatient(ctx, token); err != nil {
		t.Fatal(err)
	}
	if n := e.waitlistMessages(t); n != 0 {
		t.Errorf("messages = %d, want 0 (créneau libéré moins de min_booking_notice avant)", n)
	}
	if n := e.count(t, "SELECT count(*) FROM waitlist_offer WHERE practice_id = $1", e.pc.ID); n != 0 {
		t.Errorf("propositions = %d, want 0", n)
	}
}

func TestWaitlistIncompatibleEntriesAreSkipped(t *testing.T) {
	t.Parallel()
	e := newBookingEnv(t, thursday) // créneau libéré : mardi 20 octobre à 9 h
	wl := e.waitlist()
	ctx := context.Background()
	_, token := e.bookAt(t, tuesday9, "Alice", "06 11 11 11 11", "+33611111111")

	add := func(first string, mod func(*service.WindowInput)) {
		w := e.window()
		mod(&w)
		if _, err := wl.AddByStaff(ctx, e.actor(t), service.StaffAddInput{Patient: emailPatient(first, strings.ToLower(first)+"@example.test"), Window: w}); err != nil {
			t.Fatalf("AddByStaff %s: %v", first, err)
		}
	}
	add("Jeudi", func(w *service.WindowInput) { w.Weekdays = []string{"3"} })                                  // jeudi seulement
	add("Apres", func(w *service.WindowInput) { w.From, w.To = "14:00", "18:00" })                             // l'après-midi
	add("Avant", func(w *service.WindowInput) { w.Latest = "2026-10-19" })                                     // trop tôt
	add("Autre", func(w *service.WindowInput) { w.PractitionerID = e.otherPractitioner(t) })                   // autre praticien
	add("Mardi", func(w *service.WindowInput) { w.Weekdays = []string{"1"}; w.From, w.To = "08:00", "10:00" }) // compatible
	add("Tous", func(*service.WindowInput) {})                                                                 // compatible

	if err := e.svc.CancelByPatient(ctx, token); err != nil {
		t.Fatal(err)
	}
	tokens := e.offerTokens(t)
	if len(tokens) != 2 || tokens["mardi@example.test"] == "" || tokens["tous@example.test"] == "" {
		t.Fatalf("propositions = %v, want mardi et tous", tokens)
	}
}

func (e *bookingEnv) otherPractitioner(t *testing.T) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	err := e.db.QueryRow(context.Background(), `
		INSERT INTO practitioner (practice_id, first_name, last_name, slug) VALUES ($1, 'Autre', 'Praticien', 'autre') RETURNING id`, e.pc.ID).Scan(&id)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestWaitlistRespectsOptOutAndChannels(t *testing.T) {
	t.Parallel()
	e := newBookingEnv(t, thursday)
	wl := e.waitlist()
	ctx := context.Background()
	_, token := e.bookAt(t, tuesday9, "Alice", "06 11 11 11 11", "+33611111111")
	optOut := service.PatientDetails{FirstName: "Opt", LastName: "Out", Email: "opt@example.test", Phone: "06 33 33 33 33"}
	if _, err := wl.AddByStaff(ctx, e.actor(t), service.StaffAddInput{Patient: optOut, Window: e.window()}); err != nil {
		t.Fatal(err)
	}
	// Cabinet : changements par SMS ; un patient avec email seul se replie sur l'email.
	if _, err := e.db.Exec(ctx, "UPDATE practice SET notify_change_channel = 'sms' WHERE id = $1", e.pc.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := wl.AddByStaff(ctx, e.actor(t), service.StaffAddInput{Patient: emailPatient("Mail", "mail@example.test"), Window: e.window()}); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.CancelByPatient(ctx, token); err != nil {
		t.Fatal(err)
	}
	tokens := e.offerTokens(t)
	if len(tokens) != 1 || tokens["mail@example.test"] == "" {
		t.Fatalf("propositions = %v, want seulement mail@ (l'autre a refusé SMS et emails)", tokens)
	}
	if n := e.count(t, "SELECT count(*) FROM outbox_message WHERE practice_id = $1 AND template = 'waitlist' AND kind = 'sms'", e.pc.ID); n != 0 {
		t.Errorf("SMS = %d, want 0", n)
	}
}

func TestWaitlistOfferExpires(t *testing.T) {
	t.Parallel()
	e := newBookingEnv(t, thursday)
	wl := e.waitlist()
	ctx := context.Background()
	_, token := e.bookAt(t, tuesday9, "Alice", "06 11 11 11 11", "+33611111111")
	if _, err := wl.AddByStaff(ctx, e.actor(t), service.StaffAddInput{Patient: emailPatient("Bob", "bob@example.test"), Window: e.window()}); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.CancelByPatient(ctx, token); err != nil {
		t.Fatal(err)
	}
	tk := e.offerTokens(t)["bob@example.test"]
	v, err := wl.Offer(ctx, tk)
	if err != nil || !v.Available {
		t.Fatalf("Offer = %+v, %v ; want disponible", v, err)
	}
	e.clock.Advance(service.WaitlistOfferTTL + time.Minute)
	v, err = wl.Offer(ctx, tk)
	if err != nil || v.Available || v.Reason != "expired" {
		t.Errorf("Offer après 2 h = %+v, %v ; want expired", v, err)
	}
	if _, err := wl.Claim(ctx, tk); !errors.Is(err, service.ErrOfferExpired) {
		t.Errorf("Claim après 2 h = %v, want ErrOfferExpired", err)
	}
	if _, err := wl.Offer(ctx, "inconnu"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("jeton inconnu = %v, want ErrNotFound", err)
	}
}

func TestWaitlistClaimCreatesManageableAppointment(t *testing.T) {
	t.Parallel()
	e := newBookingEnv(t, thursday)
	wl := e.waitlist()
	ctx := context.Background()
	_, token := e.bookAt(t, tuesday9, "Alice", "06 11 11 11 11", "+33611111111")
	if _, err := wl.AddByStaff(ctx, e.actor(t), service.StaffAddInput{Patient: emailPatient("Bob", "bob@example.test"), Window: e.window()}); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.CancelByPatient(ctx, token); err != nil {
		t.Fatal(err)
	}
	res, err := wl.Claim(ctx, e.offerTokens(t)["bob@example.test"])
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	c, err := e.svc.Confirmation(ctx, res.Token)
	if err != nil {
		t.Fatalf("Confirmation: %v", err)
	}
	if !c.Start.Equal(tuesday9) || c.FirstName != "Bob" || c.Appointment.Status != "booked" {
		t.Errorf("rendez-vous = %v %q %q", c.Start, c.FirstName, c.Appointment.Status)
	}
	// Confirmation enfilée dans la transaction, avec le lien de gestion.
	if n := e.count(t, "SELECT count(*) FROM outbox_message WHERE appointment_id = $1 AND template = 'confirmation'", c.Appointment.ID); n != 1 {
		t.Errorf("confirmations = %d, want 1", n)
	}
}

func TestWaitlistOfferedOnPatientReschedule(t *testing.T) {
	t.Parallel()
	e := newBookingEnv(t, thursday)
	wl := e.waitlist()
	ctx := context.Background()
	_, token := e.bookAt(t, tuesday9, "Alice", "06 11 11 11 11", "+33611111111")
	if _, err := wl.AddByStaff(ctx, e.actor(t), service.StaffAddInput{Patient: emailPatient("Bob", "bob@example.test"), Window: e.window()}); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.RescheduleByPatient(ctx, token, time.Date(2026, 10, 20, 10, 0, 0, 0, paris)); err != nil {
		t.Fatalf("RescheduleByPatient: %v", err)
	}
	if tokens := e.offerTokens(t); len(tokens) != 1 {
		t.Fatalf("propositions = %v, want 1 (ancien créneau libéré)", tokens)
	}
}

func TestWaitlistOnlineJoinWithOTP(t *testing.T) {
	t.Parallel()
	e := newBookingEnv(t, thursday)
	wl := e.waitlist()
	ctx := context.Background()
	in := service.WaitlistVerifyInput{
		Window:  service.WindowInput{PractitionerID: e.pr.ID, TypeID: e.typ.ID, Earliest: "2026-10-15", Latest: "2026-10-31"},
		Patient: patient("Dora", "Web", "06 44 44 44 44"), Channel: "sms",
	}
	v, err := wl.StartVerification(ctx, in)
	if err != nil {
		t.Fatalf("StartVerification: %v", err)
	}
	join := service.WaitlistJoinInput{WaitlistVerifyInput: in, ChallengeID: v.ChallengeID, Code: "000000"}
	if e.otp.code("+33644444444") == "000000" {
		join.Code = "111111"
	}
	if _, err := wl.Join(ctx, join); !errors.Is(err, service.ErrOTPWrong) {
		t.Fatalf("Join code erroné = %v, want ErrOTPWrong", err)
	}
	if n := e.count(t, "SELECT count(*) FROM waitlist_entry"); n != 0 {
		t.Fatalf("inscriptions sans code valide = %d", n)
	}
	// Modifier la fenêtre après l'envoi du code invalide la vérification.
	tampered := join
	tampered.Code = e.otp.code("+33644444444")
	tampered.Window.Latest = "2026-12-31"
	if _, err := wl.Join(ctx, tampered); !errors.Is(err, service.ErrOTPExpired) {
		t.Fatalf("Join fenêtre modifiée = %v, want ErrOTPExpired", err)
	}
	join.Code = e.otp.code("+33644444444")
	entry, err := wl.Join(ctx, join)
	if err != nil {
		t.Fatalf("Join: %v", err)
	}
	if entry.Status != "active" || entry.Source != "online" || entry.VerifiedChannel.String != "sms" {
		t.Errorf("entry = %+v", entry)
	}
	if _, err := wl.Join(ctx, join); !errors.Is(err, service.ErrOTPExpired) {
		t.Errorf("code rejoué = %v, want ErrOTPExpired", err)
	}

	// Seul le canal vérifié (SMS) est utilisé pour la proposition, même si le patient a un email.
	_, token := e.bookAt(t, tuesday9, "Alice", "06 11 11 11 11", "+33611111111")
	if _, err := e.db.Exec(ctx, "UPDATE practice SET notify_change_channel = 'both' WHERE id = $1", e.pc.ID); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.CancelByPatient(ctx, token); err != nil {
		t.Fatal(err)
	}
	if n := e.count(t, "SELECT count(*) FROM outbox_message WHERE practice_id = $1 AND template = 'waitlist' AND kind = 'sms' AND recipient = '+33644444444'", e.pc.ID); n != 1 {
		t.Errorf("SMS de proposition = %d, want 1", n)
	}
}

func TestWaitlistWindowValidation(t *testing.T) {
	t.Parallel()
	e := newBookingEnv(t, thursday)
	wl := e.waitlist()
	ctx := context.Background()
	bad := map[string]func(*service.WindowInput){
		"passé":             func(w *service.WindowInput) { w.Earliest = "2026-10-01" },
		"latest < earliest": func(w *service.WindowInput) { w.Latest = "2026-10-10" },
		"date illisible":    func(w *service.WindowInput) { w.Earliest = "demain" },
		"jour invalide":     func(w *service.WindowInput) { w.Weekdays = []string{"7"} },
		"plage inversée":    func(w *service.WindowInput) { w.From, w.To = "18:00", "09:00" },
		"plage incomplète":  func(w *service.WindowInput) { w.From = "09:00" },
		"trop lointain":     func(w *service.WindowInput) { w.Latest = "2030-01-01" },
	}
	for name, mod := range bad {
		w := e.window()
		mod(&w)
		_, err := wl.AddByStaff(ctx, e.actor(t), service.StaffAddInput{Patient: emailPatient("Bob", "bob@example.test"), Window: w})
		if len(domain.FieldErrors(err)) == 0 {
			t.Errorf("%s : err = %v, want une erreur de validation", name, err)
		}
	}
	// Au plus 5 inscriptions actives par patient.
	for i := range 6 {
		_, err := wl.AddByStaff(ctx, e.actor(t), service.StaffAddInput{Patient: emailPatient("Max", "max@example.test"), Window: e.window()})
		if (i < 5) != (err == nil) {
			t.Fatalf("inscription %d : err = %v", i+1, err)
		}
	}
}

func TestWaitlistExpireAfterLatest(t *testing.T) {
	t.Parallel()
	e := newBookingEnv(t, thursday)
	wl := e.waitlist()
	ctx := context.Background()
	short := e.window()
	short.Latest = "2026-10-16"
	a, err := wl.AddByStaff(ctx, e.actor(t), service.StaffAddInput{Patient: emailPatient("Court", "court@example.test"), Window: short})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := wl.AddByStaff(ctx, e.actor(t), service.StaffAddInput{Patient: emailPatient("Long", "long@example.test"), Window: e.window()}); err != nil {
		t.Fatal(err)
	}
	if n, err := wl.ExpireOnce(ctx); err != nil || n != 0 {
		t.Fatalf("ExpireOnce avant l'échéance = %d, %v", n, err)
	}
	e.clock.Advance(3 * 24 * time.Hour) // 18 octobre
	if n, err := wl.ExpireOnce(ctx); err != nil || n != 1 {
		t.Fatalf("ExpireOnce = %d, %v ; want 1", n, err)
	}
	var status string
	if err := e.db.QueryRow(ctx, "SELECT status FROM waitlist_entry WHERE id = $1", a.ID).Scan(&status); err != nil || status != "expired" {
		t.Errorf("status = %q, %v", status, err)
	}
	if n, _ := wl.ExpireOnce(ctx); n != 0 {
		t.Errorf("ExpireOnce idempotent = %d", n)
	}
}

func TestWaitlistCancelByStaff(t *testing.T) {
	t.Parallel()
	e := newBookingEnv(t, thursday)
	wl := e.waitlist()
	ctx := context.Background()
	entry, err := wl.AddByStaff(ctx, e.actor(t), service.StaffAddInput{Patient: emailPatient("Bob", "bob@example.test"), Window: e.window()})
	if err != nil {
		t.Fatal(err)
	}
	if err := wl.Cancel(ctx, service.Actor{PracticeID: uuid.New()}, entry.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("annulation par un autre cabinet = %v, want ErrNotFound", err)
	}
	if err := wl.Cancel(ctx, e.actor(t), entry.ID); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	if err := wl.Cancel(ctx, e.actor(t), entry.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("seconde annulation = %v, want ErrNotFound", err)
	}
	_, token := e.bookAt(t, tuesday9, "Alice", "06 11 11 11 11", "+33611111111")
	if err := e.svc.CancelByPatient(ctx, token); err != nil {
		t.Fatal(err)
	}
	if n := e.waitlistMessages(t); n != 0 {
		t.Errorf("messages pour une inscription retirée = %d", n)
	}
}

func TestWaitlistOfferedOnStaffCancellation(t *testing.T) {
	t.Parallel()
	e := newBookingEnv(t, thursday)
	wl := e.waitlist()
	ctx := context.Background()
	res, _ := e.bookAt(t, tuesday9, "Alice", "06 11 11 11 11", "+33611111111")
	if _, err := wl.AddByStaff(ctx, e.actor(t), service.StaffAddInput{Patient: emailPatient("Bob", "bob@example.test"), Window: e.window()}); err != nil {
		t.Fatal(err)
	}
	cal := &service.Calendar{Pool: e.db, Clock: e.clock, BaseURL: waitlistBase}
	if err := cal.Cancel(ctx, e.actor(t), res.Appointment.ID, ""); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	if tokens := e.offerTokens(t); len(tokens) != 1 || tokens["bob@example.test"] == "" {
		t.Fatalf("propositions = %v, want Bob", tokens)
	}
}
