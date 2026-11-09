package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"doctolibre/internal/booking"
	"doctolibre/internal/domain"
	"doctolibre/internal/store"
	"doctolibre/internal/store/sqlcgen"
)

// Auto-planification de la série par le patient (T071). Le jeton de série (/s/{jeton}) est
// la seule authentification : le lien a été envoyé à un contact déjà connu du cabinet.

// maxSelfLines borne les lignes relues d'un formulaire public (la série compte au plus 100 séances).
const maxSelfLines = 100

// planByToken retrouve la série d'un lien ; inconnu, falsifié ou révoqué : ErrNotFound.
func planByToken(ctx context.Context, q *sqlcgen.Queries, token string) (sqlcgen.CarePlan, error) {
	if token == "" || len(token) > 128 {
		return sqlcgen.CarePlan{}, domain.ErrNotFound
	}
	cp, err := q.GetCarePlanByTokenHash(ctx, pgtype.Text{String: ManageTokenHash(token), Valid: true})
	if errors.Is(err, pgx.ErrNoRows) {
		return sqlcgen.CarePlan{}, domain.ErrNotFound
	}
	if err != nil {
		return sqlcgen.CarePlan{}, fmt.Errorf("lecture série: %w", err)
	}
	return cp, nil
}

// selfScheduleOpen : la série ouvre la planification au patient (réglage du cabinet, case de la
// série, série en cours, praticien et motif actifs).
func selfScheduleOpen(cp sqlcgen.CarePlan, pc sqlcgen.Practice, pr sqlcgen.Practitioner, typ sqlcgen.AppointmentType) bool {
	return pc.CarePlanSelfScheduling && cp.PatientSelfSchedule && cp.Status == planStatusActive && pr.Active && typ.Active
}

// seriesAddress est l'adresse des visites à domicile d'une série : celle de sa dernière séance
// non annulée (addrs, chronologique, "" pour une séance sans adresse), à défaut la fiche du
// patient. "" = inconnue (planification en ligne refusée).
func seriesAddress(addrs []string, patient sqlcgen.Patient) string {
	for i := len(addrs) - 1; i >= 0; i-- {
		if addrs[i] != "" {
			return addrs[i]
		}
	}
	return strings.TrimSpace(patient.Address.String)
}

// seriesSlotContext prépare le calcul de créneaux EN LIGNE d'une série : préavis, horizon,
// fermetures et jours fériés du cabinet. Le motif est accepté même s'il n'est pas réservable en
// ligne (le praticien l'a choisi pour cette série).
func seriesSlotContext(ctx context.Context, q *sqlcgen.Queries, cp sqlcgen.CarePlan) (slotContext, error) {
	pr, err := q.GetActivePractitioner(ctx, cp.PractitionerID)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && pr.PracticeID != cp.PracticeID) {
		return slotContext{}, domain.ErrNotFound
	}
	if err != nil {
		return slotContext{}, fmt.Errorf("lecture praticien: %w", err)
	}
	pc, err := q.GetPractice(ctx, cp.PracticeID)
	if err != nil {
		return slotContext{}, fmt.Errorf("lecture cabinet: %w", err)
	}
	typ, err := q.GetAppointmentType(ctx, sqlcgen.GetAppointmentTypeParams{ID: cp.AppointmentTypeID, PracticeID: cp.PracticeID})
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && !typ.Active) {
		return slotContext{}, domain.ErrNotFound
	}
	if err != nil {
		return slotContext{}, fmt.Errorf("lecture motif: %w", err)
	}
	loc, err := time.LoadLocation(pc.Timezone)
	if err != nil {
		return slotContext{}, fmt.Errorf("fuseau %q: %w", pc.Timezone, err)
	}
	if !selfScheduleOpen(cp, pc, pr, typ) {
		return slotContext{}, domain.ErrNotFound
	}
	return slotContext{pr: pr, pc: pc, typ: typ, loc: loc}, nil
}

// SelfInput est la saisie du formulaire de planification (chaînes brutes).
type SelfInput struct {
	Count                  string
	Weekdays               []string // "0" (lundi) à "6" (dimanche)
	WindowStart, WindowEnd string   // "HH:MM", les deux vides = pas de contrainte
}

type selfPrefs struct {
	count            int
	weekdays         []int16
	hasWindow        bool
	winStart, winEnd booking.LocalTime
}

func (p selfPrefs) window() *[2]booking.LocalTime {
	if !p.hasWindow {
		return nil
	}
	return &[2]booking.LocalTime{p.winStart, p.winEnd}
}

func parseSelfInput(in SelfInput, remaining int) (selfPrefs, error) {
	var errs domain.ValidationErrors
	add := func(field, msg string) { errs = append(errs, domain.ErrValidation{Field: field, Msg: msg}) }
	var p selfPrefs
	n, err := strconv.Atoi(strings.TrimSpace(in.Count))
	if err != nil || n < 1 || n > remaining {
		add("count", fmt.Sprintf("Choisissez un nombre de séances entre 1 et %d.", remaining))
	}
	p.count = n
	seen := map[int]bool{}
	for _, w := range in.Weekdays {
		d, err := strconv.Atoi(strings.TrimSpace(w))
		if err != nil || d < 0 || d > 6 {
			add("weekdays", "Jour de la semaine invalide.")
			break
		}
		if !seen[d] {
			seen[d] = true
			p.weekdays = append(p.weekdays, int16(d)) //nolint:gosec // 0..6
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
	if len(errs) > 0 {
		return selfPrefs{}, errs
	}
	return p, nil
}

// planState regroupe la série, ce qu'elle compte déjà et le contexte de créneaux.
type planState struct {
	cp        sqlcgen.CarePlan
	sc        slotContext
	patient   sqlcgen.Patient
	rows      []sqlcgen.ListCarePlanAppointmentsRow
	taken     []booking.Date // séances comptées (honorées ou à venir)
	remaining int
	address   string
}

// loadPlanState lit la série de q et vérifie qu'elle ouvre la planification (sinon ErrNotFound).
func loadPlanState(ctx context.Context, q *sqlcgen.Queries, cp sqlcgen.CarePlan) (planState, error) {
	sc, err := seriesSlotContext(ctx, q, cp)
	if err != nil {
		return planState{}, err
	}
	pat, err := q.GetPatient(ctx, sqlcgen.GetPatientParams{ID: cp.PatientID, PracticeID: cp.PracticeID})
	if errors.Is(err, pgx.ErrNoRows) {
		return planState{}, domain.ErrNotFound
	}
	if err != nil {
		return planState{}, fmt.Errorf("lecture patient: %w", err)
	}
	rows, err := q.ListCarePlanAppointments(ctx, sqlcgen.ListCarePlanAppointmentsParams{CarePlanID: pgtype.UUID{Bytes: cp.ID, Valid: true}, PracticeID: cp.PracticeID})
	if err != nil {
		return planState{}, fmt.Errorf("séances de la série: %w", err)
	}
	st := planState{cp: cp, sc: sc, patient: pat, rows: rows}
	for _, r := range rows {
		if r.Status == "done" || r.Status == "booked" {
			st.taken = append(st.taken, booking.DateOf(r.StartsAt.In(sc.loc)))
		}
	}
	st.remaining = max(int(cp.SessionsPrescribed)-len(st.taken), 0)
	var addrs []string
	for _, r := range rows {
		if r.Status != "cancelled" {
			addrs = append(addrs, r.Address.String)
		}
	}
	st.address = seriesAddress(addrs, pat)
	if st.remaining == 0 || (sc.typ.AtHome && st.address == "") {
		return planState{}, domain.ErrNotFound
	}
	return st, nil
}

// SelfProposal est une proposition de séances faite au patient (dans le fuseau du cabinet).
type SelfProposal struct {
	Lines  []time.Time
	Wanted int
}

// SelfView décrit ce que la page publique peut proposer pour une série.
type SelfView struct {
	PlanID    uuid.UUID
	Remaining int
	PerWeek   int
	Weekdays  []string // "0".."6" préremplis depuis la série
	// WindowStart, WindowEnd : "HH:MM" ("" = aucune plage).
	WindowStart, WindowEnd string
	Instructions           string
	AckRequired            bool
	Loc                    *time.Location // fuseau du cabinet : les heures affichées sont à cette heure murale
}

func hhmm(t pgtype.Time) string {
	if !t.Valid {
		return ""
	}
	m := t.Microseconds / int64(time.Minute/time.Microsecond)
	return fmt.Sprintf("%02d:%02d", m/60, m%60)
}

// SelfSchedule retourne ce qu'il faut pour afficher la planification d'une série, ou
// domain.ErrNotFound si elle n'est pas ouverte au patient (jeton révoqué, réglage ou case
// désactivés, série terminée, plus de séance à planifier, visite à domicile sans adresse).
func (b *Booking) SelfSchedule(ctx context.Context, token string) (SelfView, error) {
	q := sqlcgen.New(b.Pool)
	cp, err := planByToken(ctx, q, token)
	if err != nil {
		return SelfView{}, err
	}
	st, err := loadPlanState(ctx, q, cp)
	if err != nil {
		return SelfView{}, err
	}
	v := SelfView{
		PlanID: cp.ID, Loc: st.sc.loc, Remaining: st.remaining, PerWeek: int(cp.FrequencyPerWeek),
		WindowStart: hhmm(cp.PreferredTimeStart), WindowEnd: hhmm(cp.PreferredTimeEnd),
		Instructions: st.sc.typ.PatientInstructions, AckRequired: st.sc.typ.InstructionsAckRequired && st.sc.typ.PatientInstructions != "",
	}
	for _, w := range cp.PreferredWeekdays {
		v.Weekdays = append(v.Weekdays, strconv.Itoa(int(w)))
	}
	return v, nil
}

// guardOnline applique le garde-fou d'absences répétées (T054) : ce parcours public ne doit
// pas le contourner.
func (b *Booking) guardOnline(ctx context.Context, q *sqlcgen.Queries, st planState) error {
	blocked, err := onlineBookingBlocked(ctx, q, st.sc.pc, st.cp.PatientID, b.Clock.Now())
	if err != nil {
		return err
	}
	if blocked {
		slog.InfoContext(ctx, "planification de série refusée (absences répétées)",
			slog.String("practice_id", st.cp.PracticeID.String()), slog.String("care_plan_id", st.cp.ID.String()))
		return domain.ErrOnlineBookingBlocked
	}
	return nil
}

// ProposeSelf propose jusqu'à in.Count séances selon les préférences données, avec les créneaux en
// ligne du praticien (préavis, horizon, fermetures et fériés du cabinet). Les séances déjà posées
// comptent pour l'écart minimal et le quota hebdomadaire. Ne crée rien.
func (b *Booking) ProposeSelf(ctx context.Context, token string, in SelfInput) (SelfProposal, error) {
	q := sqlcgen.New(b.Pool)
	cp, err := planByToken(ctx, q, token)
	if err != nil {
		return SelfProposal{}, err
	}
	st, err := loadPlanState(ctx, q, cp)
	if err != nil {
		return SelfProposal{}, err
	}
	prefs, err := parseSelfInput(in, st.remaining)
	if err != nil {
		return SelfProposal{}, err
	}
	if err := b.guardOnline(ctx, q, st); err != nil {
		return SelfProposal{}, err
	}
	now := b.Clock.Now()
	got, err := st.sc.proposeSessions(ctx, q, now, booking.DateOf(now.In(st.sc.loc)), prefs.count, int(cp.FrequencyPerWeek), prefs.weekdays, prefs.window(), st.taken)
	if err != nil {
		return SelfProposal{}, err
	}
	out := SelfProposal{Wanted: prefs.count}
	for _, s := range got {
		out.Lines = append(out.Lines, s.Start.In(st.sc.loc))
	}
	return out, nil
}

// NextSelf remplace la séance current[idx] par le premier créneau compatible après elle (un jour
// sans autre séance, écart minimal respecté). Sans créneau, la proposition est rendue inchangée
// et found vaut false.
func (b *Booking) NextSelf(ctx context.Context, token string, in SelfInput, current []time.Time, idx int) (_ SelfProposal, found bool, _ error) {
	q := sqlcgen.New(b.Pool)
	cp, err := planByToken(ctx, q, token)
	if err != nil {
		return SelfProposal{}, false, err
	}
	st, err := loadPlanState(ctx, q, cp)
	if err != nil {
		return SelfProposal{}, false, err
	}
	if idx < 0 || idx >= len(current) || len(current) > st.remaining {
		return SelfProposal{}, false, domain.ErrValidation{Field: "lines", Msg: "Séance inconnue."}
	}
	// Seuls les jours et la plage comptent ici : le nombre vient des lignes en cours.
	prefs, err := parseSelfInput(SelfInput{Count: "1", Weekdays: in.Weekdays, WindowStart: in.WindowStart, WindowEnd: in.WindowEnd}, st.remaining)
	if err != nil {
		return SelfProposal{}, false, err
	}
	if err := b.guardOnline(ctx, q, st); err != nil {
		return SelfProposal{}, false, err
	}
	// Premier créneau valide après l'actuel (même jour possible) : plage horaire respectée, et écart
	// minimal et quota hebdomadaire vérifiés avec les AUTRES séances (existantes et proposées).
	others := append([]booking.Date(nil), st.taken...)
	for i, t := range current {
		if i != idx {
			others = append(others, booking.DateOf(t.In(st.sc.loc)))
		}
	}
	now := b.Clock.Now()
	from := current[idx]
	if from.Before(now) {
		from = now
	}
	slots, err := st.sc.slots(ctx, q, now, from, from.AddDate(0, 0, 7*planSearchWeeks))
	if err != nil {
		return SelfProposal{}, false, err
	}
	out := SelfProposal{Wanted: len(current)}
	for _, t := range current {
		out.Lines = append(out.Lines, t.In(st.sc.loc))
	}
	for _, sl := range slots {
		if !sl.Start.After(current[idx]) || !prefs.inWindow(sl, st.sc.loc) {
			continue
		}
		if checkSeriesRules(others, []time.Time{sl.Start}, st.sc.loc, int(cp.FrequencyPerWeek)) != nil {
			continue
		}
		out.Lines[idx] = sl.Start.In(st.sc.loc)
		found = true
		break
	}
	sort.Slice(out.Lines, func(i, j int) bool { return out.Lines[i].Before(out.Lines[j]) })
	return out, found, nil
}

// SelfConfirm est la validation d'une proposition.
type SelfConfirm struct {
	Starts []time.Time
	// InstructionsAck : le patient a coché « J'ai lu les consignes » (exigé si le motif le demande).
	InstructionsAck bool
}

// ConfirmSelf crée toutes les séances ou aucune. Sous verrou de la ligne de série, le quota est
// recompté : jamais plus de séances que prescrit, même avec deux onglets ou un double clic.
// Chaque créneau est revalidé (préavis, horizon, fermetures, fériés, chevauchements) ; un créneau
// pris renvoie *LineConflict (Is(domain.ErrSlotTaken)) et rien n'est créé. Un seul message
// récapitulatif part au patient.
func (b *Booking) ConfirmSelf(ctx context.Context, token string, in SelfConfirm) (created int, _ error) {
	if len(in.Starts) == 0 {
		return 0, domain.ErrValidation{Field: "lines", Msg: "Aucune séance à planifier."}
	}
	if len(in.Starts) > maxSelfLines {
		return 0, domain.ErrValidation{Field: "lines", Msg: "Trop de séances."}
	}
	now := b.Clock.Now()
	starts := append([]time.Time(nil), in.Starts...)
	sort.Slice(starts, func(i, j int) bool { return starts[i].Before(starts[j]) })
	for i := 1; i < len(starts); i++ {
		if starts[i].Equal(starts[i-1]) {
			return 0, domain.ErrValidation{Field: "lines", Msg: "Deux séances ne peuvent pas avoir le même horaire."}
		}
	}

	var appts []sqlcgen.Appointment
	var planID, practiceID uuid.UUID
	err := store.WithTx(ctx, b.Pool, func(q *sqlcgen.Queries, tx pgx.Tx) error {
		cp, err := planByToken(ctx, q, token)
		if err != nil {
			return err
		}
		// Verrou : les confirmations concurrentes de la même série s'exécutent l'une après l'autre.
		// La ligne relue sous verrou fait foi (révocation ou changement de réglage entre-temps).
		if cp, err = q.LockCarePlan(ctx, sqlcgen.LockCarePlanParams{ID: cp.ID, PracticeID: cp.PracticeID}); err != nil {
			return fmt.Errorf("verrou de la série: %w", err)
		}
		if !cp.TokenHash.Valid || cp.TokenHash.String != ManageTokenHash(token) {
			return domain.ErrNotFound
		}
		st, err := loadPlanState(ctx, q, cp) // recompte sous verrou
		if err != nil {
			return err
		}
		planID, practiceID = cp.ID, cp.PracticeID
		if len(starts) > st.remaining {
			return domain.ErrValidation{Field: "lines", Msg: fmt.Sprintf("Il ne reste que %d séance(s) à planifier dans cette série.", st.remaining)}
		}
		if err := checkSeriesRules(st.taken, starts, st.sc.loc, int(cp.FrequencyPerWeek)); err != nil {
			return err
		}
		if err := checkInstructionsAck(st.sc.typ, in.InstructionsAck); err != nil {
			return err
		}
		if err := b.guardOnline(ctx, q, st); err != nil {
			return err
		}
		bb, ba := st.sc.buffers()
		address := ""
		if st.sc.typ.AtHome {
			address = st.address
		}
		tokens := map[uuid.UUID]string{}
		for i, start := range starts {
			slot, err := st.sc.check(ctx, q, now, start)
			if errors.Is(err, domain.ErrSlotTaken) {
				return &LineConflict{Row: i + 1, Start: start}
			}
			if err != nil {
				return err
			}
			id, tok, hash, err := b.ManageKey.mint()
			if err != nil {
				return err
			}
			ap, err := store.CreateAppointment(ctx, q, store.NewAppointment{
				ID: id, ManageTokenHash: hash, PracticeID: cp.PracticeID, PractitionerID: st.sc.pr.ID, PatientID: cp.PatientID,
				AppointmentTypeID: st.sc.typ.ID, Start: slot.Start, End: slot.End, BufferBefore: bb, BufferAfter: ba,
				Address: address, Source: "online",
			})
			if errors.Is(err, domain.ErrSlotTaken) {
				return &LineConflict{Row: i + 1, Start: start}
			}
			if err != nil {
				return err
			}
			if err := q.AttachAppointmentToCarePlan(ctx, sqlcgen.AttachAppointmentToCarePlanParams{
				ID: ap.ID, PracticeID: cp.PracticeID, CarePlanID: pgtype.UUID{Bytes: cp.ID, Valid: true},
			}); err != nil {
				return fmt.Errorf("rattachement à la série: %w", err)
			}
			ap.CarePlanID = pgtype.UUID{Bytes: cp.ID, Valid: true}
			tokens[ap.ID] = tok
			appts = append(appts, ap)
		}
		if err := auditSelfSchedule(ctx, q, cp, len(appts)); err != nil {
			return err
		}
		// Un seul message récapitulatif (jamais une confirmation par séance).
		if len(appts) == 1 {
			return b.notifier().booked(ctx, tx, q, appts[0], tokens[appts[0].ID])
		}
		planToken, err := b.ManageKey.ensurePlanToken(ctx, q, cp)
		if err != nil {
			return err
		}
		return b.notifier().seriesBooked(ctx, tx, q, appts, planToken)
	})
	if err != nil {
		return 0, err
	}
	for _, ap := range appts {
		if b.OnCreated != nil {
			b.OnCreated("online")
		}
		b.emit(ctx, Event{Kind: EventAppointmentCreated, PracticeID: practiceID, AppointmentID: ap.ID})
	}
	slog.InfoContext(ctx, "séances planifiées par le patient",
		slog.String("care_plan_id", planID.String()), slog.Int("sessions", len(appts)))
	return len(appts), nil
}

// auditSelfSchedule journalise la création par le patient : l'id de la série, jamais de nom.
func auditSelfSchedule(ctx context.Context, q *sqlcgen.Queries, cp sqlcgen.CarePlan, n int) error {
	if err := q.CreateAuditLog(ctx, sqlcgen.CreateAuditLogParams{
		PracticeID: pgtype.UUID{Bytes: cp.PracticeID, Valid: true}, Actor: "patient", Action: "care_plan.self_scheduled",
		Entity: "care_plan", EntityID: pgtype.UUID{Bytes: cp.ID, Valid: true}, Meta: []byte(fmt.Sprintf(`{"sessions":"%d"}`, n)),
	}); err != nil {
		return fmt.Errorf("journal d'audit: %w", err)
	}
	return nil
}

// ParseSelfStarts lit les débuts de séances d'un formulaire (RFC 3339) ; une valeur illisible
// est une erreur de saisie.
func ParseSelfStarts(values []string) ([]time.Time, error) {
	if len(values) > maxSelfLines {
		return nil, domain.ErrValidation{Field: "lines", Msg: "Trop de séances."}
	}
	out := make([]time.Time, 0, len(values))
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v == "" || strings.ContainsFunc(v, unicode.IsControl) {
			continue
		}
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			return nil, domain.ErrValidation{Field: "lines", Msg: "Séance illisible : recommencez la proposition."}
		}
		out = append(out, t)
	}
	return out, nil
}

// LinkURL est l'adresse du lien de série à transmettre au patient (relative sans BaseURL).
func (s *CarePlans) LinkURL(cp sqlcgen.CarePlan) string {
	t := s.ManageKey.PlanToken(cp.ID, int(cp.TokenVersion))
	if u := planURL(s.BaseURL, t); u != "" {
		return u
	}
	return "/s/" + t
}

// civilDay numérote les jours civils (indépendant du fuseau et du DST) ; civilWeek les semaines
// lundi-dimanche (le jour 0, 1970-01-01, est un jeudi). Même numérotation que booking.ProposeSeries.
func civilDay(y int, m time.Month, d int) int {
	return int(time.Date(y, m, d, 12, 0, 0, 0, time.UTC).Unix() / 86400)
}

func civilWeek(day int) int { return (day + 3) / 7 }

// checkSeriesRules revalide, sur la saisie reçue (donc forgeable), les règles de la série que la
// proposition respecte : écart minimal entre deux séances et quota hebdomadaire, séances déjà
// comptées de la série comprises. Seules les séances nouvelles sont mises en cause.
func checkSeriesRules(existing []booking.Date, starts []time.Time, loc *time.Location, perWeek int) error {
	gap := minGapDays(perWeek)
	var days []int
	perWk := map[int]int{}
	for _, d := range existing {
		n := civilDay(d.Year, d.Month, d.Day)
		perWk[civilWeek(n)]++
	}
	var all []int // existantes puis nouvelles, pour l'écart
	for _, d := range existing {
		all = append(all, civilDay(d.Year, d.Month, d.Day))
	}
	for _, st := range starts {
		l := st.In(loc)
		n := civilDay(l.Year(), l.Month(), l.Day())
		for _, o := range all {
			if o-n < gap && n-o < gap {
				return domain.ErrValidation{Field: "lines", Msg: fmt.Sprintf("Les séances doivent être espacées d'au moins %d jour(s) : recommencez la proposition.", gap)}
			}
		}
		all = append(all, n)
		days = append(days, n)
	}
	for _, n := range days {
		perWk[civilWeek(n)]++
		if perWk[civilWeek(n)] > perWeek {
			return domain.ErrValidation{Field: "lines", Msg: fmt.Sprintf("Cette série compte au plus %d séance(s) par semaine : recommencez la proposition.", perWeek)}
		}
	}
	return nil
}

// inWindow : le soin tient dans la plage horaire souhaitée (heure murale du cabinet) ; sans plage, toujours.
func (p selfPrefs) inWindow(s booking.Slot, loc *time.Location) bool {
	if !p.hasWindow {
		return true
	}
	l := s.Start.In(loc)
	start := l.Hour()*60 + l.Minute()
	end := start + int(s.End.Sub(s.Start)/time.Minute)
	return start >= int(p.winStart) && end <= int(p.winEnd)
}
