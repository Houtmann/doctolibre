package service_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"doctolibre/internal/domain"
	"doctolibre/internal/service"
	"doctolibre/internal/store/sqlcgen"
	"doctolibre/internal/testutil"
)

// captureOTP est un OTPSender qui mémorise le dernier code par destinataire.
type captureOTP struct {
	mu    sync.Mutex
	codes map[string]string
	sent  int
}

func (c *captureOTP) SendOTP(_ context.Context, _ pgx.Tx, _ uuid.UUID, _, _, recipient, code string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.codes == nil {
		c.codes = map[string]string{}
	}
	c.codes[recipient] = code
	c.sent++
	return nil
}

func (c *captureOTP) code(recipient string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.codes[recipient]
}

var paris = func() *time.Location {
	l, err := time.LoadLocation("Europe/Paris")
	if err != nil {
		panic(err)
	}
	return l
}()

type bookingEnv struct {
	db    *pgxpool.Pool
	clock *testutil.FixedClock
	otp   *captureOTP
	svc   *service.Booking
	pc    sqlcgen.Practice
	pr    sqlcgen.Practitioner
	typ   sqlcgen.AppointmentType
}

// newBookingEnv crée un cabinet dont le praticien reçoit le mardi de 9 h à 12 h
// et le dimanche de 9 h à 10 h (heure de Paris) ; l'horloge est figée à now.
func newBookingEnv(t *testing.T, now time.Time) *bookingEnv {
	t.Helper()
	db := testutil.DB(t)
	pc := testutil.NewPractice(t, db)
	pr := testutil.NewPractitioner(t, db, pc.ID)
	typ := testutil.NewAppointmentType(t, db, pc.ID)
	addRule(t, db, pr.ID, 1, 9, 12, nil)
	addRule(t, db, pr.ID, 6, 9, 10, nil)
	clock := testutil.NewFixedClock(now)
	otp := &captureOTP{}
	return &bookingEnv{db: db, clock: clock, otp: otp, pc: pc, pr: pr, typ: typ,
		svc: &service.Booking{Pool: db, Clock: clock, OTP: otp, ManageKey: service.NewManageKey(testutil.ManageSecret)}}
}

// addRule ajoute une plage hebdomadaire (0 = lundi) valable depuis 2026-01-01.
func addRule(t *testing.T, db *pgxpool.Pool, practitioner uuid.UUID, weekday int16, fromH, toH int, types []uuid.UUID) {
	t.Helper()
	_, err := sqlcgen.New(db).CreateAvailabilityRule(context.Background(), sqlcgen.CreateAvailabilityRuleParams{
		PractitionerID: practitioner, Weekday: weekday,
		StartTime:          pgtype.Time{Microseconds: int64(fromH) * time.Hour.Microseconds(), Valid: true},
		EndTime:            pgtype.Time{Microseconds: int64(toH) * time.Hour.Microseconds(), Valid: true},
		ValidFrom:          pgtype.Date{Time: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), Valid: true},
		AppointmentTypeIds: types,
	})
	if err != nil {
		t.Fatalf("addRule: %v", err)
	}
}

func patient(first, last, phone string) service.PatientDetails {
	return service.PatientDetails{FirstName: first, LastName: last, Phone: phone, SMSOptIn: true, EmailOptIn: true}
}

func (e *bookingEnv) verifyInput(start time.Time, p service.PatientDetails) service.VerifyInput {
	return service.VerifyInput{PractitionerID: e.pr.ID, TypeID: e.typ.ID, Start: start, Patient: p, Channel: "sms"}
}

// start envoie un code et retourne l'entrée de Book prête à l'emploi.
func (e *bookingEnv) start(t *testing.T, start time.Time, p service.PatientDetails, phoneE164 string) service.BookInput {
	t.Helper()
	in := e.verifyInput(start, p)
	v, err := e.svc.StartVerification(context.Background(), in)
	if err != nil {
		t.Fatalf("StartVerification: %v", err)
	}
	return service.BookInput{VerifyInput: in, ChallengeID: v.ChallengeID, Code: e.otp.code(phoneE164)}
}

func (e *bookingEnv) count(t *testing.T, sql string, args ...any) int {
	t.Helper()
	var n int
	if err := e.db.QueryRow(context.Background(), sql, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

var (
	thursday = time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC)
	tuesday9 = time.Date(2026, 10, 20, 9, 0, 0, 0, paris) // 07:00 UTC (CEST)
)

func TestBookConcurrentSameSlot(t *testing.T) {
	t.Parallel()
	e := newBookingEnv(t, thursday)
	a := e.start(t, tuesday9, patient("Alice", "Durand", "06 11 11 11 11"), "+33611111111")
	b := e.start(t, tuesday9, patient("Bruno", "Martin", "06 22 22 22 22"), "+33622222222")

	var wg sync.WaitGroup
	gate := make(chan struct{})
	errs := make([]error, 2)
	for i, in := range []service.BookInput{a, b} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-gate
			_, errs[i] = e.svc.Book(context.Background(), in)
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
	if n := e.count(t, "SELECT count(*) FROM appointment WHERE practitioner_id = $1 AND status = 'booked'", e.pr.ID); n != 1 {
		t.Errorf("%d rendez-vous en base, attendu 1", n)
	}
	// Le perdant n'a pas consommé son code : la transaction est annulée en entier.
	if n := e.count(t, "SELECT count(*) FROM otp_challenge WHERE consumed_at IS NOT NULL"); n != 1 {
		t.Errorf("%d codes consommés, attendu 1", n)
	}
}

func TestForgedSlotRefused(t *testing.T) {
	t.Parallel()
	e := newBookingEnv(t, thursday)
	in := e.verifyInput(tuesday9, patient("Alice", "Durand", "0611111111"))
	forged := map[string]time.Time{
		"hors grille (9 h 07)":       tuesday9.Add(7 * time.Minute),
		"jour sans plage (mercredi)": tuesday9.AddDate(0, 0, 1),
		"avant la plage (8 h)":       tuesday9.Add(-time.Hour),
		"après la plage (12 h)":      tuesday9.Add(3 * time.Hour),
		"dans le passé":              thursday.Add(-48 * time.Hour),
		"au-delà de l'horizon":       tuesday9.AddDate(0, 0, 70),
		"avant le délai minimal":     time.Date(2026, 10, 15, 10, 0, 0, 0, paris), // jeudi, pas de plage de toute façon
	}
	for name, start := range forged {
		in.Start = start
		if _, err := e.svc.StartVerification(context.Background(), in); !errors.Is(err, domain.ErrSlotTaken) {
			t.Errorf("%s : StartVerification = %v, attendu ErrSlotTaken", name, err)
		}
		if err := e.svc.CheckSlot(context.Background(), e.pr.ID, e.typ.ID, start); !errors.Is(err, domain.ErrSlotTaken) {
			t.Errorf("%s : CheckSlot = %v, attendu ErrSlotTaken", name, err)
		}
	}
	if e.otp.sent != 0 {
		t.Errorf("%d codes envoyés pour des créneaux refusés", e.otp.sent)
	}

	// Délai minimal de réservation (2 h par défaut) : à 8 h, le créneau de 9 h est trop proche, celui de 11 h non.
	e.clock.Advance(tuesday9.Add(-time.Hour).Sub(thursday))
	if err := e.svc.CheckSlot(context.Background(), e.pr.ID, e.typ.ID, tuesday9); !errors.Is(err, domain.ErrSlotTaken) {
		t.Errorf("créneau dans le délai minimal : %v", err)
	}
	if err := e.svc.CheckSlot(context.Background(), e.pr.ID, e.typ.ID, tuesday9.Add(2*time.Hour)); err != nil {
		t.Errorf("créneau hors délai minimal : %v", err)
	}
}

func TestBookRevalidatesSlotAfterCode(t *testing.T) {
	t.Parallel()
	e := newBookingEnv(t, thursday)
	in := e.start(t, tuesday9, patient("Alice", "Durand", "0611111111"), "+33611111111")
	// Entre l'envoi du code et la confirmation, le praticien s'absente.
	_, err := sqlcgen.New(e.db).CreateAvailabilityException(context.Background(), sqlcgen.CreateAvailabilityExceptionParams{
		PractitionerID: e.pr.ID, StartsAt: tuesday9.Add(-time.Hour), EndsAt: tuesday9.Add(time.Hour), Kind: "closed",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Book(context.Background(), in); !errors.Is(err, domain.ErrSlotTaken) {
		t.Fatalf("Book = %v, attendu ErrSlotTaken", err)
	}
	if n := e.count(t, "SELECT count(*) FROM appointment"); n != 0 {
		t.Errorf("%d rendez-vous créés", n)
	}
}

func TestBookCopiesBuffersAndCreatesPatient(t *testing.T) {
	t.Parallel()
	e := newBookingEnv(t, thursday)
	if _, err := e.db.Exec(context.Background(),
		"UPDATE appointment_type SET buffer_before = interval '10 minutes', buffer_after = interval '15 minutes' WHERE id = $1", e.typ.ID); err != nil {
		t.Fatal(err)
	}
	in := e.start(t, tuesday9, patient("Alice", "Durand", "06 11 11 11 11"), "+33611111111")
	res, err := e.svc.Book(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	a := res.Appointment
	if a.Source != "online" || a.Status != "booked" {
		t.Errorf("source=%s statut=%s", a.Source, a.Status)
	}
	if !a.During.Lower.Time.Equal(tuesday9) || !a.During.Upper.Time.Equal(tuesday9.Add(30*time.Minute)) {
		t.Errorf("during = %v", a.During)
	}
	wantLo, wantHi := tuesday9.Add(-10*time.Minute), tuesday9.Add(45*time.Minute)
	if !a.DuringBlocked.Lower.Time.Equal(wantLo) || !a.DuringBlocked.Upper.Time.Equal(wantHi) {
		t.Errorf("during_blocked = %v, attendu [%v,%v)", a.DuringBlocked, wantLo, wantHi)
	}
	// Zone bloquée [8 h 50, 9 h 45) : le prochain créneau de la grille dont le buffer avant (10 min) ne la touche pas est 10 h.
	slots, err := e.svc.AvailableSlots(context.Background(), e.pr.ID, e.typ.ID, tuesday9, tuesday9.Add(3*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(slots) == 0 || !slots[0].Start.Equal(tuesday9.Add(time.Hour)) {
		t.Errorf("premier créneau après réservation = %v", slots)
	}
	p, err := sqlcgen.New(e.db).GetPatient(context.Background(), sqlcgen.GetPatientParams{ID: a.PatientID, PracticeID: e.pc.ID})
	if err != nil {
		t.Fatal(err)
	}
	if p.Phone.String != "+33611111111" || p.FirstName != "Alice" {
		t.Errorf("patient = %+v", p)
	}
	if !a.ManageTokenHash.Valid || a.ManageTokenHash.String == res.Token || a.ManageTokenHash.String != service.ManageTokenHash(res.Token) {
		t.Error("le jeton de gestion doit être stocké haché")
	}
}

func TestFindOrCreatePatientReuse(t *testing.T) {
	t.Parallel()
	e := newBookingEnv(t, thursday)
	book := func(start time.Time, p service.PatientDetails, e164 string) sqlcgen.Appointment {
		res, err := e.svc.Book(context.Background(), e.start(t, start, p, e164))
		if err != nil {
			t.Fatal(err)
		}
		return res.Appointment
	}
	a1 := book(tuesday9, patient("Alice", "Durand", "06 11 11 11 11"), "+33611111111")
	a2 := book(tuesday9.Add(time.Hour), patient("ALICE", "durand", "+33 6 11 11 11 11"), "+33611111111")
	if a1.PatientID != a2.PatientID {
		t.Error("même téléphone et même nom : le patient doit être réutilisé")
	}
	// Même téléphone, autre prénom (famille) : fiche distincte.
	a3 := book(tuesday9.Add(2*time.Hour), patient("Léo", "Durand", "0611111111"), "+33611111111")
	if a3.PatientID == a1.PatientID {
		t.Error("un autre prénom sur le même téléphone ne doit pas être fusionné")
	}
	// Retrouvé par email + nom quand le téléphone diffère.
	p := service.PatientDetails{FirstName: "Alice", LastName: "Durand", Phone: "0699999999", Email: "Alice@Example.test", SMSOptIn: true}
	mail := tuesday9.AddDate(0, 0, 7)
	in := e.verifyInput(mail, p)
	v, err := e.svc.StartVerification(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	res, err := e.svc.Book(context.Background(), service.BookInput{VerifyInput: in, ChallengeID: v.ChallengeID, Code: e.otp.code("+33699999999")})
	if err != nil {
		t.Fatal(err)
	}
	if res.Appointment.PatientID == a1.PatientID {
		t.Error("téléphone différent, email jamais vu : nouvelle fiche attendue")
	}
	in2 := e.verifyInput(mail.Add(time.Hour), service.PatientDetails{FirstName: "alice", LastName: "DURAND", Phone: "0688888888", Email: "alice@example.test"})
	v2, err := e.svc.StartVerification(context.Background(), in2)
	if err != nil {
		t.Fatal(err)
	}
	res2, err := e.svc.Book(context.Background(), service.BookInput{VerifyInput: in2, ChallengeID: v2.ChallengeID, Code: e.otp.code("+33688888888")})
	if err != nil {
		t.Fatal(err)
	}
	if res2.Appointment.PatientID != res.Appointment.PatientID {
		t.Error("même email et même nom : le patient doit être retrouvé")
	}
}

func TestOTPRules(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	e := newBookingEnv(t, thursday)

	t.Run("code incorrect puis correct", func(t *testing.T) {
		in := e.start(t, tuesday9, patient("A", "Un", "0611000001"), "+33611000001")
		good := in.Code
		in.Code = wrongCode(good)
		if _, err := e.svc.Book(ctx, in); !errors.Is(err, service.ErrOTPWrong) {
			t.Fatalf("code faux : %v", err)
		}
		in.Code = good
		if _, err := e.svc.Book(ctx, in); err != nil {
			t.Fatalf("code juste après un échec : %v", err)
		}
		// Le code ne sert qu'une fois.
		if _, err := e.svc.Book(ctx, in); !errors.Is(err, service.ErrOTPExpired) {
			t.Fatalf("réutilisation : %v, attendu ErrOTPExpired", err)
		}
	})

	t.Run("cinq essais maximum", func(t *testing.T) {
		in := e.start(t, tuesday9.Add(time.Hour), patient("B", "Deux", "0611000002"), "+33611000002")
		good := in.Code
		in.Code = wrongCode(good)
		for i := range service.OTPMaxAttempts {
			if _, err := e.svc.Book(ctx, in); !errors.Is(err, service.ErrOTPWrong) {
				t.Fatalf("essai %d : %v", i+1, err)
			}
		}
		in.Code = good
		if _, err := e.svc.Book(ctx, in); !errors.Is(err, service.ErrOTPExpired) {
			t.Fatalf("après 5 essais, même le bon code est refusé : %v", err)
		}
	})

	t.Run("expiration à 10 minutes", func(t *testing.T) {
		in := e.start(t, tuesday9.Add(90*time.Minute), patient("C", "Trois", "0611000003"), "+33611000003")
		e.clock.Advance(service.OTPTTL + time.Second)
		defer e.clock.Advance(-(service.OTPTTL + time.Second))
		if _, err := e.svc.Book(ctx, in); !errors.Is(err, service.ErrOTPExpired) {
			t.Fatalf("code expiré : %v", err)
		}
	})

	t.Run("données modifiées après l'envoi du code", func(t *testing.T) {
		in := e.start(t, tuesday9.Add(2*time.Hour), patient("D", "Quatre", "0611000004"), "+33611000004")
		in.Patient.LastName = "Autre"
		if _, err := e.svc.Book(ctx, in); !errors.Is(err, service.ErrOTPExpired) {
			t.Fatalf("nom modifié : %v", err)
		}
		in.Patient.LastName = "Quatre"
		in.Start = in.Start.Add(15 * time.Minute) // un autre créneau valide
		if _, err := e.svc.Book(ctx, in); !errors.Is(err, service.ErrOTPExpired) {
			t.Fatalf("créneau modifié : %v", err)
		}
	})

	t.Run("code d'un autre challenge", func(t *testing.T) {
		a := e.start(t, tuesday9.Add(30*time.Minute), patient("E", "Cinq", "0611000005"), "+33611000005")
		b := e.start(t, tuesday9.Add(60*time.Minute), patient("F", "Six", "0611000006"), "+33611000006")
		a.Code = b.Code // même si, par hasard, égal : l'identifiant sert de sel
		if a.Code == e.otp.code("+33611000005") {
			t.Skip("collision fortuite des deux codes")
		}
		if _, err := e.svc.Book(ctx, a); !errors.Is(err, service.ErrOTPWrong) {
			t.Fatalf("code d'un autre challenge : %v", err)
		}
	})

	t.Run("code stocké haché", func(t *testing.T) {
		code := e.otp.code("+33611000001")
		if n := e.count(t, "SELECT count(*) FROM otp_challenge WHERE code_hash = $1 OR recipient_hash LIKE '%0611000001%'", code); n != 0 {
			t.Error("code ou numéro en clair dans otp_challenge")
		}
	})
}

func wrongCode(good string) string {
	if good == "000000" {
		return "111111"
	}
	return "000000"
}

func TestOTPRateLimitPerRecipient(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	e := newBookingEnv(t, thursday)
	in := e.verifyInput(tuesday9, patient("Alice", "Durand", "0611111111"))
	for i := range service.OTPPerHour {
		if _, err := e.svc.StartVerification(ctx, in); err != nil {
			t.Fatalf("demande %d : %v", i+1, err)
		}
	}
	if _, err := e.svc.StartVerification(ctx, in); !errors.Is(err, service.ErrOTPRateLimited) {
		t.Fatalf("4e demande : %v, attendu ErrOTPRateLimited", err)
	}
	// Un autre numéro n'est pas touché.
	other := e.verifyInput(tuesday9, patient("Bruno", "Martin", "0622222222"))
	if _, err := e.svc.StartVerification(ctx, other); err != nil {
		t.Fatalf("autre numéro : %v", err)
	}
	// Une heure plus tard, le compteur est libéré.
	e.clock.Advance(time.Hour + time.Minute)
	if _, err := e.svc.StartVerification(ctx, in); err != nil {
		t.Fatalf("après une heure : %v", err)
	}
}

func TestStartVerificationValidation(t *testing.T) {
	t.Parallel()
	e := newBookingEnv(t, thursday)
	tests := []struct {
		name    string
		mod     func(*service.VerifyInput)
		wantFld string
	}{
		{"prénom manquant", func(v *service.VerifyInput) { v.Patient.FirstName = " " }, "first_name"},
		{"nom manquant", func(v *service.VerifyInput) { v.Patient.LastName = "" }, "last_name"},
		{"téléphone invalide", func(v *service.VerifyInput) { v.Patient.Phone = "12" }, "phone"},
		{"email invalide", func(v *service.VerifyInput) { v.Patient.Email = "pas-un-email" }, "email"},
		{"aucun contact", func(v *service.VerifyInput) { v.Patient.Phone = "" }, "phone"},
		{"canal email sans email", func(v *service.VerifyInput) { v.Channel = "email" }, "email"},
		{"canal inconnu", func(v *service.VerifyInput) { v.Channel = "pigeon" }, "channel"},
		{"naissance future", func(v *service.VerifyInput) { v.Patient.BirthDate = "2030-01-01" }, "birth_date"},
		{"naissance illisible", func(v *service.VerifyInput) { v.Patient.BirthDate = "12/03/1980" }, "birth_date"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := e.verifyInput(tuesday9, patient("Alice", "Durand", "0611111111"))
			tt.mod(&in)
			_, err := e.svc.StartVerification(context.Background(), in)
			fe := domain.FieldErrors(err)
			found := false
			for _, f := range fe {
				found = found || f.Field == tt.wantFld
			}
			if !found {
				t.Fatalf("erreur = %v, champ %q attendu", err, tt.wantFld)
			}
		})
	}
	if e.otp.sent != 0 {
		t.Errorf("%d codes envoyés pour des saisies invalides", e.otp.sent)
	}
}

func TestSlotsAcrossDST(t *testing.T) {
	t.Parallel()
	// Le dimanche de 9 h à 10 h : même heure murale avant et après le passage à l'heure d'été (29/03/2026).
	march, october := time.Date(2026, 3, 18, 9, 0, 0, 0, time.UTC), time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC)
	e := newBookingEnv(t, march)
	tests := []struct {
		now      time.Time
		day      time.Time
		wantUTC  []time.Time
		describe string
	}{
		{march, time.Date(2026, 3, 22, 0, 0, 0, 0, paris), []time.Time{time.Date(2026, 3, 22, 8, 0, 0, 0, time.UTC), time.Date(2026, 3, 22, 8, 30, 0, 0, time.UTC)}, "CET (UTC+1)"},
		{march, time.Date(2026, 3, 29, 0, 0, 0, 0, paris), []time.Time{time.Date(2026, 3, 29, 7, 0, 0, 0, time.UTC), time.Date(2026, 3, 29, 7, 30, 0, 0, time.UTC)}, "jour du changement, CEST (UTC+2)"},
		{october, time.Date(2026, 10, 25, 0, 0, 0, 0, paris), []time.Time{time.Date(2026, 10, 25, 8, 0, 0, 0, time.UTC), time.Date(2026, 10, 25, 8, 30, 0, 0, time.UTC)}, "retour à l'heure d'hiver, CET"},
	}
	for _, tt := range tests {
		e.svc.Clock = testutil.NewFixedClock(tt.now)
		got, err := e.svc.AvailableSlots(context.Background(), e.pr.ID, e.typ.ID, tt.day, tt.day.AddDate(0, 0, 1))
		if err != nil {
			t.Fatal(err)
		}
		// 15 min de pas : 9 h, 9 h 15, 9 h 30 (le dernier créneau de 30 min finit à 10 h).
		if len(got) != 3 || !got[0].Start.Equal(tt.wantUTC[0]) || !got[2].Start.Equal(tt.wantUTC[1]) {
			t.Errorf("%s : créneaux = %v", tt.describe, got)
		}
		if h := got[0].Start.In(paris).Hour(); h != 9 {
			t.Errorf("%s : heure murale %d h, attendu 9 h", tt.describe, h)
		}
	}
	// Réservation le jour du changement d'heure.
	e.svc.Clock = testutil.NewFixedClock(march)
	sunday := time.Date(2026, 3, 29, 9, 0, 0, 0, paris)
	in := e.start(t, sunday, patient("Alice", "Durand", "0611111111"), "+33611111111")
	res, err := e.svc.Book(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Appointment.During.Lower.Time.Equal(time.Date(2026, 3, 29, 7, 0, 0, 0, time.UTC)) {
		t.Errorf("during = %v", res.Appointment.During)
	}
}

func TestAllowedTypesFollowRules(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := testutil.DB(t)
	pc := testutil.NewPractice(t, db)
	pr := testutil.NewPractitioner(t, db, pc.ID)
	t1 := testutil.NewAppointmentType(t, db, pc.ID)
	t2 := testutil.NewAppointmentType(t, db, pc.ID)
	// Mardi : motif 1 seulement. Dimanche : tous les motifs (NULL).
	addRule(t, db, pr.ID, 1, 9, 12, []uuid.UUID{t1.ID})
	addRule(t, db, pr.ID, 6, 9, 10, nil)
	if _, err := db.Exec(ctx, "UPDATE appointment_type SET online_bookable = false WHERE id = $1", t2.ID); err != nil {
		t.Fatal(err)
	}
	svc := &service.Booking{Pool: db, Clock: testutil.NewFixedClock(thursday), OTP: &captureOTP{}, ManageKey: service.NewManageKey(testutil.ManageSecret)}

	// t2 n'est pas réservable en ligne : refusé même s'il est autorisé par une plage.
	if _, err := svc.AvailableSlots(ctx, pr.ID, t2.ID, tuesday9, tuesday9.Add(time.Hour)); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("motif hors ligne : %v, attendu ErrNotFound", err)
	}
	if _, err := db.Exec(ctx, "UPDATE appointment_type SET online_bookable = true WHERE id = $1", t2.ID); err != nil {
		t.Fatal(err)
	}
	got, err := svc.AvailableSlots(ctx, pr.ID, t2.ID, tuesday9, tuesday9.Add(3*time.Hour))
	if err != nil || len(got) != 0 {
		t.Errorf("t2 le mardi : %v %v, aucun créneau attendu", got, err)
	}
	got, err = svc.AvailableSlots(ctx, pr.ID, t1.ID, tuesday9, tuesday9.Add(3*time.Hour))
	if err != nil || len(got) == 0 {
		t.Errorf("t1 le mardi : %v %v", got, err)
	}
	sunday := time.Date(2026, 10, 25, 0, 0, 0, 0, paris)
	if got, err = svc.AvailableSlots(ctx, pr.ID, t2.ID, sunday, sunday.AddDate(0, 0, 1)); err != nil || len(got) == 0 {
		t.Errorf("t2 le dimanche : %v %v", got, err)
	}
	// Motif désactivé : inconnu côté public.
	if _, err := db.Exec(ctx, "UPDATE appointment_type SET active = false WHERE id = $1", t1.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AvailableSlots(ctx, pr.ID, t1.ID, tuesday9, tuesday9.Add(time.Hour)); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("motif inactif : %v", err)
	}
	page, err := svc.Practitioner(ctx, pc.Slug, pr.Slug)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Types) != 1 || page.Types[0].ID != t2.ID {
		t.Errorf("motifs listés = %v, attendu seulement t2", page.Types)
	}
}

func TestOutboxOTPSender(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	e := newBookingEnv(t, thursday)
	e.svc.OTP = service.OutboxOTPSender{}

	in := e.verifyInput(tuesday9, patient("Alice", "Durand", "06 11 11 11 11"))
	if _, err := e.svc.StartVerification(ctx, in); err != nil {
		t.Fatal(err)
	}
	var kind, tpl, recipient, body string
	if err := e.db.QueryRow(ctx, "SELECT kind, template, recipient, payload->>'body' FROM outbox_message").Scan(&kind, &tpl, &recipient, &body); err != nil {
		t.Fatal(err)
	}
	if kind != "sms" || tpl != "otp" || recipient != "+33611111111" || !strings.Contains(body, e.pc.Name[:5]) {
		t.Errorf("sms: kind=%s template=%s recipient=%s body=%q", kind, tpl, recipient, body)
	}
	if e.count(t, "SELECT count(*) FROM outbox_message WHERE (payload->>'otp')::boolean") != 1 {
		t.Error("le SMS doit porter otp=true (exempt d'opt-out et de plage silencieuse)")
	}

	in.Channel, in.Patient.Email = "email", "alice@example.test"
	if _, err := e.svc.StartVerification(ctx, in); err != nil {
		t.Fatal(err)
	}
	var code string
	if err := e.db.QueryRow(ctx, "SELECT payload->>'code' FROM outbox_message WHERE kind = 'email' AND template = 'otp' AND recipient = 'alice@example.test'").Scan(&code); err != nil {
		t.Fatal(err)
	}
	if len(code) != 6 {
		t.Errorf("code email = %q", code)
	}
}

func TestConfirmationByToken(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	e := newBookingEnv(t, thursday)
	res, err := e.svc.Book(ctx, e.start(t, tuesday9, patient("Alice", "Durand", "0611111111"), "+33611111111"))
	if err != nil {
		t.Fatal(err)
	}
	c, err := e.svc.Confirmation(ctx, res.Token)
	if err != nil {
		t.Fatal(err)
	}
	if c.FirstName != "Alice" || !c.Start.Equal(tuesday9) || c.Start.Location().String() != "Europe/Paris" || c.Appointment.ID != res.Appointment.ID {
		t.Errorf("confirmation = %+v", c)
	}
	for _, bad := range []string{"", "inconnu", res.Token + "x", strings.Repeat("a", 200)} {
		if _, err := e.svc.Confirmation(ctx, bad); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("jeton %q : %v, attendu ErrNotFound", bad, err)
		}
	}
}
