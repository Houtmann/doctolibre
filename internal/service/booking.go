package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
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
	// Events reçoit les faits métier après commit (nil = NopSink). Les messages aux
	// patients ne passent pas par lui : ils sont enfilés dans la transaction métier.
	Events EventSink
	// BaseURL : adresse publique (DOCTOLIBRE_BASE_URL) pour les liens /r/{jeton} des
	// messages ; vide = messages sans lien.
	BaseURL string
	// ManageKey dérive les jetons des liens /r/{jeton} (obligatoire).
	ManageKey ManageKey
	// OnCreated est appelé (source "online") après le commit d'un rendez-vous ; nil = rien.
	OnCreated func(source string)
}

func (b *Booking) notifier() notifier {
	return notifier{clock: b.Clock, baseURL: b.BaseURL, key: b.ManageKey}
}

// PublicPractice est la page d'accueil publique d'un cabinet.
type PublicPractice struct {
	Practice      sqlcgen.Practice
	Practitioners []sqlcgen.Practitioner
	// Channels : canaux de vérification actifs ; aucun => réservation en ligne fermée (T086).
	Channels Channels
}

// PractitionerPage est la page publique d'un praticien et ses motifs réservables.
type PractitionerPage struct {
	Practice     sqlcgen.Practice
	Practitioner sqlcgen.Practitioner
	Types        []sqlcgen.AppointmentType
	// Channels : canaux de vérification actifs ; aucun => réservation en ligne fermée (T086).
	Channels Channels
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
	// Channels : canaux de vérification actifs (au moins un, sinon Target échoue avec BookingClosedError).
	Channels Channels
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
	ch, err := channelsOf(ctx, q, pc.ID)
	if err != nil {
		return PublicPractice{}, err
	}
	return PublicPractice{Practice: pc, Practitioners: prs, Channels: ch}, nil
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
	if out.Channels, err = channelsOf(ctx, q, pc.ID); err != nil {
		return PractitionerPage{}, err
	}
	for _, t := range types {
		if t.AtHome && !homeVisitsOnline(pc) {
			continue
		}
		for _, r := range rules {
			if ruleAllows(r.AppointmentTypeIds, t.ID) {
				out.Types = append(out.Types, t)
				break
			}
		}
	}
	return out, nil
}

// homeVisitsOnline : la réservation en ligne à domicile exige l'option du cabinet
// et au moins une zone desservie.
func homeVisitsOnline(pc sqlcgen.Practice) bool {
	return pc.HomeVisitOnline && len(pc.HomeVisitZones) > 0
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
	if !page.Channels.Any() {
		return Target{}, BookingClosedError{Phone: page.Practice.Phone}
	}
	for _, t := range page.Types {
		if t.ID == typeID {
			return Target{
				Practice: page.Practice, Practitioner: page.Practitioner, Type: t, Loc: loc, Channels: page.Channels,
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
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && (!typ.Active || !typ.OnlineBookable || (typ.AtHome && !homeVisitsOnline(pc)))) {
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

// buffers retourne les marges effectives du motif : trajet compris pour une
// visite à domicile (même règle que celle copiée sur le RDV, donc que la contrainte EXCLUDE).
func (s slotContext) buffers() (before, after time.Duration) {
	return domain.HomeBuffers(s.typ.AtHome,
		microsToDuration(intervalMicros(s.typ.BufferBefore)), microsToDuration(intervalMicros(s.typ.BufferAfter)),
		microsToDuration(intervalMicros(s.typ.TravelBuffer)))
}

// checkAddress valide l'adresse d'une visite à domicile : obligatoire et dans
// une zone desservie. Hors visite à domicile, l'adresse est ignorée.
func (s slotContext) checkAddress(d details) error {
	if !s.typ.AtHome {
		return nil
	}
	switch {
	case d.address == "":
		return domain.ErrValidation{Field: "address", Msg: "L'adresse de la visite à domicile est requise."}
	case !domain.InHomeZone(d.address, s.pc.HomeVisitZones):
		return domain.ErrValidation{Field: "address", Msg: "Cette adresse est hors de la zone desservie à domicile. Indiquez le code postal ou la commune, ou contactez le cabinet."}
	}
	return nil
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
	if s.pc.CloseOnHolidays {
		// Jours fériés : fermetures sur la journée entière du cabinet, sauf si
		// une ouverture exceptionnelle explicite recoupe le jour (Closed l'emporterait).
		var opens []booking.Interval
		for _, x := range ex {
			if x.Kind == booking.Open {
				opens = append(opens, booking.Interval{Start: x.Start, End: x.End})
			}
		}
		ex = append(ex, booking.HolidayClosures(s.loc, lo, hi, s.pc.HolidaysAlsaceMoselle, opens)...)
	}
	busy := make([]booking.Busy, 0, len(appts))
	for _, a := range appts {
		if a.ID == s.ignore {
			continue
		}
		busy = append(busy, booking.Busy{Start: a.DuringBlocked.Lower.Time, End: a.DuringBlocked.Upper.Time})
	}
	// Ressource partagée du motif (T060) : occupée par n'importe quel praticien.
	rbusy, err := q.ListResourceBusyForType(ctx, sqlcgen.ListResourceBusyForTypeParams{
		PracticeID: s.pc.ID, AppointmentTypeID: s.typ.ID, FromAt: lo, ToAt: hi})
	if err != nil {
		return nil, fmt.Errorf("occupations de la ressource: %w", err)
	}
	resBusy := make([]booking.Busy, 0, len(rbusy))
	for _, r := range rbusy {
		if r.AppointmentID == s.ignore {
			continue
		}
		resBusy = append(resBusy, booking.Busy{Start: r.DuringBlocked.Lower.Time, End: r.DuringBlocked.Upper.Time})
	}
	bb, ba := s.buffers()
	return booking.Slots(booking.Request{
		Loc: s.loc, From: from, To: to, Now: now,
		MinNotice:    microsToDuration(intervalMicros(s.pc.MinBookingNotice)),
		Horizon:      microsToDuration(intervalMicros(s.pc.BookingHorizon)),
		Duration:     microsToDuration(intervalMicros(s.typ.Duration)),
		BufferBefore: bb,
		BufferAfter:  ba,
		TypeID:       s.typ.ID,
		ResourceBusy: resBusy,
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
	if err := sc.checkAddress(d); err != nil {
		return Verification{}, err
	}
	if err := checkOTPChannel(ctx, q, sc.pc, in.Channel); err != nil {
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

	apptID, token, tokenHash, err := b.ManageKey.mint()
	if err != nil {
		return BookResult{}, err
	}
	var appt sqlcgen.Appointment
	err = store.WithTx(ctx, b.Pool, func(q *sqlcgen.Queries, tx pgx.Tx) error {
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
		if err := sc.checkAddress(d); err != nil {
			return err
		}
		slot, err := sc.check(ctx, q, now, in.Start)
		if err != nil {
			return err
		}
		if !sc.typ.AtHome {
			d.address = ""
		}
		patient, err := findOrCreatePatient(ctx, q, sc.pc.ID, d)
		if err != nil {
			return err
		}
		if blocked, err := onlineBookingBlocked(ctx, q, sc.pc, patient.ID, now); err != nil {
			return err
		} else if blocked {
			slog.InfoContext(ctx, "réservation en ligne refusée (absences répétées)",
				slog.String("practice_id", sc.pc.ID.String()), slog.String("patient_id", patient.ID.String()))
			return domain.ErrOnlineBookingBlocked
		}
		bb, ba := sc.buffers()
		appt, err = store.CreateAppointment(ctx, q, store.NewAppointment{
			ID: apptID, PracticeID: sc.pc.ID, PractitionerID: sc.pr.ID, PatientID: patient.ID, AppointmentTypeID: sc.typ.ID,
			Start: slot.Start, End: slot.End,
			BufferBefore:    bb,
			BufferAfter:     ba,
			Address:         d.address,
			Source:          "online",
			ManageTokenHash: tokenHash,
			// Seul le contact du canal du code est vérifié : les messages n'iront que là.
			VerifiedChannel: in.Channel,
		})
		if err != nil {
			return err
		}
		return b.notifier().booked(ctx, tx, q, appt, token)
	})
	if err != nil {
		return BookResult{}, err
	}
	if b.OnCreated != nil {
		b.OnCreated("online")
	}
	b.emit(ctx, Event{Kind: EventAppointmentCreated, PracticeID: appt.PracticeID, AppointmentID: appt.ID})
	return BookResult{Appointment: appt, Token: token}, nil
}

// ManageTokenHash est le haché (sha256 hex) stocké dans appointment.manage_token_hash.
func ManageTokenHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// newManageToken tire un jeton aléatoire (liens de proposition de la liste d'attente) ;
// les liens de gestion des rendez-vous, eux, sont dérivés (ManageKey).
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

// NoShowWindowMonths est la fenêtre fixe de comptage des absences.
const NoShowWindowMonths = 6

// noShowSince est le début de la fenêtre : un RDV manqué commençant exactement à
// cet instant compte (borne incluse), un plus ancien est ignoré.
func noShowSince(now time.Time) time.Time { return now.AddDate(0, -NoShowWindowMonths, 0) }

// onlineBookingBlocked indique si le patient a atteint le seuil d'absences du
// cabinet (seuil NULL = jamais bloqué).
func onlineBookingBlocked(ctx context.Context, q *sqlcgen.Queries, pc sqlcgen.Practice, patientID uuid.UUID, now time.Time) (bool, error) {
	if !pc.NoShowBlockThreshold.Valid {
		return false, nil
	}
	n, err := q.CountPatientNoShowsSince(ctx, sqlcgen.CountPatientNoShowsSinceParams{
		PatientID: patientID, PracticeID: pc.ID, Since: noShowSince(now),
	})
	if err != nil {
		return false, fmt.Errorf("comptage des absences: %w", err)
	}
	return n >= int32(pc.NoShowBlockThreshold.Int16), nil
}
