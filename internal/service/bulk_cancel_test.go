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

const bulkReason = "Praticien souffrant"

// bulkCal : agenda avec adresse publique (liens de gestion dans les messages).
func (e *bookingEnv) bulkCal() *service.Calendar {
	return &service.Calendar{Pool: e.db, Clock: e.clock, BaseURL: waitlistBase, ManageKey: service.NewManageKey(testutil.ManageSecret)}
}

// adminOn : administrateur agissant sur l'agenda du praticien du cabinet de test.
func (e *bookingEnv) adminOn(t *testing.T) service.Actor {
	a := e.actor(t)
	a.Role, a.Target = domain.RoleAdmin, e.pr.ID
	return a
}

// tuesdayAll : la journée entière du mardi 2026-10-20.
func tuesdayAll() service.ClosureInput {
	return service.ClosureInput{StartDate: "2026-10-20", Reason: bulkReason}
}

func (e *bookingEnv) bulkCount(t *testing.T, where string) int {
	t.Helper()
	return e.count(t, "SELECT count(*) FROM appointment WHERE practitioner_id = $1 AND "+where, e.pr.ID)
}

func TestBulkCancelPreviewIsReadOnlyAndExact(t *testing.T) {
	t.Parallel()
	e := newBookingEnv(t, thursday)
	ctx := context.Background()
	res, token := e.bookAt(t, tuesday9, "Alice", "06 11 11 11 11", "+33611111111")
	e.bookAt(t, tuesday9.Add(time.Hour), "Bob", "06 22 22 22 22", "+33622222222")
	e.bookAt(t, tuesday9.Add(2*time.Hour), "Carl", "06 33 33 33 33", "+33633333333")
	// Un RDV déjà annulé n'est pas listé.
	if err := e.svc.CancelByPatient(ctx, token); err != nil {
		t.Fatal(err)
	}
	_ = res

	pv, err := e.bulkCal().CancelPreview(ctx, e.adminOn(t), service.ClosureInput{StartDate: "2026-10-20", StartTime: "10:00", EndTime: "12:00", Reason: bulkReason})
	if err != nil {
		t.Fatalf("CancelPreview: %v", err)
	}
	if len(pv.Items) != 2 || pv.Items[0].Patient != "Bob Test" || pv.Items[1].Patient != "Carl Test" {
		t.Fatalf("aperçu = %+v, attendu Bob et Carl", pv.Items)
	}
	if pv.Items[0].Start.Format("15:04") != "10:00" || pv.Reason != bulkReason {
		t.Errorf("aperçu = %+v", pv)
	}
	// Rien n'a bougé.
	if n := e.bulkCount(t, "status = 'booked'"); n != 2 {
		t.Errorf("%d RDV à venir après aperçu, attendu 2", n)
	}
	if n := e.count(t, "SELECT count(*) FROM availability_exception WHERE practitioner_id = $1", e.pr.ID); n != 0 {
		t.Errorf("%d absence(s) créée(s) par l'aperçu", n)
	}
	if n := e.count(t, "SELECT count(*) FROM outbox_message WHERE practice_id = $1 AND template = 'cancellation'", e.pc.ID); n != 0 {
		t.Errorf("%d message(s) d'annulation après aperçu", n)
	}
}

func TestBulkCancelRangeCancelsAuditsAndNotifies(t *testing.T) {
	t.Parallel()
	e := newBookingEnv(t, thursday)
	ctx := context.Background()
	e.bookAt(t, tuesday9, "Alice", "06 11 11 11 11", "+33611111111")
	_, bobToken := e.bookAt(t, tuesday9.Add(time.Hour), "Bob", "06 22 22 22 22", "+33622222222")
	_, carlToken := e.bookAt(t, tuesday9.Add(2*time.Hour), "Carl", "06 33 33 33 33", "+33633333333")
	// Carl a déjà annulé : ignoré.
	if err := e.svc.CancelByPatient(ctx, carlToken); err != nil {
		t.Fatal(err)
	}
	sink := &countSink{}
	cal := e.bulkCal()
	cal.Events = sink
	actor := e.adminOn(t)

	// Plage partielle avec heures : 10:00–10:30 ne touche que Bob (Alice à 9 h, la fin est exclue).
	n, err := cal.CancelRange(ctx, actor, service.ClosureInput{StartDate: "2026-10-20", StartTime: "10:00", EndTime: "10:30", Reason: bulkReason})
	if err != nil || n != 1 {
		t.Fatalf("CancelRange partiel = %d, %v ; attendu 1", n, err)
	}
	if st, by, _ := e.status(t, bobToken); st != "cancelled" || by != "staff" {
		t.Errorf("Bob : %s par %s", st, by)
	}
	if n := e.bulkCount(t, "status = 'booked'"); n != 1 {
		t.Errorf("%d RDV à venir, attendu 1 (Alice)", n)
	}
	var reason string
	if err := e.db.QueryRow(ctx, "SELECT cancel_reason FROM appointment WHERE status = 'cancelled' AND cancelled_by = 'staff' AND practitioner_id = $1", e.pr.ID).Scan(&reason); err != nil || reason != bulkReason {
		t.Errorf("cancel_reason = %q, %v", reason, err)
	}
	if got := len(sink.kinds); got != 1 || sink.kinds[0] != service.EventAppointmentCancelled {
		t.Errorf("événements = %v", sink.kinds)
	}

	// L'absence existe et couvre exactement la plage.
	if c := e.count(t, "SELECT count(*) FROM availability_exception WHERE practitioner_id = $1 AND kind = 'closed' AND reason = $2 AND lower(during) = $3 AND upper(during) = $4",
		e.pr.ID, bulkReason, tuesday9.Add(time.Hour), tuesday9.Add(90*time.Minute)); c != 1 {
		t.Errorf("absence attendue, trouvée %d", c)
	}
	// Audit : l'annulation du RDV (bulk) et l'absence.
	if c := e.count(t, "SELECT count(*) FROM audit_log WHERE practice_id = $1 AND action = 'appointment.cancelled' AND meta->>'bulk' = 'true'", e.pc.ID); c != 1 {
		t.Errorf("audit d'annulation: %d", c)
	}
	if c := e.count(t, "SELECT count(*) FROM audit_log WHERE practice_id = $1 AND action = 'availability.closure_cancel' AND meta->>'cancelled' = '1'", e.pc.ID); c != 1 {
		t.Errorf("audit de l'absence: %d", c)
	}
	// Message au patient : motif + lien de gestion de T050.
	var body string
	if err := e.db.QueryRow(ctx, "SELECT payload->>'body' FROM outbox_message WHERE practice_id = $1 AND template = 'cancellation' AND recipient = '+33622222222'", e.pc.ID).Scan(&body); err != nil {
		t.Fatalf("message d'annulation de Bob: %v", err)
	}
	if !strings.Contains(body, bulkReason) || !strings.Contains(body, waitlistBase+"/r/") {
		t.Errorf("SMS = %q, attendu le motif et le lien de gestion", body)
	}
	if c := e.count(t, "SELECT count(*) FROM outbox_message WHERE practice_id = $1 AND template = 'cancellation'", e.pc.ID); c != 1 {
		t.Errorf("%d message(s) d'annulation, attendu 1", c)
	}

	// Journée entière : Alice, seul RDV restant.
	n, err = cal.CancelRange(ctx, actor, tuesdayAll())
	if err != nil || n != 1 {
		t.Fatalf("CancelRange journée = %d, %v ; attendu 1", n, err)
	}
	if c := e.bulkCount(t, "status = 'booked'"); c != 0 {
		t.Errorf("%d RDV à venir restants", c)
	}
}

func TestBulkCancelIsAtomic(t *testing.T) {
	t.Parallel()
	e := newBookingEnv(t, thursday)
	ctx := context.Background()
	e.bookAt(t, tuesday9, "Alice", "06 11 11 11 11", "+33611111111")
	e.bookAt(t, tuesday9.Add(time.Hour), "Bob", "06 22 22 22 22", "+33622222222")
	// La mise en file du message de Bob échoue : tout doit être annulé, y compris
	// l'absence et l'annulation d'Alice, traitée avant.
	if _, err := e.db.Exec(ctx, `
		CREATE FUNCTION fail_bob() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'échec simulé'; END $$;
		CREATE TRIGGER fail_bob BEFORE INSERT ON outbox_message FOR EACH ROW
		WHEN (NEW.template = 'cancellation' AND NEW.recipient = '+33622222222') EXECUTE FUNCTION fail_bob();`); err != nil {
		t.Fatal(err)
	}
	if _, err := e.bulkCal().CancelRange(ctx, e.adminOn(t), tuesdayAll()); err == nil {
		t.Fatal("CancelRange a réussi malgré l'échec simulé")
	}
	if c := e.bulkCount(t, "status = 'booked'"); c != 2 {
		t.Errorf("%d RDV à venir, attendu 2 (rien annulé)", c)
	}
	if c := e.count(t, "SELECT count(*) FROM availability_exception WHERE practitioner_id = $1", e.pr.ID); c != 0 {
		t.Errorf("%d absence(s) laissée(s)", c)
	}
	if c := e.count(t, "SELECT count(*) FROM outbox_message WHERE practice_id = $1 AND template = 'cancellation'", e.pc.ID); c != 0 {
		t.Errorf("%d message(s) laissé(s)", c)
	}
	if c := e.count(t, "SELECT count(*) FROM audit_log WHERE practice_id = $1 AND action IN ('appointment.cancelled', 'availability.closure_cancel')", e.pc.ID); c != 0 {
		t.Errorf("%d ligne(s) d'audit laissée(s)", c)
	}
}

// Aucune offre de liste d'attente pour un créneau de la plage fermée ; le témoin
// (annulation simple) prouve que l'inscrit aurait été prévenu.
func TestBulkCancelDoesNotOfferClosedSlotsToWaitlist(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	witness := newBookingEnv(t, thursday)
	wl := witness.waitlist()
	res, _ := witness.bookAt(t, tuesday9, "Alice", "06 11 11 11 11", "+33611111111")
	if _, err := wl.AddByStaff(ctx, witness.actor(t), service.StaffAddInput{Patient: emailPatient("Carl", "carl@example.test"), Window: witness.window()}); err != nil {
		t.Fatal(err)
	}
	if err := witness.bulkCal().Cancel(ctx, witness.actor(t), res.Appointment.ID, ""); err != nil {
		t.Fatal(err)
	}
	if n := witness.waitlistMessages(t); n != 1 {
		t.Fatalf("témoin : %d proposition(s), attendu 1", n)
	}

	e := newBookingEnv(t, thursday)
	wl = e.waitlist()
	e.bookAt(t, tuesday9, "Alice", "06 11 11 11 11", "+33611111111")
	if _, err := wl.AddByStaff(ctx, e.actor(t), service.StaffAddInput{Patient: emailPatient("Carl", "carl@example.test"), Window: e.window()}); err != nil {
		t.Fatal(err)
	}
	n, err := e.bulkCal().CancelRange(ctx, e.adminOn(t), service.ClosureInput{StartDate: "2026-10-20", StartTime: "09:00", EndTime: "10:00", Reason: bulkReason})
	if err != nil || n != 1 {
		t.Fatalf("CancelRange = %d, %v", n, err)
	}
	if n := e.waitlistMessages(t); n != 0 {
		t.Errorf("%d proposition(s) pour un créneau fermé", n)
	}
	if n := e.count(t, "SELECT count(*) FROM waitlist_offer WHERE practice_id = $1", e.pc.ID); n != 0 {
		t.Errorf("%d offre(s) enregistrée(s)", n)
	}
}

func TestBulkCancelAccess(t *testing.T) {
	t.Parallel()
	e := newBookingEnv(t, thursday)
	ctx := context.Background()
	e.bookAt(t, tuesday9, "Alice", "06 11 11 11 11", "+33611111111")
	cal := e.bulkCal()
	base := e.adminOn(t)

	other := e.otherPractitioner(t)
	cases := []struct {
		name string
		act  service.Actor
		ok   bool
	}{
		{"praticien sur son agenda", service.Actor{UserID: base.UserID, PracticeID: e.pc.ID, Role: domain.RolePractitioner, PractitionerID: e.pr.ID, Target: e.pr.ID}, true},
		{"autre praticien", service.Actor{UserID: base.UserID, PracticeID: e.pc.ID, Role: domain.RolePractitioner, PractitionerID: other, Target: e.pr.ID}, false},
		{"secrétariat", service.Actor{UserID: base.UserID, PracticeID: e.pc.ID, Role: domain.RoleSecretary, Target: e.pr.ID}, false},
		{"remplaçant délégué", service.Actor{UserID: base.UserID, PracticeID: e.pc.ID, Role: domain.RolePractitioner, PractitionerID: other, Delegated: []uuid.UUID{e.pr.ID}, Target: e.pr.ID}, true},
	}
	for _, c := range cases {
		_, perr := cal.CancelPreview(ctx, c.act, tuesdayAll())
		if (perr == nil) != c.ok || (!c.ok && !errors.Is(perr, domain.ErrForbidden)) {
			t.Errorf("%s : aperçu = %v, attendu ok=%v", c.name, perr, c.ok)
		}
		if !c.ok {
			if _, err := cal.CancelRange(ctx, c.act, tuesdayAll()); !errors.Is(err, domain.ErrForbidden) {
				t.Errorf("%s : CancelRange = %v, attendu ErrForbidden", c.name, err)
			}
		}
	}
	if n := e.bulkCount(t, "status = 'booked'"); n != 1 {
		t.Errorf("%d RDV à venir : un refus a pourtant modifié des données", n)
	}
	if n := e.count(t, "SELECT count(*) FROM availability_exception WHERE practitioner_id = $1", e.pr.ID); n != 0 {
		t.Errorf("%d absence(s) créée(s) malgré les refus", n)
	}
}

func TestBulkCancelValidation(t *testing.T) {
	t.Parallel()
	e := newBookingEnv(t, thursday)
	ctx := context.Background()
	cal := e.bulkCal()
	for name, in := range map[string]service.ClosureInput{
		"sans date":       {},
		"dans le passé":   {StartDate: "2026-10-01"},
		"motif trop long": {StartDate: "2026-10-20", Reason: strings.Repeat("x", 201)},
		"une seule heure": {StartDate: "2026-10-20", StartTime: "09:00"},
	} {
		if _, err := cal.CancelPreview(ctx, e.adminOn(t), in); len(domain.FieldErrors(err)) == 0 {
			t.Errorf("%s : aperçu = %v, attendu une erreur de validation", name, err)
		}
		if _, err := cal.CancelRange(ctx, e.adminOn(t), in); len(domain.FieldErrors(err)) == 0 {
			t.Errorf("%s : CancelRange = %v, attendu une erreur de validation", name, err)
		}
	}
}
