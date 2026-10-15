package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"doctolibre/internal/booking"
	"doctolibre/internal/domain"
	"doctolibre/internal/store"
	"doctolibre/internal/store/sqlcgen"
)

// Booking est le service de réservation en ligne (côté patient, sans compte).
type Booking struct {
	Pool  *pgxpool.Pool
	Clock domain.Clock
	OTP   OTPSender
	// Events reçoit les faits métier (nil = NopSink) ; les notifications sont branchées en T023.
	Events EventSink
}

// PublicPractice est la page d'accueil publique d'un cabinet.
type PublicPractice struct {
	Practice      sqlcgen.Practice
	Practitioners []sqlcgen.Practitioner
}

// PractitionerPage est la page publique d'un praticien et ses motifs réservables.
type PractitionerPage struct {
	Practice     sqlcgen.Practice
	Practitioner sqlcgen.Practitioner
	Types        []sqlcgen.AppointmentType
}

// Target est la cible validée d'une réservation : praticien actif, motif
// réservable en ligne et autorisé par au moins une plage, fuseau du cabinet.
type Target struct {
	Practice     sqlcgen.Practice
	Practitioner sqlcgen.Practitioner
	Type         sqlcgen.AppointmentType
	Loc          *time.Location
	MinNotice    time.Duration
	Horizon      time.Duration
}

// Practice retourne le cabinet public (par slug) et ses praticiens actifs.
func (b *Booking) Practice(ctx context.Context, slug string) (PublicPractice, error) {
	q := sqlcgen.New(b.Pool)
	pc, err := q.GetPracticeBySlug(ctx, slug)
	if errors.Is(err, pgx.ErrNoRows) {
		return PublicPractice{}, domain.ErrNotFound
	}
	if err != nil {
		return PublicPractice{}, fmt.Errorf("lecture cabinet: %w", err)
	}
	prs, err := q.ListPractitioners(ctx, sqlcgen.ListPractitionersParams{PracticeID: pc.ID, PageSize: pageSize})
	if err != nil {
		return PublicPractice{}, fmt.Errorf("liste des praticiens: %w", err)
	}
	return PublicPractice{Practice: pc, Practitioners: prs}, nil
}

// Practitioner retourne la page publique d'un praticien. Un motif n'est listé
// que s'il est actif, réservable en ligne et autorisé par au moins une plage
// (availability_rule.appointment_type_ids NULL = tous les motifs).
func (b *Booking) Practitioner(ctx context.Context, practiceSlug, practitionerSlug string) (PractitionerPage, error) {
	q := sqlcgen.New(b.Pool)
	pc, err := q.GetPracticeBySlug(ctx, practiceSlug)
	if errors.Is(err, pgx.ErrNoRows) {
		return PractitionerPage{}, domain.ErrNotFound
	}
	if err != nil {
		return PractitionerPage{}, fmt.Errorf("lecture cabinet: %w", err)
	}
	pr, err := q.GetPractitionerBySlug(ctx, sqlcgen.GetPractitionerBySlugParams{PracticeID: pc.ID, Slug: practitionerSlug})
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && !pr.Active) {
		return PractitionerPage{}, domain.ErrNotFound
	}
	if err != nil {
		return PractitionerPage{}, fmt.Errorf("lecture praticien: %w", err)
	}
	types, err := q.ListBookableAppointmentTypes(ctx, sqlcgen.ListBookableAppointmentTypesParams{PracticeID: pc.ID, PageSize: pageSize})
	if err != nil {
		return PractitionerPage{}, fmt.Errorf("liste des motifs: %w", err)
	}
	rules, err := q.ListAvailabilityRules(ctx, pr.ID)
	if err != nil {
		return PractitionerPage{}, fmt.Errorf("liste des plages: %w", err)
	}
	out := PractitionerPage{Practice: pc, Practitioner: pr}
	for _, t := range types {
		for _, r := range rules {
			if ruleAllows(r.AppointmentTypeIds, t.ID) {
				out.Types = append(out.Types, t)
				break
			}
		}
	}
	return out, nil
}

func ruleAllows(ids []uuid.UUID, id uuid.UUID) bool {
	if len(ids) == 0 {
		return true
	}
	for _, x := range ids {
		if x == id {
			return true
		}
	}
	return false
}

// Target valide (cabinet, praticien, motif) pour une réservation.
func (b *Booking) Target(ctx context.Context, practiceSlug, practitionerSlug string, typeID uuid.UUID) (Target, error) {
	page, err := b.Practitioner(ctx, practiceSlug, practitionerSlug)
	if err != nil {
		return Target{}, err
	}
	loc, err := time.LoadLocation(page.Practice.Timezone)
	if err != nil {
		return Target{}, fmt.Errorf("fuseau %q: %w", page.Practice.Timezone, err)
	}
	for _, t := range page.Types {
		if t.ID == typeID {
			return Target{
				Practice: page.Practice, Practitioner: page.Practitioner, Type: t, Loc: loc,
				MinNotice: time.Duration(intervalMicros(page.Practice.MinBookingNotice)) * time.Microsecond,
				Horizon:   time.Duration(intervalMicros(page.Practice.BookingHorizon)) * time.Microsecond,
			}, nil
		}
	}
	return Target{}, domain.ErrNotFound
}

// slotContext regroupe ce qu'il faut pour calculer ou revalider des créneaux.
type slotContext struct {
	pr  sqlcgen.Practitioner
	pc  sqlcgen.Practice
	typ sqlcgen.AppointmentType
	loc *time.Location
	// ignore : rendez-vous déplacé, qui ne doit pas bloquer ses propres créneaux.
	ignore uuid.UUID
}

func loadSlotContext(ctx context.Context, q *sqlcgen.Queries, practitionerID, typeID uuid.UUID) (slotContext, error) {
	pr, err := q.GetActivePractitioner(ctx, practitionerID)
	if errors.Is(err, pgx.ErrNoRows) {
		return slotContext{}, domain.ErrNotFound
	}
	if err != nil {
		return slotContext{}, fmt.Errorf("lecture praticien: %w", err)
	}
	pc, err := q.GetPractice(ctx, pr.PracticeID)
	if err != nil {
		return slotContext{}, fmt.Errorf("lecture cabinet: %w", err)
	}
	typ, err := q.GetAppointmentType(ctx, sqlcgen.GetAppointmentTypeParams{ID: typeID, PracticeID: pr.PracticeID})
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && (!typ.Active || !typ.OnlineBookable)) {
		return slotContext{}, domain.ErrNotFound
	}
	if err != nil {
		return slotContext{}, fmt.Errorf("lecture motif: %w", err)
	}
	loc, err := time.LoadLocation(pc.Timezone)
	if err != nil {
		return slotContext{}, fmt.Errorf("fuseau %q: %w", pc.Timezone, err)
	}
	return slotContext{pr: pr, pc: pc, typ: typ, loc: loc}, nil
}

// slots charge règles, exceptions et rendez-vous puis appelle le moteur pur
// booking.Slots : mêmes règles que la contrainte EXCLUDE (buffers copiés du motif).
func (s slotContext) slots(ctx context.Context, q *sqlcgen.Queries, now, from, to time.Time) ([]booking.Slot, error) {
	rules, err := q.ListAvailabilityRules(ctx, s.pr.ID)
	if err != nil {
		return nil, fmt.Errorf("liste des plages: %w", err)
	}
	// Les buffers et les absences débordent des créneaux : fenêtre élargie d'un jour.
	lo, hi := from.AddDate(0, 0, -1), to.AddDate(0, 0, 1)
	excs, err := q.ListAvailabilityExceptions(ctx, sqlcgen.ListAvailabilityExceptionsParams{PractitionerID: s.pr.ID, FromAt: lo, ToAt: hi})
	if err != nil {
		return nil, fmt.Errorf("liste des absences: %w", err)
	}
	appts, err := q.ListAppointmentsForPractitioner(ctx, sqlcgen.ListAppointmentsForPractitionerParams{PractitionerID: s.pr.ID, FromAt: lo, ToAt: hi})
	if err != nil {
		return nil, fmt.Errorf("liste des rendez-vous: %w", err)
	}

	wr := make([]booking.WeeklyRule, 0, len(rules))
	for _, r := range rules {
		w := booking.WeeklyRule{
			Weekday: time.Weekday((int(r.Weekday) + 1) % 7), // 0 = lundi en base, 0 = dimanche en Go
			Start:   booking.LocalTime(r.StartTime.Microseconds / int64(time.Minute/time.Microsecond)),
			End:     booking.LocalTime(r.EndTime.Microseconds / int64(time.Minute/time.Microsecond)),
			TypeIDs: r.AppointmentTypeIds,
		}
		if r.ValidFrom.Valid {
			d := booking.DateOf(r.ValidFrom.Time)
			w.ValidFrom = &d
		}
		if r.ValidUntil.Valid {
			d := booking.DateOf(r.ValidUntil.Time)
			w.ValidUntil = &d
		}
		wr = append(wr, w)
	}
	ex := make([]booking.Exception, 0, len(excs))
	for _, e := range excs {
		kind := booking.Closed
		if e.Kind == "open" {
			kind = booking.Open
		}
		ex = append(ex, booking.Exception{Start: e.During.Lower.Time, End: e.During.Upper.Time, Kind: kind})
	}
	busy := make([]booking.Busy, 0, len(appts))
	for _, a := range appts {
		if a.ID == s.ignore {
			continue
		}
		busy = append(busy, booking.Busy{Start: a.DuringBlocked.Lower.Time, End: a.DuringBlocked.Upper.Time})
	}
	return booking.Slots(booking.Request{
		Loc: s.loc, From: from, To: to, Now: now,
		MinNotice:    microsToDuration(intervalMicros(s.pc.MinBookingNotice)),
		Horizon:      microsToDuration(intervalMicros(s.pc.BookingHorizon)),
		Duration:     microsToDuration(intervalMicros(s.typ.Duration)),
		BufferBefore: microsToDuration(intervalMicros(s.typ.BufferBefore)),
		BufferAfter:  microsToDuration(intervalMicros(s.typ.BufferAfter)),
		TypeID:       s.typ.ID,
	}, wr, ex, busy), nil
}

func microsToDuration(us int64) time.Duration { return time.Duration(us) * time.Microsecond }

// check vérifie qu'un créneau commençant exactement à start est réservable.
func (s slotContext) check(ctx context.Context, q *sqlcgen.Queries, now, start time.Time) (booking.Slot, error) {
	got, err := s.slots(ctx, q, now, start, start.Add(time.Second))
	if err != nil {
		return booking.Slot{}, err
	}
	for _, sl := range got {
		if sl.Start.Equal(start) {
			return sl, nil
		}
	}
	return booking.Slot{}, domain.ErrSlotTaken
}

// AvailableSlots retourne les créneaux réservables en ligne dont le début est
// dans [from, to), pour un praticien et un motif.
func (b *Booking) AvailableSlots(ctx context.Context, practitionerID, typeID uuid.UUID, from, to time.Time) ([]booking.Slot, error) {
	q := sqlcgen.New(b.Pool)
	sc, err := loadSlotContext(ctx, q, practitionerID, typeID)
	if err != nil {
		return nil, err
	}
	return sc.slots(ctx, q, b.Clock.Now(), from, to)
}

// CheckSlot retourne domain.ErrSlotTaken si start n'est pas un créneau réservable.
func (b *Booking) CheckSlot(ctx context.Context, practitionerID, typeID uuid.UUID, start time.Time) error {
	q := sqlcgen.New(b.Pool)
	sc, err := loadSlotContext(ctx, q, practitionerID, typeID)
	if err != nil {
		return err
	}
	_, err = sc.check(ctx, q, b.Clock.Now(), start)
	return err
}

// VerifyInput décrit une demande de réservation avant vérification d'identité.
type VerifyInput struct {
	PractitionerID uuid.UUID
	TypeID         uuid.UUID
	Start          time.Time
	Patient        PatientDetails
	Channel        string // "sms" | "email" : où envoyer le code
}

// Verification est le résultat de StartVerification.
type Verification struct {
	ChallengeID uuid.UUID
	Channel     string
}

// StartVerification valide la saisie, revalide le créneau puis envoie un code
// à 6 chiffres. Limite : OTPPerHour codes par heure et par destinataire
// (ErrOTPRateLimited). Les erreurs de saisie sont des domain.ValidationErrors ;
// un créneau indisponible ou forgé donne domain.ErrSlotTaken.
func (b *Booking) StartVerification(ctx context.Context, in VerifyInput) (Verification, error) {
	now := b.Clock.Now()
	d, err := parseDetails(in.Patient, in.Channel, now)
	if err != nil {
		return Verification{}, err
	}
	q := sqlcgen.New(b.Pool)
	sc, err := loadSlotContext(ctx, q, in.PractitionerID, in.TypeID)
	if err != nil {
		return Verification{}, err
	}
	if _, err := sc.check(ctx, q, now, in.Start); err != nil {
		return Verification{}, err
	}
	code, err := newOTPCode()
	if err != nil {
		return Verification{}, err
	}
	recipient := d.recipient(in.Channel)
	recipientHash := sha256Hex(sc.pc.ID.String(), in.Channel, recipient)

	var id uuid.UUID
	err = store.WithTx(ctx, b.Pool, func(q *sqlcgen.Queries, tx pgx.Tx) error {
		// Sérialise les demandes pour un même destinataire : le comptage est exact.
		if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1, 0))", recipientHash); err != nil {
			return fmt.Errorf("verrou OTP: %w", err)
		}
		if err := q.DeleteOldOTPChallenges(ctx, now.Add(-time.Hour)); err != nil {
			return fmt.Errorf("purge OTP: %w", err)
		}
		n, err := q.CountRecentOTPChallenges(ctx, sqlcgen.CountRecentOTPChallengesParams{RecipientHash: recipientHash, Since: now.Add(-time.Hour)})
		if err != nil {
			return fmt.Errorf("comptage OTP: %w", err)
		}
		if n >= OTPPerHour {
			return ErrOTPRateLimited
		}
		// L'identifiant sert de sel au haché du code : on le tire avant l'insertion.
		id = uuid.New()
		if _, err := q.CreateOTPChallenge(ctx, sqlcgen.CreateOTPChallengeParams{
			ID: id, PracticeID: sc.pc.ID, Channel: in.Channel, RecipientHash: recipientHash,
			BindingHash: d.binding(in.PractitionerID, in.TypeID, in.Start, in.Channel),
			CodeHash:    otpHash(id, code), ExpiresAt: now.Add(OTPTTL), CreatedAt: now,
		}); err != nil {
			return fmt.Errorf("création OTP: %w", err)
		}
		return b.OTP.SendOTP(ctx, tx, sc.pc.ID, sc.pc.Name, in.Channel, recipient, code)
	})
	if err != nil {
		return Verification{}, err
	}
	return Verification{ChallengeID: id, Channel: in.Channel}, nil
}

// BookInput décrit une réservation à confirmer.
type BookInput struct {
	VerifyInput
	ChallengeID uuid.UUID
	Code        string
}

// BookResult est le rendez-vous créé. Token est le secret du lien de gestion
// (jamais stocké en clair : seul son sha256 l'est).
type BookResult struct {
	Appointment sqlcgen.Appointment
	Token       string
}

// Book vérifie le code puis réserve : le créneau est revalidé côté serveur (on ne
// fait jamais confiance au start reçu), le patient est retrouvé ou créé, et le
// rendez-vous est inséré dans la même transaction. Un chevauchement détecté par
// la contrainte EXCLUDE devient domain.ErrSlotTaken.
//
// Erreurs : ErrOTPWrong (réessayer), ErrOTPExpired (redemander un code),
// domain.ErrSlotTaken, domain.ValidationErrors.
func (b *Booking) Book(ctx context.Context, in BookInput) (BookResult, error) {
	now := b.Clock.Now()
	d, err := parseDetails(in.Patient, in.Channel, now)
	if err != nil {
		return BookResult{}, err
	}
	// 1. Le code. Chaque essai est compté et validé (commit) indépendamment de la suite.
	q := sqlcgen.New(b.Pool)
	ch, err := q.TryOTPChallenge(ctx, sqlcgen.TryOTPChallengeParams{ID: in.ChallengeID, Now: now, MaxAttempts: OTPMaxAttempts})
	if errors.Is(err, pgx.ErrNoRows) {
		return BookResult{}, ErrOTPExpired
	}
	if err != nil {
		return BookResult{}, fmt.Errorf("lecture OTP: %w", err)
	}
	if ch.BindingHash != d.binding(in.PractitionerID, in.TypeID, in.Start, in.Channel) {
		return BookResult{}, ErrOTPExpired // données modifiées depuis l'envoi du code
	}
	if !otpMatches(ch.CodeHash, in.ChallengeID, in.Code) {
		return BookResult{}, ErrOTPWrong
	}

	token, err := newManageToken()
	if err != nil {
		return BookResult{}, err
	}
	var appt sqlcgen.Appointment
	err = store.WithTx(ctx, b.Pool, func(q *sqlcgen.Queries, _ pgx.Tx) error {
		// 2. Le code ne sert qu'une fois (annulé avec la transaction si la suite échoue).
		n, err := q.ConsumeOTPChallenge(ctx, sqlcgen.ConsumeOTPChallengeParams{ID: in.ChallengeID, Now: now})
		if err != nil {
			return fmt.Errorf("consommation OTP: %w", err)
		}
		if n == 0 {
			return ErrOTPExpired
		}
		// 3. Revalidation du créneau, sur l'état lu dans la transaction.
		sc, err := loadSlotContext(ctx, q, in.PractitionerID, in.TypeID)
		if err != nil {
			return err
		}
		if sc.pc.ID != ch.PracticeID {
			return ErrOTPExpired
		}
		slot, err := sc.check(ctx, q, now, in.Start)
		if err != nil {
			return err
		}
		patient, err := findOrCreatePatient(ctx, q, sc.pc.ID, d)
		if err != nil {
			return err
		}
		appt, err = store.CreateAppointment(ctx, q, store.NewAppointment{
			PracticeID: sc.pc.ID, PractitionerID: sc.pr.ID, PatientID: patient.ID, AppointmentTypeID: sc.typ.ID,
			Start: slot.Start, End: slot.End,
			BufferBefore:    microsToDuration(intervalMicros(sc.typ.BufferBefore)),
			BufferAfter:     microsToDuration(intervalMicros(sc.typ.BufferAfter)),
			Source:          "online",
			ManageTokenHash: ManageTokenHash(token),
		})
		return err
	})
	if err != nil {
		return BookResult{}, err
	}
	b.emit(ctx, Event{Kind: EventAppointmentCreated, PracticeID: appt.PracticeID, AppointmentID: appt.ID})
	return BookResult{Appointment: appt, Token: token}, nil
}

// ManageTokenHash est le haché (sha256 hex) stocké dans appointment.manage_token_hash.
func ManageTokenHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func newManageToken() (string, error) {
	var buf [32]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", fmt.Errorf("jeton de gestion: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf[:]), nil
}

// Confirmation est le récapitulatif d'un rendez-vous retrouvé par son lien.
type Confirmation struct {
	Appointment  sqlcgen.Appointment
	Practice     sqlcgen.Practice
	Practitioner sqlcgen.Practitioner
	Type         sqlcgen.AppointmentType
	FirstName    string // prénom du patient (seule donnée personnelle exposée)
	Start, End   time.Time
	Loc          *time.Location
	// Changeable : le patient peut encore annuler ou déplacer (RDV à venir, délai du cabinet respecté).
	Changeable bool
	// MinCancel est le délai minimal avant le RDV ; Horizon la fenêtre de réservation.
	MinCancel, Horizon time.Duration
}

// Confirmation retrouve un rendez-vous par le secret de son lien de gestion.
// Jeton inconnu : domain.ErrNotFound (aucune information sur l'existence).
func (b *Booking) Confirmation(ctx context.Context, token string) (Confirmation, error) {
	if token == "" || len(token) > 128 {
		return Confirmation{}, domain.ErrNotFound
	}
	q := sqlcgen.New(b.Pool)
	a, err := q.GetAppointmentByManageToken(ctx, pgtype.Text{String: ManageTokenHash(token), Valid: true})
	if errors.Is(err, pgx.ErrNoRows) {
		return Confirmation{}, domain.ErrNotFound
	}
	if err != nil {
		return Confirmation{}, fmt.Errorf("lecture rendez-vous: %w", err)
	}
	pc, err := q.GetPractice(ctx, a.PracticeID)
	if err != nil {
		return Confirmation{}, fmt.Errorf("lecture cabinet: %w", err)
	}
	pr, err := q.GetPractitioner(ctx, sqlcgen.GetPractitionerParams{ID: a.PractitionerID, PracticeID: a.PracticeID})
	if err != nil {
		return Confirmation{}, fmt.Errorf("lecture praticien: %w", err)
	}
	typ, err := q.GetAppointmentType(ctx, sqlcgen.GetAppointmentTypeParams{ID: a.AppointmentTypeID, PracticeID: a.PracticeID})
	if err != nil {
		return Confirmation{}, fmt.Errorf("lecture motif: %w", err)
	}
	pat, err := q.GetPatient(ctx, sqlcgen.GetPatientParams{ID: a.PatientID, PracticeID: a.PracticeID})
	if err != nil {
		return Confirmation{}, fmt.Errorf("lecture patient: %w", err)
	}
	loc, err := time.LoadLocation(pc.Timezone)
	if err != nil {
		return Confirmation{}, fmt.Errorf("fuseau %q: %w", pc.Timezone, err)
	}
	minCancel := microsToDuration(intervalMicros(pc.MinCancelNotice))
	start := a.During.Lower.Time
	return Confirmation{
		Appointment: a, Practice: pc, Practitioner: pr, Type: typ, FirstName: pat.FirstName, Loc: loc,
		Start: start.In(loc), End: a.During.Upper.Time.In(loc),
		Changeable: a.Status == "booked" && start.Sub(b.Clock.Now()) >= minCancel,
		MinCancel:  minCancel, Horizon: microsToDuration(intervalMicros(pc.BookingHorizon)),
	}, nil
}

// Now retourne l'instant courant de l'horloge du service (pour les handlers).
func (b *Booking) Now() time.Time { return b.Clock.Now() }
