package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"doctolibre/internal/domain"
	"doctolibre/internal/store"
	"doctolibre/internal/store/sqlcgen"
)

// Paramètres de la liste d'attente.
const (
	// DefaultWaitlistEvery : période de la tâche d'expiration des inscriptions.
	DefaultWaitlistEvery = time.Hour
	// maxActiveWaitlist borne les inscriptions actives d'un même patient.
	maxActiveWaitlist = 5
	// maxWaitlistSpan : durée maximale de la fenêtre souhaitée.
	maxWaitlistSpan = 366
)

// ErrOfferExpired : la proposition n'est plus valable (délai écoulé, déjà
// utilisée ou inscription close). Le créneau pris par un autre est domain.ErrSlotTaken.
var ErrOfferExpired = errors.New("proposition expirée")

// Waitlist gère la liste d'attente : inscription (patient avec code de
// vérification, ou cabinet), propositions de créneaux libérés (enfilées par les
// annulations, cf. waitlist_offers.go) et réservation par lien /w/{jeton}.
type Waitlist struct {
	Pool  *pgxpool.Pool
	Clock domain.Clock
	// OTP envoie le code de vérification de l'inscription en ligne.
	OTP OTPSender
	// BaseURL : adresse publique pour le lien de gestion du rendez-vous obtenu.
	BaseURL string
	// ManageKey dérive les jetons des liens de gestion des RDV obtenus (obligatoire).
	ManageKey ManageKey
	// Logger de la tâche d'expiration (nil = slog.Default()).
	Logger *slog.Logger
	// Every : période de Run (défaut DefaultWaitlistEvery).
	Every time.Duration
}

func (w *Waitlist) notifier() notifier {
	return notifier{clock: w.Clock, baseURL: w.BaseURL, key: w.ManageKey}
}

// WindowInput est la saisie brute des créneaux souhaités.
type WindowInput struct {
	PractitionerID uuid.UUID // uuid.Nil = tous les praticiens
	TypeID         uuid.UUID
	Earliest       string   // AAAA-MM-JJ
	Latest         string   // AAAA-MM-JJ
	Weekdays       []string // "0" (lundi) à "6" (dimanche) ; vide = tous
	From, To       string   // HH:MM, début de séance souhaité ; vides = toute la journée
}

// window est la saisie validée.
type window struct {
	practitioner     pgtype.UUID
	typeID           uuid.UUID
	earliest, latest pgtype.Date
	weekdays         []int16
	from, to         pgtype.Time
}

func waitlistClock(s string) (pgtype.Time, bool) {
	t, err := time.Parse("15:04", s)
	if err != nil {
		return pgtype.Time{}, false
	}
	return pgtype.Time{Microseconds: int64(t.Hour()*60+t.Minute()) * int64(time.Minute/time.Microsecond), Valid: true}, true
}

// parseWindow valide la fenêtre souhaitée, dates en jours du cabinet (loc).
func parseWindow(in WindowInput, now time.Time, loc *time.Location) (window, error) {
	var errs domain.ValidationErrors
	add := func(field, msg string) { errs = append(errs, domain.ErrValidation{Field: field, Msg: msg}) }
	w := window{typeID: in.TypeID, practitioner: pgtype.UUID{Bytes: in.PractitionerID, Valid: in.PractitionerID != uuid.Nil}}

	ln := now.In(loc)
	today := time.Date(ln.Year(), ln.Month(), ln.Day(), 0, 0, 0, 0, time.UTC)
	from, errFrom := time.Parse("2006-01-02", strings.TrimSpace(in.Earliest))
	to, errTo := time.Parse("2006-01-02", strings.TrimSpace(in.Latest))
	switch {
	case errFrom != nil:
		add("earliest", "Indiquez la première date qui vous convient.")
	case from.Before(today):
		add("earliest", "La première date ne peut pas être dans le passé.")
	}
	switch {
	case errTo != nil:
		add("latest", "Indiquez la dernière date qui vous convient.")
	case errFrom == nil && to.Before(from):
		add("latest", "La dernière date doit suivre la première.")
	case to.After(today.AddDate(0, 0, maxWaitlistSpan)):
		add("latest", "La dernière date est trop lointaine (un an maximum).")
	}
	if errFrom == nil && errTo == nil {
		w.earliest = pgtype.Date{Time: from, Valid: true}
		w.latest = pgtype.Date{Time: to, Valid: true}
	}

	seen := map[int16]bool{}
	for _, s := range in.Weekdays {
		n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 16)
		if err != nil || n < 0 || n > 6 {
			add("weekdays", "Jour de la semaine invalide.")
			break
		}
		if d := int16(n); !seen[d] {
			seen[d] = true
			w.weekdays = append(w.weekdays, d)
		}
	}
	if w.weekdays == nil {
		w.weekdays = []int16{}
	}

	f, t := strings.TrimSpace(in.From), strings.TrimSpace(in.To)
	if f != "" || t != "" {
		pf, okf := waitlistClock(f)
		pt, okt := waitlistClock(t)
		switch {
		case !okf || !okt:
			add("time_from", "Indiquez l'heure de début et l'heure de fin de la plage horaire, ou laissez les deux vides.")
		case pf.Microseconds >= pt.Microseconds:
			add("time_to", "La fin de la plage horaire doit suivre son début.")
		default:
			w.from, w.to = pf, pt
		}
	}
	if len(errs) > 0 {
		return window{}, errs
	}
	return w, nil
}

// binding lie le code de vérification à la fenêtre et aux coordonnées saisies.
func (w window) binding(d details, channel string) string {
	parts := []string{"waitlist", uuid.UUID(w.practitioner.Bytes).String(), w.typeID.String(),
		w.earliest.Time.Format("2006-01-02"), w.latest.Time.Format("2006-01-02"),
		fmt.Sprint(w.weekdays), fmt.Sprint(w.from.Microseconds, w.from.Valid), fmt.Sprint(w.to.Microseconds, w.to.Valid),
		d.binding(uuid.UUID(w.practitioner.Bytes), w.typeID, time.Time{}, channel)}
	return sha256Hex(parts...)
}

// waitlistTarget vérifie que le motif peut être proposé par la liste d'attente
// (actif, réservable en ligne, hors domicile) et retourne le cabinet. Avec un
// praticien, le cabinet est celui du praticien ; sans praticien (saisie du
// cabinet), practiceID est obligatoire.
func waitlistTarget(ctx context.Context, q *sqlcgen.Queries, practiceID, practitionerID, typeID uuid.UUID) (sqlcgen.Practice, *time.Location, error) {
	var pc sqlcgen.Practice
	if practitionerID != uuid.Nil {
		sc, err := loadSlotContext(ctx, q, practitionerID, typeID)
		if err != nil {
			return pc, nil, err
		}
		if practiceID != uuid.Nil && sc.pc.ID != practiceID {
			return pc, nil, domain.ErrNotFound
		}
		if sc.typ.AtHome {
			return pc, nil, errWaitlistHome
		}
		return sc.pc, sc.loc, nil
	}
	if practiceID == uuid.Nil {
		return pc, nil, domain.ErrNotFound
	}
	pc, err := q.GetPractice(ctx, practiceID)
	if err != nil {
		return pc, nil, fmt.Errorf("lecture cabinet: %w", err)
	}
	typ, err := q.GetAppointmentType(ctx, sqlcgen.GetAppointmentTypeParams{ID: typeID, PracticeID: practiceID})
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && (!typ.Active || !typ.OnlineBookable)) {
		return pc, nil, domain.ErrNotFound
	}
	if err != nil {
		return pc, nil, fmt.Errorf("lecture motif: %w", err)
	}
	if typ.AtHome {
		return pc, nil, errWaitlistHome
	}
	loc, err := time.LoadLocation(pc.Timezone)
	if err != nil {
		return pc, nil, fmt.Errorf("fuseau %q: %w", pc.Timezone, err)
	}
	return pc, loc, nil
}

var errWaitlistHome = domain.ErrValidation{Field: "type", Msg: "La liste d'attente n'est pas disponible pour les visites à domicile. Contactez le cabinet."}

// WaitlistVerifyInput décrit une inscription en ligne avant vérification du contact.
type WaitlistVerifyInput struct {
	Window  WindowInput
	Patient PatientDetails
	Channel string // "sms" | "email" : où envoyer le code
}

// StartVerification valide la saisie puis envoie un code à 6 chiffres (mêmes
// limites que la réservation : OTPPerHour codes par heure et par destinataire,
// ErrOTPRateLimited). Le praticien est obligatoire côté patient.
func (w *Waitlist) StartVerification(ctx context.Context, in WaitlistVerifyInput) (Verification, error) {
	now := w.Clock.Now()
	if in.Window.PractitionerID == uuid.Nil {
		return Verification{}, domain.ErrNotFound
	}
	d, err := parseDetails(in.Patient, in.Channel, now)
	if err != nil {
		return Verification{}, err
	}
	q := sqlcgen.New(w.Pool)
	pc, loc, err := waitlistTarget(ctx, q, uuid.Nil, in.Window.PractitionerID, in.Window.TypeID)
	if err != nil {
		return Verification{}, err
	}
	if err := checkOTPChannel(ctx, q, pc, in.Channel); err != nil {
		return Verification{}, err
	}
	win, err := parseWindow(in.Window, now, loc)
	if err != nil {
		return Verification{}, err
	}
	code, err := newOTPCode()
	if err != nil {
		return Verification{}, err
	}
	recipient := d.recipient(in.Channel)
	recipientHash := sha256Hex(pc.ID.String(), in.Channel, recipient)

	var id uuid.UUID
	err = store.WithTx(ctx, w.Pool, func(q *sqlcgen.Queries, tx pgx.Tx) error {
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
		id = uuid.New()
		if _, err := q.CreateOTPChallenge(ctx, sqlcgen.CreateOTPChallengeParams{
			ID: id, PracticeID: pc.ID, Channel: in.Channel, RecipientHash: recipientHash,
			BindingHash: win.binding(d, in.Channel), CodeHash: otpHash(id, code), ExpiresAt: now.Add(OTPTTL), CreatedAt: now,
		}); err != nil {
			return fmt.Errorf("création OTP: %w", err)
		}
		return w.OTP.SendOTP(ctx, tx, pc.ID, pc.Name, in.Channel, recipient, code)
	})
	if err != nil {
		return Verification{}, err
	}
	return Verification{ChallengeID: id, Channel: in.Channel}, nil
}

// WaitlistJoinInput décrit une inscription à confirmer.
type WaitlistJoinInput struct {
	WaitlistVerifyInput
	ChallengeID uuid.UUID
	Code        string
}

// Join vérifie le code puis inscrit le patient (retrouvé ou créé). Erreurs :
// ErrOTPWrong (réessayer), ErrOTPExpired (redemander un code), domain.ValidationErrors.
func (w *Waitlist) Join(ctx context.Context, in WaitlistJoinInput) (sqlcgen.WaitlistEntry, error) {
	now := w.Clock.Now()
	d, err := parseDetails(in.Patient, in.Channel, now)
	if err != nil {
		return sqlcgen.WaitlistEntry{}, err
	}
	q := sqlcgen.New(w.Pool)
	ch, err := q.TryOTPChallenge(ctx, sqlcgen.TryOTPChallengeParams{ID: in.ChallengeID, Now: now, MaxAttempts: OTPMaxAttempts})
	if errors.Is(err, pgx.ErrNoRows) {
		return sqlcgen.WaitlistEntry{}, ErrOTPExpired
	}
	if err != nil {
		return sqlcgen.WaitlistEntry{}, fmt.Errorf("lecture OTP: %w", err)
	}
	pc, loc, err := waitlistTarget(ctx, q, ch.PracticeID, in.Window.PractitionerID, in.Window.TypeID)
	if err != nil {
		return sqlcgen.WaitlistEntry{}, err
	}
	win, err := parseWindow(in.Window, now, loc)
	if err != nil {
		return sqlcgen.WaitlistEntry{}, err
	}
	if ch.BindingHash != win.binding(d, in.Channel) {
		return sqlcgen.WaitlistEntry{}, ErrOTPExpired // données modifiées depuis l'envoi du code
	}
	if !otpMatches(ch.CodeHash, in.ChallengeID, in.Code) {
		return sqlcgen.WaitlistEntry{}, ErrOTPWrong
	}
	var entry sqlcgen.WaitlistEntry
	err = store.WithTx(ctx, w.Pool, func(q *sqlcgen.Queries, _ pgx.Tx) error {
		n, err := q.ConsumeOTPChallenge(ctx, sqlcgen.ConsumeOTPChallengeParams{ID: in.ChallengeID, Now: now})
		if err != nil {
			return fmt.Errorf("consommation OTP: %w", err)
		}
		if n == 0 {
			return ErrOTPExpired
		}
		patient, err := findOrCreatePatient(ctx, q, pc.ID, d)
		if err != nil {
			return err
		}
		entry, err = insertWaitlistEntry(ctx, q, pc.ID, patient.ID, win, in.Channel, "online", now)
		return err
	})
	if err != nil {
		return sqlcgen.WaitlistEntry{}, err
	}
	return entry, nil
}

func insertWaitlistEntry(ctx context.Context, q *sqlcgen.Queries, practiceID, patientID uuid.UUID, win window, verified, source string, now time.Time) (sqlcgen.WaitlistEntry, error) {
	n, err := q.CountActiveWaitlistForPatient(ctx, patientID)
	if err != nil {
		return sqlcgen.WaitlistEntry{}, fmt.Errorf("comptage des inscriptions: %w", err)
	}
	if n >= maxActiveWaitlist {
		return sqlcgen.WaitlistEntry{}, domain.ErrValidation{Field: "earliest", Msg: "Ce patient a déjà le nombre maximal d'inscriptions actives en liste d'attente."}
	}
	e, err := q.CreateWaitlistEntry(ctx, sqlcgen.CreateWaitlistEntryParams{
		PracticeID: practiceID, PatientID: patientID, PractitionerID: win.practitioner, AppointmentTypeID: win.typeID,
		Earliest: win.earliest, Latest: win.latest, Weekdays: win.weekdays, TimeFrom: win.from, TimeTo: win.to,
		VerifiedChannel: pgtype.Text{String: verified, Valid: verified != ""}, Source: source, CreatedAt: now,
	})
	if err != nil {
		return sqlcgen.WaitlistEntry{}, fmt.Errorf("création de l'inscription: %w", err)
	}
	return e, nil
}

// StaffAddInput est l'inscription saisie par le cabinet.
type StaffAddInput struct {
	Patient PatientDetails
	Window  WindowInput
}

// AddByStaff inscrit un patient (retrouvé ou créé) sans code de vérification : le
// cabinet connaît ses coordonnées, tous ses contacts sont utilisables.
func (w *Waitlist) AddByStaff(ctx context.Context, a Actor, in StaffAddInput) (sqlcgen.WaitlistEntry, error) {
	now := w.Clock.Now()
	var entry sqlcgen.WaitlistEntry
	err := store.WithTx(ctx, w.Pool, func(q *sqlcgen.Queries, _ pgx.Tx) error {
		if in.Window.PractitionerID != uuid.Nil {
			pr, err := q.GetPractitioner(ctx, sqlcgen.GetPractitionerParams{ID: in.Window.PractitionerID, PracticeID: a.PracticeID})
			if errors.Is(err, pgx.ErrNoRows) {
				return domain.ErrNotFound
			}
			if err != nil {
				return fmt.Errorf("lecture praticien: %w", err)
			}
			if !pr.Active {
				return domain.ErrNotFound
			}
		}
		_, loc, err := waitlistTarget(ctx, q, a.PracticeID, in.Window.PractitionerID, in.Window.TypeID)
		if err != nil {
			return err
		}
		win, err := parseWindow(in.Window, now, loc)
		if err != nil {
			return err
		}
		patient, err := FindOrCreatePatient(ctx, q, a.PracticeID, in.Patient, now)
		if err != nil {
			return err
		}
		entry, err = insertWaitlistEntry(ctx, q, a.PracticeID, patient.ID, win, "", "staff", now)
		return err
	})
	if err != nil {
		return sqlcgen.WaitlistEntry{}, err
	}
	return entry, nil
}

// WaitlistForm est ce qu'il faut pour afficher l'écran du cabinet.
type WaitlistForm struct {
	Entries       []sqlcgen.ListWaitlistEntriesRow
	Types         []sqlcgen.AppointmentType
	Practitioners []sqlcgen.Practitioner
	Loc           *time.Location
	Today         string // AAAA-MM-JJ, jour du cabinet
}

// Form retourne l'écran du cabinet : inscriptions (actives d'abord) et choix du formulaire.
func (w *Waitlist) Form(ctx context.Context, a Actor) (WaitlistForm, error) {
	q := sqlcgen.New(w.Pool)
	pc, err := q.GetPractice(ctx, a.PracticeID)
	if err != nil {
		return WaitlistForm{}, fmt.Errorf("lecture cabinet: %w", err)
	}
	loc, err := time.LoadLocation(pc.Timezone)
	if err != nil {
		return WaitlistForm{}, fmt.Errorf("fuseau %q: %w", pc.Timezone, err)
	}
	entries, err := q.ListWaitlistEntries(ctx, sqlcgen.ListWaitlistEntriesParams{PracticeID: a.PracticeID, PageSize: pageSize})
	if err != nil {
		return WaitlistForm{}, fmt.Errorf("liste d'attente: %w", err)
	}
	types, err := q.ListBookableAppointmentTypes(ctx, sqlcgen.ListBookableAppointmentTypesParams{PracticeID: a.PracticeID, PageSize: pageSize})
	if err != nil {
		return WaitlistForm{}, fmt.Errorf("liste des motifs: %w", err)
	}
	prs, err := q.ListPractitioners(ctx, sqlcgen.ListPractitionersParams{PracticeID: a.PracticeID, PageSize: pageSize})
	if err != nil {
		return WaitlistForm{}, fmt.Errorf("liste des praticiens: %w", err)
	}
	f := WaitlistForm{Entries: entries, Loc: loc, Today: w.Clock.Now().In(loc).Format("2006-01-02"), Practitioners: prs}
	for _, t := range types {
		if !t.AtHome {
			f.Types = append(f.Types, t)
		}
	}
	return f, nil
}

// Cancel retire une inscription active du cabinet.
func (w *Waitlist) Cancel(ctx context.Context, a Actor, id uuid.UUID) error {
	n, err := sqlcgen.New(w.Pool).CancelWaitlistEntry(ctx, sqlcgen.CancelWaitlistEntryParams{ID: id, PracticeID: a.PracticeID})
	if err != nil {
		return fmt.Errorf("annulation de l'inscription: %w", err)
	}
	if n == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// --- Proposition (lien /w/{jeton}) ---

// OfferView est la proposition retrouvée par son lien.
type OfferView struct {
	Practice     sqlcgen.Practice
	Practitioner sqlcgen.Practitioner
	Type         sqlcgen.AppointmentType
	FirstName    string
	Start, End   time.Time // fuseau du cabinet
	ExpiresAt    time.Time
	Loc          *time.Location
	// Available : le patient peut encore réserver. Sinon, Reason vaut « expired »
	// (délai écoulé ou inscription close), « taken » (créneau pris) ou « claimed »
	// (déjà réservé via ce lien).
	Available bool
	Reason    string
}

func offerToken(token string) (string, error) {
	if token == "" || len(token) > 128 {
		return "", domain.ErrNotFound
	}
	return ManageTokenHash(token), nil
}

// Offer retrouve une proposition par le secret de son lien. Jeton inconnu :
// domain.ErrNotFound. Lecture seule.
func (w *Waitlist) Offer(ctx context.Context, token string) (OfferView, error) {
	hash, err := offerToken(token)
	if err != nil {
		return OfferView{}, err
	}
	q := sqlcgen.New(w.Pool)
	o, err := q.GetWaitlistOfferByToken(ctx, hash)
	if errors.Is(err, pgx.ErrNoRows) {
		return OfferView{}, domain.ErrNotFound
	}
	if err != nil {
		return OfferView{}, fmt.Errorf("lecture proposition: %w", err)
	}
	e, err := q.GetWaitlistEntry(ctx, o.EntryID)
	if err != nil {
		return OfferView{}, fmt.Errorf("lecture inscription: %w", err)
	}
	pc, err := q.GetPractice(ctx, o.PracticeID)
	if err != nil {
		return OfferView{}, fmt.Errorf("lecture cabinet: %w", err)
	}
	pr, err := q.GetPractitioner(ctx, sqlcgen.GetPractitionerParams{ID: o.PractitionerID, PracticeID: o.PracticeID})
	if err != nil {
		return OfferView{}, fmt.Errorf("lecture praticien: %w", err)
	}
	typ, err := q.GetAppointmentType(ctx, sqlcgen.GetAppointmentTypeParams{ID: e.AppointmentTypeID, PracticeID: o.PracticeID})
	if err != nil {
		return OfferView{}, fmt.Errorf("lecture motif: %w", err)
	}
	pat, err := q.GetPatient(ctx, sqlcgen.GetPatientParams{ID: e.PatientID, PracticeID: o.PracticeID})
	if err != nil {
		return OfferView{}, fmt.Errorf("lecture patient: %w", err)
	}
	loc, err := time.LoadLocation(pc.Timezone)
	if err != nil {
		return OfferView{}, fmt.Errorf("fuseau %q: %w", pc.Timezone, err)
	}
	now := w.Clock.Now()
	v := OfferView{
		Practice: pc, Practitioner: pr, Type: typ, FirstName: pat.FirstName, Loc: loc,
		Start: o.SlotStart.In(loc), End: o.SlotStart.Add(microsToDuration(intervalMicros(typ.Duration))).In(loc),
		ExpiresAt: o.ExpiresAt.In(loc),
	}
	switch {
	case o.ClaimedAt.Valid:
		v.Reason = "claimed"
	case !now.Before(o.ExpiresAt) || e.Status != "active":
		v.Reason = "expired"
	default:
		sc, err := loadSlotContext(ctx, q, o.PractitionerID, e.AppointmentTypeID)
		if err == nil {
			_, err = sc.check(ctx, q, now, o.SlotStart)
		}
		switch {
		case err == nil:
			v.Available = true
		case errors.Is(err, domain.ErrSlotTaken), errors.Is(err, domain.ErrNotFound):
			v.Reason = "taken"
		default:
			return OfferView{}, err
		}
	}
	return v, nil
}

// Claim réserve le créneau proposé (même règles que la réservation en ligne : le
// moteur de créneaux revalide, la contrainte EXCLUDE tranche entre deux patients
// simultanés). Le premier gagne ; l'autre reçoit domain.ErrSlotTaken. Retourne le
// rendez-vous et le jeton du lien de gestion. Erreurs : domain.ErrNotFound (jeton
// inconnu), ErrOfferExpired, domain.ErrSlotTaken.
func (w *Waitlist) Claim(ctx context.Context, token string) (BookResult, error) {
	hash, err := offerToken(token)
	if err != nil {
		return BookResult{}, err
	}
	apptID, manage, manageHash, err := w.ManageKey.mint()
	if err != nil {
		return BookResult{}, err
	}
	now := w.Clock.Now()
	var appt sqlcgen.Appointment
	err = store.WithTx(ctx, w.Pool, func(q *sqlcgen.Queries, tx pgx.Tx) error {
		o, err := q.LockWaitlistOfferByToken(ctx, hash)
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("lecture proposition: %w", err)
		}
		if o.ClaimedAt.Valid || !now.Before(o.ExpiresAt) {
			return ErrOfferExpired
		}
		e, err := q.LockWaitlistEntry(ctx, o.EntryID)
		if err != nil {
			return fmt.Errorf("lecture inscription: %w", err)
		}
		if e.Status != "active" {
			return ErrOfferExpired
		}
		sc, err := loadSlotContext(ctx, q, o.PractitionerID, e.AppointmentTypeID)
		if errors.Is(err, domain.ErrNotFound) {
			return domain.ErrSlotTaken
		}
		if err != nil {
			return err
		}
		slot, err := sc.check(ctx, q, now, o.SlotStart)
		if err != nil {
			return err
		}
		bb, ba := sc.buffers()
		appt, err = store.CreateAppointment(ctx, q, store.NewAppointment{
			ID: apptID, PracticeID: o.PracticeID, PractitionerID: o.PractitionerID, PatientID: e.PatientID, AppointmentTypeID: sc.typ.ID,
			Start: slot.Start, End: slot.End, BufferBefore: bb, BufferAfter: ba,
			Source: "online", ManageTokenHash: manageHash, VerifiedChannel: e.VerifiedChannel.String,
		})
		if err != nil {
			return err
		}
		if err := q.FulfillWaitlistEntry(ctx, e.ID); err != nil {
			return fmt.Errorf("clôture de l'inscription: %w", err)
		}
		if err := q.MarkWaitlistOfferClaimed(ctx, sqlcgen.MarkWaitlistOfferClaimedParams{ID: o.ID, Now: pgtype.Timestamptz{Time: now, Valid: true}}); err != nil {
			return fmt.Errorf("clôture de la proposition: %w", err)
		}
		return w.notifier().booked(ctx, tx, q, appt, manage)
	})
	if err != nil {
		return BookResult{}, err
	}
	return BookResult{Appointment: appt, Token: manage}, nil
}

// --- Expiration ---

// ExpireOnce passe en « expired » les inscriptions dont la dernière date est
// passée (jour du cabinet) et retourne leur nombre.
func (w *Waitlist) ExpireOnce(ctx context.Context) (int, error) {
	n, err := sqlcgen.New(w.Pool).ExpireWaitlistEntries(ctx, w.Clock.Now())
	if err != nil {
		return 0, fmt.Errorf("expiration de la liste d'attente: %w", err)
	}
	return int(n), nil
}

// Run appelle ExpireOnce périodiquement jusqu'à l'annulation de ctx.
func (w *Waitlist) Run(ctx context.Context) error {
	every := w.Every
	if every <= 0 {
		every = DefaultWaitlistEvery
	}
	log := w.Logger
	if log == nil {
		log = slog.Default()
	}
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		if n, err := w.ExpireOnce(ctx); err != nil {
			if ctx.Err() == nil {
				log.Error("liste d'attente: expiration en échec", "error", err)
			}
		} else if n > 0 {
			log.Info("liste d'attente: inscriptions expirées", "count", n)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
		}
	}
}
