package service_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"doctolibre/internal/domain"
	"doctolibre/internal/service"
)

// joinOnline inscrit un patient en ligne par email ; retourne l'erreur de Join.
func (e *bookingEnv) joinOnline(t *testing.T, wl *service.Waitlist, first, email string) error {
	t.Helper()
	ctx := context.Background()
	in := service.WaitlistVerifyInput{
		Window:  service.WindowInput{PractitionerID: e.pr.ID, TypeID: e.typ.ID, Earliest: "2026-10-15", Latest: "2026-10-31"},
		Patient: service.PatientDetails{FirstName: first, LastName: "Web", Email: email, SMSOptIn: true, EmailOptIn: true}, Channel: "email",
	}
	v, err := wl.StartVerification(ctx, in)
	if err != nil {
		t.Fatalf("StartVerification: %v", err)
	}
	_, err = wl.Join(ctx, service.WaitlistJoinInput{WaitlistVerifyInput: in, ChallengeID: v.ChallengeID, Code: e.otp.code(email)})
	return err
}

func (e *bookingEnv) recapLink(t *testing.T, recipient string) []string {
	t.Helper()
	rows, err := e.db.Query(context.Background(),
		"SELECT payload->>'leave_url' FROM outbox_message WHERE practice_id = $1 AND template = 'waitlist_joined' AND recipient = $2", e.pc.ID, recipient)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var l string
		if err := rows.Scan(&l); err != nil {
			t.Fatal(err)
		}
		out = append(out, l)
	}
	return out
}

func TestWaitlistJoinQueuesRecapOnceAndLeaveLink(t *testing.T) {
	t.Parallel()
	e := newBookingEnv(t, thursday)
	wl := e.waitlist()
	ctx := context.Background()
	if err := e.joinOnline(t, wl, "Eve", "eve@example.test"); err != nil {
		t.Fatalf("Join: %v", err)
	}
	links := e.recapLink(t, "eve@example.test")
	if len(links) != 1 || !strings.HasPrefix(links[0], waitlistBase+"/wl/") {
		t.Fatalf("récapitulatifs = %v, want un seul avec lien /wl/", links)
	}
	token := strings.TrimPrefix(links[0], waitlistBase+"/wl/")
	// Seul le haché est stocké.
	if n := e.count(t, "SELECT count(*) FROM waitlist_entry WHERE unsubscribe_hash = $1", token); n != 0 {
		t.Error("jeton de désinscription stocké en clair")
	}
	if n := e.count(t, "SELECT count(*) FROM waitlist_entry WHERE unsubscribe_hash = $1", service.ManageTokenHash(token)); n != 1 {
		t.Error("haché du jeton absent")
	}

	// Lecture seule : la page de confirmation ne désinscrit pas.
	v, err := wl.LeaveView(ctx, token)
	if err != nil || v.Practice.ID != e.pc.ID || v.Type == "" || v.Window == "" {
		t.Fatalf("LeaveView = %+v, %v", v, err)
	}
	if n := e.count(t, "SELECT count(*) FROM waitlist_entry WHERE status = 'active'"); n != 1 {
		t.Fatalf("inscriptions actives après lecture = %d, want 1", n)
	}
	if err := wl.Leave(ctx, token); err != nil {
		t.Fatalf("Leave: %v", err)
	}
	if n := e.count(t, "SELECT count(*) FROM waitlist_entry WHERE status = 'cancelled'"); n != 1 {
		t.Errorf("inscriptions retirées = %d, want 1", n)
	}
	// Jeton rejoué, inconnu ou mal formé : même erreur neutre.
	for _, tk := range []string{token, "inconnu", "", strings.Repeat("a", 200)} {
		if err := wl.Leave(ctx, tk); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("Leave(%.10q) = %v, want ErrNotFound", tk, err)
		}
		if _, err := wl.LeaveView(ctx, tk); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("LeaveView(%.10q) = %v, want ErrNotFound", tk, err)
		}
	}
}

func TestWaitlistJoinFailureQueuesNoRecap(t *testing.T) {
	t.Parallel()
	e := newBookingEnv(t, thursday)
	wl := e.waitlist()
	ctx := context.Background()
	// Le patient a déjà 5 inscriptions actives (cabinet) : la sixième, en ligne, échoue.
	p := service.PatientDetails{FirstName: "Fred", LastName: "Web", Email: "fred@example.test", SMSOptIn: true, EmailOptIn: true}
	for range 5 {
		if _, err := wl.AddByStaff(ctx, e.actor(t), service.StaffAddInput{Patient: p, Window: e.window()}); err != nil {
			t.Fatalf("AddByStaff: %v", err)
		}
	}
	var ve domain.ErrValidation
	if err := e.joinOnline(t, wl, "Fred", "fred@example.test"); !errors.As(err, &ve) {
		t.Fatalf("Join = %v, want ErrValidation", err)
	}
	if n := e.count(t, "SELECT count(*) FROM outbox_message WHERE practice_id = $1 AND template = 'waitlist_joined'", e.pc.ID); n != 0 {
		t.Errorf("récapitulatifs après échec = %d, want 0", n)
	}
	// Les inscriptions du cabinet n'envoient pas de récapitulatif.
	if links := e.recapLink(t, "fred@example.test"); len(links) != 0 {
		t.Errorf("récapitulatif pour une saisie du cabinet: %v", links)
	}
}

// TestWaitlistOTPPurposeText : l'OTP de liste d'attente utilise son propre texte (email et SMS) ;
// celui de réservation reste sur le template « otp » (voir TestOutboxOTPSender).
func TestWaitlistOTPPurposeText(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	e := newBookingEnv(t, thursday)
	wl := e.waitlist()
	wl.OTP = service.OutboxOTPSender{}
	in := service.WaitlistVerifyInput{
		Window:  service.WindowInput{PractitionerID: e.pr.ID, TypeID: e.typ.ID, Earliest: "2026-10-15", Latest: "2026-10-31"},
		Patient: patient("Dora", "Web", "06 44 44 44 44"), Channel: "sms",
	}
	if _, err := wl.StartVerification(ctx, in); err != nil {
		t.Fatal(err)
	}
	var body string
	if err := e.db.QueryRow(ctx, "SELECT payload->>'body' FROM outbox_message WHERE kind = 'sms' AND template = 'otp'").Scan(&body); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body, "liste d'attente") || strings.Contains(body, "rendez-vous") {
		t.Errorf("SMS = %q", body)
	}
	in.Channel, in.Patient.Email = "email", "dora@example.test"
	if _, err := wl.StartVerification(ctx, in); err != nil {
		t.Fatal(err)
	}
	if n := e.count(t, "SELECT count(*) FROM outbox_message WHERE kind = 'email' AND template = 'otp_waitlist' AND recipient = 'dora@example.test'"); n != 1 {
		t.Errorf("emails otp_waitlist = %d, want 1", n)
	}
}

func TestWaitlistOfferCarriesLeaveLink(t *testing.T) {
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
	var link string
	if err := e.db.QueryRow(ctx, "SELECT payload->>'leave_url' FROM outbox_message WHERE practice_id = $1 AND template = 'waitlist' AND kind = 'email'", e.pc.ID).Scan(&link); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(link, waitlistBase+"/wl/") {
		t.Fatalf("leave_url de l'offre = %q", link)
	}
	if err := wl.Leave(ctx, strings.TrimPrefix(link, waitlistBase+"/wl/")); err != nil {
		t.Fatalf("Leave depuis le lien d'une offre: %v", err)
	}
}
