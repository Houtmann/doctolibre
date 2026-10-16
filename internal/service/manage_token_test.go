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
	"doctolibre/internal/testutil"
)

func TestDeriveManageToken(t *testing.T) {
	t.Parallel()
	key := service.NewManageKey(testutil.ManageSecret)
	id := uuid.New()
	tok := service.DeriveManageToken(key, id, 1)
	if tok == "" || tok != service.DeriveManageToken(key, id, 1) || tok != key.Token(id, 1) {
		t.Fatalf("dérivation instable : %q", tok)
	}
	if len(tok) != 43 || strings.ContainsAny(tok, "+/=") {
		t.Errorf("jeton %q : base64url sans padding de 32 octets attendu", tok)
	}
	for name, other := range map[string]string{
		"version":                    service.DeriveManageToken(key, id, 2),
		"id":                         service.DeriveManageToken(key, uuid.New(), 1),
		"clé":                        service.DeriveManageToken(service.NewManageKey(strings.Repeat("x", 32)), id, 1),
		"non dérivée du secret brut": service.DeriveManageToken([]byte(testutil.ManageSecret), id, 1),
	} {
		if other == tok {
			t.Errorf("changer %s ne change pas le jeton", name)
		}
	}
}

func TestBookedAppointmentLinkIsDerived(t *testing.T) {
	t.Parallel()
	e := newBookingEnv(t, thursday)
	res := bookOnline(t, e, tuesday9)
	want := service.NewManageKey(testutil.ManageSecret).Token(res.Appointment.ID, 1)
	if res.Token != want {
		t.Fatalf("jeton en ligne non dérivé")
	}
	if _, err := e.svc.Confirmation(context.Background(), want); err != nil {
		t.Errorf("RDV non gérable par son lien dérivé : %v", err)
	}
}

func TestStaffAppointmentLinkIsDerived(t *testing.T) {
	t.Parallel()
	f := newCalFixture(t)
	ctx := context.Background()
	appt, err := f.cal.Create(ctx, f.act, f.quick("2026-10-19", "09:00", false))
	if err != nil {
		t.Fatal(err)
	}
	b := &service.Booking{Pool: f.db, Clock: f.cal.Clock, ManageKey: service.NewManageKey(testutil.ManageSecret)}
	if _, err := b.Confirmation(ctx, b.ManageKey.Token(appt.ID, 1)); err != nil {
		t.Errorf("RDV du cabinet non gérable par son lien dérivé : %v", err)
	}
}

func TestCarePlanSessionsHaveDerivedLinks(t *testing.T) {
	t.Parallel()
	f := newPlanFixture(t)
	ctx := context.Background()
	prop, err := f.plans.Propose(ctx, f.act, f.input())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.plans.Create(ctx, f.act, service.CreateInput{Plan: f.input(), Lines: f.lines(t, prop)}); err != nil {
		t.Fatal(err)
	}
	b := &service.Booking{Pool: f.db, Clock: f.cal.Clock, ManageKey: service.NewManageKey(testutil.ManageSecret)}
	rows, err := f.db.Query(ctx, "SELECT id FROM appointment WHERE practice_id = $1", f.act.PracticeID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		c, err := b.Confirmation(ctx, b.ManageKey.Token(id, 1))
		if err != nil || c.Appointment.ID != id {
			t.Fatalf("séance %s non gérable par son lien : %v", id, err)
		}
		n++
	}
	if n != 10 {
		t.Errorf("%d séances, attendu 10", n)
	}
}

func TestWaitlistClaimedAppointmentLinkIsDerived(t *testing.T) {
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
		t.Fatal(err)
	}
	if want := service.NewManageKey(testutil.ManageSecret).Token(res.Appointment.ID, 1); res.Token != want {
		t.Errorf("jeton de l'offre non dérivé")
	}
}

func TestReminderCarriesManageLinkOnlyWhenHashMatches(t *testing.T) {
	t.Parallel()
	e := newBookingEnv(t, thursday)
	neu := bookOnline(t, e, tuesday9)
	old := bookOnline(t, e, tuesday9.Add(time.Hour))
	// Ancien RDV : jeton aléatoire, sans rapport avec la dérivation.
	mustExec(t, e.db, "UPDATE appointment SET manage_token_hash = $1 WHERE id = $2", service.ManageTokenHash("jeton-aleatoire-herite"), old.Appointment.ID)

	e.clock.Advance(time.Date(2026, 10, 19, 18, 5, 0, 0, paris).Sub(thursday))
	rem := e.reminders()
	rem.BaseURL = baseURL
	if n, err := rem.RunOnce(context.Background()); err != nil || n != 2 {
		t.Fatalf("RunOnce : n=%d err=%v", n, err)
	}
	for _, m := range only(outMsgs(t, e.db, e.pc.ID), "reminder") {
		b := m.body(t)
		switch {
		case strings.Contains(m.DedupKey, neu.Appointment.ID.String()):
			if !strings.Contains(b, baseURL+"/r/"+neu.Token) {
				t.Errorf("rappel du RDV neuf sans lien : %q", b)
			}
		case strings.Contains(m.DedupKey, old.Appointment.ID.String()):
			if strings.Contains(b, "/r/") {
				t.Errorf("rappel d'un ancien RDV avec un lien : %q", b)
			}
		}
	}
}

func TestStaffCancellationCarriesLinkAndCancelledPageStaysReadable(t *testing.T) {
	t.Parallel()
	f := newCalFixture(t)
	f.cal.BaseURL = baseURL
	ctx := context.Background()
	appt, err := f.cal.Create(ctx, f.act, f.quick("2026-10-19", "09:00", false))
	if err != nil {
		t.Fatal(err)
	}
	if err := f.cal.Cancel(ctx, f.act, appt.ID, "imprévu"); err != nil {
		t.Fatal(err)
	}
	token := service.NewManageKey(testutil.ManageSecret).Token(appt.ID, 1)
	c := only(outMsgs(t, f.db, f.act.PracticeID), "cancellation")
	if len(c) != 1 || !strings.Contains(c[0].body(t), baseURL+"/r/"+token) {
		t.Fatalf("annulation sans lien de gestion : %+v", c)
	}
	b := &service.Booking{Pool: f.db, Clock: f.cal.Clock, ManageKey: service.NewManageKey(testutil.ManageSecret)}
	got, err := b.Confirmation(ctx, token)
	if err != nil || got.Appointment.Status != "cancelled" {
		t.Fatalf("lien d'un RDV annulé : %v %q", err, got.Appointment.Status)
	}
}

func TestPatientCancelKeepsLinkReadable(t *testing.T) {
	t.Parallel()
	e := newBookingEnv(t, thursday)
	res := bookOnline(t, e, tuesday9)
	if err := e.svc.CancelByPatient(context.Background(), res.Token); err != nil {
		t.Fatal(err)
	}
	if st, _, _ := e.status(t, res.Token); st != "cancelled" {
		t.Errorf("statut = %q", st)
	}
}

func TestRevokeManageToken(t *testing.T) {
	t.Parallel()
	e := newBookingEnv(t, thursday)
	res := bookOnline(t, e, tuesday9)
	ctx := context.Background()
	fresh, err := e.svc.RevokeManageToken(ctx, e.pc.ID, res.Appointment.ID)
	if err != nil {
		t.Fatal(err)
	}
	if fresh == res.Token || fresh != service.NewManageKey(testutil.ManageSecret).Token(res.Appointment.ID, 2) {
		t.Errorf("nouveau jeton inattendu")
	}
	if _, err := e.svc.Confirmation(ctx, res.Token); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("ancien lien encore valide : %v", err)
	}
	if _, err := e.svc.Confirmation(ctx, fresh); err != nil {
		t.Errorf("nouveau lien refusé : %v", err)
	}
	// Autre cabinet : introuvable, rien n'est modifié.
	if _, err := e.svc.RevokeManageToken(ctx, uuid.New(), res.Appointment.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("révocation inter-cabinets : %v", err)
	}
}

func TestErasureInvalidatesManageLink(t *testing.T) {
	t.Parallel()
	e := newBookingEnv(t, thursday)
	res := bookOnline(t, e, tuesday9)
	ctx := context.Background()
	if err := e.svc.CancelByPatient(ctx, res.Token); err != nil { // l'effacement refuse les RDV à venir
		t.Fatal(err)
	}
	pat := &service.Patients{Pool: e.db, Clock: e.clock}
	if _, err := pat.Erase(ctx, e.actor(t), res.Appointment.PatientID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Confirmation(ctx, res.Token); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("lien encore valide après effacement : %v", err)
	}
}

func TestMissingManageKeyIsAnError(t *testing.T) {
	t.Parallel()
	e := newBookingEnv(t, thursday)
	b := &service.Booking{Pool: e.db, Clock: e.clock}
	if _, err := b.RevokeManageToken(context.Background(), e.pc.ID, uuid.New()); !errors.Is(err, service.ErrNoManageKey) {
		t.Errorf("err = %v", err)
	}
}
