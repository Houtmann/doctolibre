package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"doctolibre/internal/booking"
	"doctolibre/internal/domain"
	"doctolibre/internal/store"
	"doctolibre/internal/store/sqlcgen"
)

// Bornes des séries de séances.
const (
	maxPlanSessions   = 100
	maxPlanLabelLen   = 100
	planSearchWeeks   = 12 // semaines de marge au-delà de la durée théorique de la série
	replacementWeeks  = 12
	planStatusActive  = "active"
	planStatusDone    = "completed"
	planStatusStopped = "cancelled"
)

var planStatusLabels = map[string]string{planStatusActive: "En cours", planStatusDone: "Terminée", planStatusStopped: "Abandonnée"}

// CarePlans gère les séries de séances (une prescription = N séances). Aucune
// donnée médicale : seulement un libellé administratif, un motif et des dates.
type CarePlans struct {
	Pool   *pgxpool.Pool
	Clock  domain.Clock
	Events EventSink // nil = NopSink ; appelé après commit
	// BaseURL : adresse publique (messages aux patients) ; vide = messages sans lien.
	BaseURL string
}

func (s *CarePlans) notifier() notifier { return notifier{clock: s.Clock, baseURL: s.BaseURL} }

func (s *CarePlans) practice() *Practice { return &Practice{Pool: s.Pool, Clock: s.Clock} }

func (s *CarePlans) emit(ctx context.Context, e Event) {
	e.At = s.Clock.Now()
	if s.Events != nil {
		s.Events.Emit(ctx, e)
	}
}

// PlanInput est la saisie du formulaire d'une série (chaînes brutes).
type PlanInput struct {
	PatientID, TypeID, Label, Prescribed, PerWeek string
	Weekdays                                      []string // "0" (lundi) à "6" (dimanche)
	WindowStart, WindowEnd                        string   // "HH:MM", les deux vides = pas de contrainte
	StartDate                                     string   // "AAAA-MM-JJ"
	Address                                       string   // visites à domicile uniquement
}

type planParams struct {
	patientID, typeID uuid.UUID
	label             string
	prescribed        int
	perWeek           int
	weekdays          []int16
	hasWindow         bool
	winStart, winEnd  booking.LocalTime
	start             booking.Date
	address           string
}

func parsePlan(in PlanInput) (planParams, error) {
	var errs domain.ValidationErrors
	add := func(field, msg string) { errs = append(errs, domain.ErrValidation{Field: field, Msg: msg}) }
	var p planParams
	var err error
	if p.patientID, err = uuid.Parse(strings.TrimSpace(in.PatientID)); err != nil {
		add("patient_id", "Patient invalide.")
	}
	if p.typeID, err = uuid.Parse(strings.TrimSpace(in.TypeID)); err != nil {
		add("type_id", "Choisissez un motif.")
	}
	p.label = strings.TrimSpace(in.Label)
	if tooLong(p.label, maxPlanLabelLen) || strings.ContainsFunc(p.label, unicode.IsControl) {
		add("label", "Le libellé est limité à 100 caractères, sans retour à la ligne. Aucune information médicale ici.")
	}
	if p.prescribed, err = strconv.Atoi(strings.TrimSpace(in.Prescribed)); err != nil || p.prescribed < 1 || p.prescribed > maxPlanSessions {
		add("prescribed", "Le nombre de séances doit être compris entre 1 et 100.")
	}
	if p.perWeek, err = strconv.Atoi(strings.TrimSpace(in.PerWeek)); err != nil || p.perWeek < 1 || p.perWeek > 7 {
		add("per_week", "La fréquence doit être de 1 à 7 séances par semaine.")
	}
	seen := map[int]bool{}
	for _, w := range in.Weekdays {
		n, err := strconv.Atoi(strings.TrimSpace(w))
		if err != nil || n < 0 || n > 6 {
			add("weekdays", "Jour de la semaine invalide.")
			break
		}
		if !seen[n] {
			seen[n] = true
			p.weekdays = append(p.weekdays, int16(n)) //nolint:gosec // 0..6
		}
	}
	ws, we := strings.TrimSpace(in.WindowStart), strings.TrimSpace(in.WindowEnd)
	if ws != "" || we != "" {
		s, err1 := booking.ParseLocalTime(ws)
		e, err2 := booking.ParseLocalTime(we)
		if err1 != nil || err2 != nil || s >= e {
			add("window", "La plage horaire souhaitée doit avoir un début et une fin (HH:MM), le début avant la fin.")
		}
		p.hasWindow, p.winStart, p.winEnd = true, s, e
	}
	d, err := time.Parse("2006-01-02", strings.TrimSpace(in.StartDate))
	if err != nil {
		add("start_date", "La date de début doit être au format AAAA-MM-JJ.")
	}
	p.start = booking.DateOf(d)
	p.address = strings.TrimSpace(in.Address)
	if tooLong(p.address, maxAddressLen) || strings.ContainsFunc(p.address, unicode.IsControl) {
		add("address", "L'adresse n'est pas valide (300 caractères maximum, sans retour à la ligne).")
	}
	if len(errs) > 0 {
		return planParams{}, errs
	}
	return p, nil
}

// minGapDays : un jour sans séance entre deux séances tant que la fréquence le permet.
func minGapDays(perWeek int) int {
	if perWeek <= 3 {
		return 2
	}
	return 1
}

func goWeekdays(ws []int16) []time.Weekday {
	out := make([]time.Weekday, len(ws))
	for i, w := range ws {
		out[i] = time.Weekday((int(w) + 1) % 7) // 0 = lundi en base, 0 = dimanche en Go
	}
	return out
}

// staffSlotContext prépare le calcul de créneaux pour le cabinet : contrairement
// à la réservation en ligne, ni préavis, ni horizon, et tous les motifs actifs.
func staffSlotContext(ctx context.Context, q *sqlcgen.Queries, practiceID, practitionerID, typeID uuid.UUID) (slotContext, error) {
	pr, err := q.GetActivePractitioner(ctx, practitionerID)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && pr.PracticeID != practiceID) {
		return slotContext{}, domain.ErrNotFound
	}
	if err != nil {
		return slotContext{}, fmt.Errorf("lecture praticien: %w", err)
	}
	pc, err := q.GetPractice(ctx, practiceID)
	if err != nil {
		return slotContext{}, fmt.Errorf("lecture cabinet: %w", err)
	}
	typ, err := q.GetAppointmentType(ctx, sqlcgen.GetAppointmentTypeParams{ID: typeID, PracticeID: practiceID})
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && !typ.Active) {
		return slotContext{}, domain.ErrValidation{Field: "type_id", Msg: "Motif inconnu ou désactivé."}
	}
	if err != nil {
		return slotContext{}, fmt.Errorf("lecture motif: %w", err)
	}
	loc, err := time.LoadLocation(pc.Timezone)
	if err != nil {
		return slotContext{}, fmt.Errorf("fuseau %q: %w", pc.Timezone, err)
	}
	pc.MinBookingNotice, pc.BookingHorizon = pgtype.Interval{Valid: true}, pgtype.Interval{Valid: true}
	return slotContext{pr: pr, pc: pc, typ: typ, loc: loc}, nil
}

// proposeSessions calcule jusqu'à count séances à partir de la date from (heure du cabinet).
func (sc slotContext) proposeSessions(ctx context.Context, q *sqlcgen.Queries, now time.Time, from booking.Date, count, perWeek int,
	weekdays []int16, window *[2]booking.LocalTime, taken []booking.Date) ([]booking.Slot, error) {
	weeks := (count+max(perWeek, 1)-1)/max(perWeek, 1) + planSearchWeeks
	start := time.Date(from.Year, from.Month, from.Day, 0, 0, 0, 0, sc.loc)
	slots, err := sc.slots(ctx, q, now, start, start.AddDate(0, 0, 7*weeks))
	if err != nil {
		return nil, err
	}
	req := booking.SeriesRequest{
		Loc: sc.loc, Count: count, PerWeek: perWeek, Weekdays: goWeekdays(weekdays),
		From: from, MinGapDays: minGapDays(perWeek), Taken: taken,
	}
	if window != nil {
		req.WindowStart, req.WindowEnd = window[0], window[1]
	}
	return booking.ProposeSeries(slots, req), nil
}

func (p planParams) window() *[2]booking.LocalTime {
	if !p.hasWindow {
		return nil
	}
	return &[2]booking.LocalTime{p.winStart, p.winEnd}
}

// ProposalLine est une séance proposée (ou une ligne vide à remplir à la main).
type ProposalLine struct {
	Row        int // 1-based
	Date, Time string
}

// Proposal est la proposition automatique d'une série, que le praticien ajuste.
type Proposal struct {
	Patient sqlcgen.Patient
	AtHome  bool
	Lines   []ProposalLine
	Found   int // lignes pré-remplies (les autres sont vides)
}

// FormData alimente le formulaire de nouvelle série.
type FormData struct {
	Patient sqlcgen.Patient
	Types   []sqlcgen.AppointmentType
	Today   string // date du jour dans le fuseau du cabinet (AAAA-MM-JJ)
}

// Form retourne le patient et les motifs actifs pour le formulaire.
func (s *CarePlans) Form(ctx context.Context, a Actor, patientID uuid.UUID) (FormData, error) {
	p, err := sqlcgen.New(s.Pool).GetPatient(ctx, sqlcgen.GetPatientParams{ID: patientID, PracticeID: a.PracticeID})
	if errors.Is(err, pgx.ErrNoRows) {
		return FormData{}, domain.ErrNotFound
	}
	if err != nil {
		return FormData{}, fmt.Errorf("lecture patient: %w", err)
	}
	all, err := s.practice().AppointmentTypes(ctx, a)
	if err != nil {
		return FormData{}, err
	}
	loc, err := s.practice().location(ctx, a.PracticeID)
	if err != nil {
		return FormData{}, err
	}
	out := FormData{Patient: p, Today: s.Clock.Now().In(loc).Format("2006-01-02")}
	for _, t := range all {
		if t.Active {
			out.Types = append(out.Types, t)
		}
	}
	return out, nil
}

// Propose calcule la proposition de séances (premier créneau compatible par
// semaine, jours préférés d'abord, un jour libre entre deux séances).
func (s *CarePlans) Propose(ctx context.Context, a Actor, in PlanInput) (Proposal, error) {
	p, err := parsePlan(in)
	if err != nil {
		return Proposal{}, err
	}
	q := sqlcgen.New(s.Pool)
	patient, err := q.GetPatient(ctx, sqlcgen.GetPatientParams{ID: p.patientID, PracticeID: a.PracticeID})
	if errors.Is(err, pgx.ErrNoRows) {
		return Proposal{}, domain.ErrValidation{Field: "patient_id", Msg: "Patient introuvable."}
	}
	if err != nil {
		return Proposal{}, fmt.Errorf("lecture patient: %w", err)
	}
	pr, err := s.practice().Practitioner(ctx, a)
	if err != nil {
		return Proposal{}, err
	}
	sc, err := staffSlotContext(ctx, q, a.PracticeID, pr.ID, p.typeID)
	if err != nil {
		return Proposal{}, err
	}
	got, err := sc.proposeSessions(ctx, q, s.Clock.Now(), p.start, p.prescribed, p.perWeek, p.weekdays, p.window(), nil)
	if err != nil {
		return Proposal{}, err
	}
	out := Proposal{Patient: patient, AtHome: sc.typ.AtHome, Found: len(got)}
	for i := range p.prescribed {
		l := ProposalLine{Row: i + 1}
		if i < len(got) {
			t := got[i].Start.In(sc.loc)
			l.Date, l.Time = t.Format("2006-01-02"), t.Format("15:04")
		}
		out.Lines = append(out.Lines, l)
	}
	return out, nil
}

// LineInput est une séance du formulaire (date et heure du cabinet). Une ligne
// entièrement vide est ignorée.
type LineInput struct{ Date, Time string }

// CreateInput est la validation d'une série : paramètres + séances ajustées.
type CreateInput struct {
	Plan  PlanInput
	Lines []LineInput
}

// LineConflict signale la séance (ligne Row, 1-based) qui chevauche un autre
// rendez-vous. Toute la série est alors annulée (rien n'est créé). Is(ErrSlotTaken) est vrai.
type LineConflict struct {
	Row   int
	Start time.Time
}

func (e *LineConflict) Error() string {
	return fmt.Sprintf("séance %d : %s", e.Row, ErrSlotTakenMsg)
}

// Is permet errors.Is(err, domain.ErrSlotTaken).
func (e *LineConflict) Is(target error) bool { return target == domain.ErrSlotTaken }

type sessionLine struct {
	row   int
	start time.Time
}

// parseLines lit les lignes non vides ; les erreurs de saisie citent la ligne.
func parseLines(lines []LineInput, loc *time.Location) ([]sessionLine, domain.ValidationErrors) {
	var out []sessionLine
	var errs domain.ValidationErrors
	for i, l := range lines {
		date, clock := strings.TrimSpace(l.Date), strings.TrimSpace(l.Time)
		if date == "" && clock == "" {
			continue
		}
		d, err := time.Parse("2006-01-02", date)
		sec, ok := parseClock(clock)
		if err != nil || !ok {
			errs = append(errs, domain.ErrValidation{Field: "lines", Msg: fmt.Sprintf("Séance %d : date (AAAA-MM-JJ) et heure (HH:MM) requises.", i+1)})
			continue
		}
		out = append(out, sessionLine{row: i + 1, start: time.Date(d.Year(), d.Month(), d.Day(), int(sec/3600), int(sec/60%60), 0, 0, loc)})
	}
	return out, errs
}

// validateLines vérifie que chaque séance est future et dans les horaires d'ouverture.
func validateLines(ctx context.Context, q *sqlcgen.Queries, sc slotContext, now time.Time, lines []sessionLine) domain.ValidationErrors {
	var errs domain.ValidationErrors
	add := func(row int, msg string) {
		errs = append(errs, domain.ErrValidation{Field: "lines", Msg: fmt.Sprintf("Séance %d : %s", row, msg)})
	}
	dur := intervalDuration(sc.typ.Duration)
	for _, l := range lines {
		switch ok, err := withinOpeningHours(ctx, q, sc.pr.ID, sc.typ.ID, l.start, l.start.Add(dur), sc.loc); {
		case !l.start.After(now):
			add(l.row, "la date doit être dans le futur.")
		case err != nil:
			errs = append(errs, domain.ErrValidation{Field: "lines", Msg: "Impossible de vérifier les horaires d'ouverture."})
		case !ok:
			add(l.row, "ce créneau est hors des horaires d'ouverture.")
		}
	}
	return errs
}

// insertSessions crée les RDV de la série dans la transaction de q ; le premier
// chevauchement renvoie *LineConflict (l'appelant annule la transaction).
func insertSessions(ctx context.Context, q *sqlcgen.Queries, a Actor, sc slotContext, patientID, planID uuid.UUID, address string, lines []sessionLine) ([]uuid.UUID, error) {
	bb, ba := sc.buffers()
	dur := intervalDuration(sc.typ.Duration)
	if !sc.typ.AtHome {
		address = ""
	}
	var ids []uuid.UUID
	for _, l := range lines {
		ap, err := store.CreateAppointment(ctx, q, store.NewAppointment{
			PracticeID: a.PracticeID, PractitionerID: sc.pr.ID, PatientID: patientID, AppointmentTypeID: sc.typ.ID,
			Start: l.start, End: l.start.Add(dur), BufferBefore: bb, BufferAfter: ba, Address: address, Source: "staff",
		})
		if errors.Is(err, domain.ErrSlotTaken) {
			return nil, &LineConflict{Row: l.row, Start: l.start}
		}
		if err != nil {
			return nil, err
		}
		if err := q.AttachAppointmentToCarePlan(ctx, sqlcgen.AttachAppointmentToCarePlanParams{
			ID: ap.ID, PracticeID: a.PracticeID, CarePlanID: pgtype.UUID{Bytes: planID, Valid: true},
		}); err != nil {
			return nil, fmt.Errorf("rattachement à la série: %w", err)
		}
		if err := audit(ctx, q, a, EventAppointmentCreated, ap.ID, map[string]string{"start": l.start.UTC().Format(time.RFC3339), "care_plan": planID.String()}); err != nil {
			return nil, err
		}
		ids = append(ids, ap.ID)
	}
	return ids, nil
}

func auditPlan(ctx context.Context, q *sqlcgen.Queries, a Actor, action string, id uuid.UUID, meta map[string]string) error {
	b, err := json.Marshal(meta)
	if err != nil {
		return fmt.Errorf("meta audit: %w", err)
	}
	if err := q.CreateAuditLog(ctx, sqlcgen.CreateAuditLogParams{
		UserID: pgtype.UUID{Bytes: a.UserID, Valid: true}, PracticeID: pgtype.UUID{Bytes: a.PracticeID, Valid: true}, Actor: "user", Action: action,
		Entity: "care_plan", EntityID: pgtype.UUID{Bytes: id, Valid: true}, Meta: b,
	}); err != nil {
		return fmt.Errorf("journal d'audit: %w", err)
	}
	return nil
}

func pgTime(l booking.LocalTime) pgtype.Time {
	return pgtype.Time{Microseconds: int64(l) * int64(time.Minute/time.Microsecond), Valid: true}
}

// Create crée la série et tous ses rendez-vous en une transaction : tout ou
// rien. Un chevauchement renvoie *LineConflict (ErrSlotTaken) avec la ligne en cause.
func (s *CarePlans) Create(ctx context.Context, a Actor, in CreateInput) (sqlcgen.CarePlan, error) {
	p, err := parsePlan(in.Plan)
	if err != nil {
		return sqlcgen.CarePlan{}, err
	}
	pr, err := s.practice().Practitioner(ctx, a)
	if err != nil {
		return sqlcgen.CarePlan{}, err
	}
	var plan sqlcgen.CarePlan
	var ids []uuid.UUID
	err = store.WithTx(ctx, s.Pool, func(q *sqlcgen.Queries, tx pgx.Tx) error {
		sc, err := staffSlotContext(ctx, q, a.PracticeID, pr.ID, p.typeID)
		if err != nil {
			return err
		}
		if sc.typ.AtHome && p.address == "" {
			return domain.ErrValidation{Field: "address", Msg: "L'adresse de la visite à domicile est requise."}
		}
		if _, err := q.GetPatient(ctx, sqlcgen.GetPatientParams{ID: p.patientID, PracticeID: a.PracticeID}); errors.Is(err, pgx.ErrNoRows) {
			return domain.ErrValidation{Field: "patient_id", Msg: "Patient introuvable."}
		} else if err != nil {
			return fmt.Errorf("lecture patient: %w", err)
		}
		lines, errs := parseLines(in.Lines, sc.loc)
		if len(lines) == 0 && len(errs) == 0 {
			errs = append(errs, domain.ErrValidation{Field: "lines", Msg: "Planifiez au moins une séance."})
		}
		if len(lines) > p.prescribed {
			errs = append(errs, domain.ErrValidation{Field: "lines", Msg: "Il y a plus de séances planifiées que de séances prescrites."})
		}
		errs = append(errs, validateLines(ctx, q, sc, s.Clock.Now(), lines)...)
		if len(errs) > 0 {
			return errs
		}
		cp := sqlcgen.CreateCarePlanParams{
			PracticeID: a.PracticeID, PatientID: p.patientID, PractitionerID: pr.ID, AppointmentTypeID: p.typeID,
			Label: p.label, SessionsPrescribed: int32(p.prescribed), FrequencyPerWeek: int16(p.perWeek), //nolint:gosec // bornés par parsePlan
			PreferredWeekdays: p.weekdays,
			StartDate:         pgtype.Date{Time: time.Date(p.start.Year, p.start.Month, p.start.Day, 0, 0, 0, 0, time.UTC), Valid: true},
		}
		if cp.PreferredWeekdays == nil {
			cp.PreferredWeekdays = []int16{}
		}
		if p.hasWindow {
			cp.PreferredTimeStart, cp.PreferredTimeEnd = pgTime(p.winStart), pgTime(p.winEnd)
		}
		if plan, err = q.CreateCarePlan(ctx, cp); err != nil {
			return fmt.Errorf("création de la série: %w", err)
		}
		if ids, err = insertSessions(ctx, q, a, sc, p.patientID, plan.ID, p.address, lines); err != nil {
			return err
		}
		if err := auditPlan(ctx, q, a, "care_plan.created", plan.ID, map[string]string{"sessions": fmt.Sprint(len(ids)), "prescribed": fmt.Sprint(p.prescribed)}); err != nil {
			return err
		}
		// Une seule confirmation pour toute la série ; les rappels restent par séance.
		appts := make([]sqlcgen.Appointment, 0, len(ids))
		for _, id := range ids {
			ap, err := q.GetAppointment(ctx, sqlcgen.GetAppointmentParams{ID: id, PracticeID: a.PracticeID})
			if err != nil {
				return fmt.Errorf("lecture rendez-vous: %w", err)
			}
			appts = append(appts, ap)
		}
		return s.notifier().seriesBooked(ctx, tx, q, appts)
	})
	if err != nil {
		return sqlcgen.CarePlan{}, err
	}
	for _, id := range ids {
		s.emit(ctx, Event{Kind: EventAppointmentCreated, PracticeID: a.PracticeID, AppointmentID: id})
	}
	return plan, nil
}

// --- Lecture : décompte et badges ---

// PlanSession est une séance d'une série. Ordinal > 0 pour les séances comptées
// (honorées ou à venir), dans l'ordre chronologique.
type PlanSession struct {
	ID          uuid.UUID
	Start       time.Time
	Status      string
	StatusLabel string
	Ordinal     int
}

// PlanView est une série avec son décompte.
type PlanView struct {
	ID                  uuid.UUID
	Label, TypeName     string
	Status, StatusLabel string
	Prescribed, PerWeek int
	Done, Booked, Count int // Count = Done + Booked
	Remaining           int
	Weekdays            string
	Sessions            []PlanSession
	Active              bool
	NeedsMoreSessions   bool // active et des séances restent à planifier
}

type planAppt struct {
	id     uuid.UUID
	start  time.Time
	status string
}

func weekdaysLabel(ws []int16) string {
	var names []string
	for _, w := range ws {
		names = append(names, Capitalize(WeekdayName(int(w))))
	}
	return strings.Join(names, ", ")
}

func buildPlanView(cp sqlcgen.CarePlan, typeName string, appts []planAppt, loc *time.Location) PlanView {
	v := PlanView{
		ID: cp.ID, Label: cp.Label, TypeName: typeName, Status: cp.Status, StatusLabel: planStatusLabels[cp.Status],
		Prescribed: int(cp.SessionsPrescribed), PerWeek: int(cp.FrequencyPerWeek), Weekdays: weekdaysLabel(cp.PreferredWeekdays),
		Active: cp.Status == planStatusActive,
	}
	for _, ap := range appts {
		ps := PlanSession{ID: ap.id, Start: ap.start.In(loc), Status: ap.status, StatusLabel: statusLabels[ap.status]}
		switch ap.status {
		case "done":
			v.Done++
		case "booked":
			v.Booked++
		}
		if ap.status == "done" || ap.status == "booked" {
			ps.Ordinal = v.Done + v.Booked
		}
		v.Sessions = append(v.Sessions, ps)
	}
	v.Count = v.Done + v.Booked
	v.Remaining = max(v.Prescribed-v.Count, 0)
	v.NeedsMoreSessions = v.Active && v.Remaining > 0
	return v
}

// ForPatient retourne les séries d'un patient (récentes d'abord) avec leurs séances.
func (s *CarePlans) ForPatient(ctx context.Context, a Actor, patientID uuid.UUID) ([]PlanView, error) {
	q := sqlcgen.New(s.Pool)
	plans, err := q.ListCarePlansForPatient(ctx, sqlcgen.ListCarePlansForPatientParams{PracticeID: a.PracticeID, PatientID: patientID})
	if err != nil {
		return nil, fmt.Errorf("séries du patient: %w", err)
	}
	if len(plans) == 0 {
		return nil, nil
	}
	rows, err := q.ListPatientCarePlanAppointments(ctx, sqlcgen.ListPatientCarePlanAppointmentsParams{PatientID: patientID, PracticeID: a.PracticeID})
	if err != nil {
		return nil, fmt.Errorf("séances du patient: %w", err)
	}
	loc, err := s.practice().location(ctx, a.PracticeID)
	if err != nil {
		return nil, err
	}
	by := map[uuid.UUID][]planAppt{}
	for _, r := range rows {
		by[r.CarePlanID.Bytes] = append(by[r.CarePlanID.Bytes], planAppt{r.ID, r.StartsAt, r.Status})
	}
	out := make([]PlanView, 0, len(plans))
	for _, r := range plans {
		cp := sqlcgen.CarePlan{
			ID: r.ID, PatientID: r.PatientID, PractitionerID: r.PractitionerID, Label: r.Label, SessionsPrescribed: r.SessionsPrescribed,
			FrequencyPerWeek: r.FrequencyPerWeek, PreferredWeekdays: r.PreferredWeekdays, Status: r.Status,
		}
		out = append(out, buildPlanView(cp, r.TypeName, by[r.ID], loc))
	}
	return out, nil
}

// Replacement est le prochain créneau compatible proposé pour remplacer une séance annulée.
type Replacement struct {
	PlanID     uuid.UUID
	Date, Time string
	Start      time.Time
}

// PlanBadge est la mention « séance 4/10 » d'un rendez-vous de série.
type PlanBadge struct {
	PlanID      uuid.UUID
	PatientID   uuid.UUID
	Label       string
	Ordinal     int // 0 = séance non comptée (annulée ou absent)
	Prescribed  int
	Count       int
	Replacement *Replacement
}

// ForAppointment retourne le badge de série d'un RDV (nil s'il n'en fait pas partie)
// et, pour une séance annulée d'une série en cours, un créneau de remplacement.
func (s *CarePlans) ForAppointment(ctx context.Context, a Actor, apptID uuid.UUID) (*PlanBadge, error) {
	q := sqlcgen.New(s.Pool)
	ap, err := q.GetAppointment(ctx, sqlcgen.GetAppointmentParams{ID: apptID, PracticeID: a.PracticeID})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("lecture rendez-vous: %w", err)
	}
	if !ap.CarePlanID.Valid {
		return nil, nil
	}
	cp, view, err := s.load(ctx, q, a, ap.CarePlanID.Bytes)
	if err != nil {
		return nil, err
	}
	b := &PlanBadge{PlanID: cp.ID, PatientID: cp.PatientID, Label: cp.Label, Prescribed: view.Prescribed, Count: view.Count}
	for _, ps := range view.Sessions {
		if ps.ID == apptID {
			b.Ordinal = ps.Ordinal
		}
	}
	if ap.Status == "cancelled" && view.NeedsMoreSessions {
		if b.Replacement, err = s.replacement(ctx, q, a, cp, view); err != nil {
			return nil, err
		}
	}
	return b, nil
}

func (s *CarePlans) load(ctx context.Context, q *sqlcgen.Queries, a Actor, id uuid.UUID) (sqlcgen.CarePlan, PlanView, error) {
	cp, err := q.GetCarePlan(ctx, sqlcgen.GetCarePlanParams{ID: id, PracticeID: a.PracticeID})
	if errors.Is(err, pgx.ErrNoRows) {
		return cp, PlanView{}, domain.ErrNotFound
	}
	if err != nil {
		return cp, PlanView{}, fmt.Errorf("lecture série: %w", err)
	}
	rows, err := q.ListCarePlanAppointments(ctx, sqlcgen.ListCarePlanAppointmentsParams{CarePlanID: pgtype.UUID{Bytes: id, Valid: true}, PracticeID: a.PracticeID})
	if err != nil {
		return cp, PlanView{}, fmt.Errorf("séances de la série: %w", err)
	}
	loc, err := s.practice().location(ctx, a.PracticeID)
	if err != nil {
		return cp, PlanView{}, err
	}
	appts := make([]planAppt, len(rows))
	for i, r := range rows {
		appts[i] = planAppt{r.ID, r.StartsAt, r.Status}
	}
	return cp, buildPlanView(cp, "", appts, loc), nil
}

// replacement cherche le prochain créneau compatible (à partir de demain,
// quota hebdomadaire et écart minimal comptés avec les séances non annulées).
func (s *CarePlans) replacement(ctx context.Context, q *sqlcgen.Queries, a Actor, cp sqlcgen.CarePlan, v PlanView) (*Replacement, error) {
	sc, err := staffSlotContext(ctx, q, a.PracticeID, cp.PractitionerID, cp.AppointmentTypeID)
	if err != nil {
		return nil, err
	}
	var taken []booking.Date
	for _, ps := range v.Sessions {
		if ps.Status != "cancelled" {
			taken = append(taken, booking.DateOf(ps.Start))
		}
	}
	now := s.Clock.Now()
	var win *[2]booking.LocalTime
	if cp.PreferredTimeStart.Valid && cp.PreferredTimeEnd.Valid {
		m := int64(time.Minute / time.Microsecond)
		win = &[2]booking.LocalTime{booking.LocalTime(cp.PreferredTimeStart.Microseconds / m), booking.LocalTime(cp.PreferredTimeEnd.Microseconds / m)}
	}
	// Une seule séance : fenêtre de recherche plus courte que pour une série entière.
	// À partir de demain (jamais avant le début de la série).
	tomorrow := time.Date(now.In(sc.loc).Year(), now.In(sc.loc).Month(), now.In(sc.loc).Day()+1, 12, 0, 0, 0, sc.loc)
	from := booking.DateOf(tomorrow)
	if cp.StartDate.Valid && tomorrow.Before(cp.StartDate.Time.Add(12*time.Hour)) {
		from = booking.DateOf(cp.StartDate.Time)
	}
	start := time.Date(from.Year, from.Month, from.Day, 0, 0, 0, 0, sc.loc)
	slots, err := sc.slots(ctx, q, now, start, start.AddDate(0, 0, 7*replacementWeeks))
	if err != nil {
		return nil, err
	}
	got := booking.ProposeSeries(slots, booking.SeriesRequest{
		Loc: sc.loc, Count: 1, PerWeek: int(cp.FrequencyPerWeek), Weekdays: goWeekdays(cp.PreferredWeekdays),
		WindowStart: winOr(win, 0), WindowEnd: winOr(win, 1), From: from, MinGapDays: minGapDays(int(cp.FrequencyPerWeek)), Taken: taken,
	})
	if len(got) == 0 {
		return nil, nil
	}
	t := got[0].Start.In(sc.loc)
	return &Replacement{PlanID: cp.ID, Date: t.Format("2006-01-02"), Time: t.Format("15:04"), Start: t}, nil
}

func winOr(w *[2]booking.LocalTime, i int) booking.LocalTime {
	if w == nil {
		return 0
	}
	return w[i]
}

// AddSession planifie une séance de plus dans une série en cours (remplacement
// d'une séance annulée, ou séance manquante). Refusée si la série est complète.
func (s *CarePlans) AddSession(ctx context.Context, a Actor, planID uuid.UUID, date, clock string) (sqlcgen.Appointment, error) {
	var out sqlcgen.Appointment
	err := store.WithTx(ctx, s.Pool, func(q *sqlcgen.Queries, tx pgx.Tx) error {
		cp, err := q.LockCarePlan(ctx, sqlcgen.LockCarePlanParams{ID: planID, PracticeID: a.PracticeID})
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("verrou série: %w", err)
		}
		_, v, err := s.load(ctx, q, a, planID)
		if err != nil {
			return err
		}
		if !v.NeedsMoreSessions {
			return domain.ErrValidation{Field: "lines", Msg: "Cette série n'est plus en cours ou toutes ses séances sont déjà planifiées."}
		}
		sc, err := staffSlotContext(ctx, q, a.PracticeID, cp.PractitionerID, cp.AppointmentTypeID)
		if err != nil {
			return err
		}
		lines, errs := parseLines([]LineInput{{Date: date, Time: clock}}, sc.loc)
		if len(lines) == 0 && len(errs) == 0 {
			errs = append(errs, domain.ErrValidation{Field: "lines", Msg: "Date et heure requises."})
		}
		errs = append(errs, validateLines(ctx, q, sc, s.Clock.Now(), lines)...)
		if len(errs) > 0 {
			return errs
		}
		// La visite à domicile garde l'adresse de la dernière séance de la série.
		var address string
		rows, err := q.ListCarePlanAppointments(ctx, sqlcgen.ListCarePlanAppointmentsParams{CarePlanID: pgtype.UUID{Bytes: planID, Valid: true}, PracticeID: a.PracticeID})
		if err != nil {
			return fmt.Errorf("séances de la série: %w", err)
		}
		for _, r := range rows {
			if r.Address.String != "" {
				address = r.Address.String
			}
		}
		if sc.typ.AtHome && address == "" {
			return domain.ErrValidation{Field: "address", Msg: "L'adresse de la visite à domicile est introuvable : planifiez la séance depuis l'agenda."}
		}
		ids, err := insertSessions(ctx, q, a, sc, cp.PatientID, planID, address, lines)
		if err != nil {
			return err
		}
		if out, err = q.GetAppointment(ctx, sqlcgen.GetAppointmentParams{ID: ids[0], PracticeID: a.PracticeID}); err != nil {
			return fmt.Errorf("lecture rendez-vous: %w", err)
		}
		if err := auditPlan(ctx, q, a, "care_plan.session_added", planID, map[string]string{"appointment": ids[0].String()}); err != nil {
			return err
		}
		// Séance ajoutée (remplacement) : le patient reçoit sa date, comme pour un RDV du cabinet.
		return s.notifier().booked(ctx, tx, q, out, "")
	})
	if err != nil {
		return sqlcgen.Appointment{}, err
	}
	s.emit(ctx, Event{Kind: EventAppointmentCreated, PracticeID: a.PracticeID, AppointmentID: out.ID})
	return out, nil
}

// SetStatus clôt (completed), abandonne (cancelled) ou rouvre (active) une
// série. Les rendez-vous déjà planifiés ne sont pas modifiés.
func (s *CarePlans) SetStatus(ctx context.Context, a Actor, planID uuid.UUID, status string) (sqlcgen.CarePlan, error) {
	if _, ok := planStatusLabels[status]; !ok {
		return sqlcgen.CarePlan{}, domain.ErrValidation{Field: "status", Msg: "Statut de série invalide."}
	}
	var out sqlcgen.CarePlan
	err := store.WithTx(ctx, s.Pool, func(q *sqlcgen.Queries, _ pgx.Tx) error {
		var err error
		out, err = q.SetCarePlanStatus(ctx, sqlcgen.SetCarePlanStatusParams{ID: planID, PracticeID: a.PracticeID, Status: status})
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("statut de la série: %w", err)
		}
		return auditPlan(ctx, q, a, "care_plan.status", planID, map[string]string{"status": status})
	})
	return out, err
}
