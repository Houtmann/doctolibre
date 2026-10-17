package service

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"doctolibre/internal/domain"
	"doctolibre/internal/store"
	"doctolibre/internal/store/sqlcgen"
)

// weekdayNames : 0 = lundi, comme availability_rule.weekday.
var weekdayNames = [7]string{"lundi", "mardi", "mercredi", "jeudi", "vendredi", "samedi", "dimanche"}

// WeekdayName retourne le nom français du jour (0 = lundi).
func WeekdayName(w int) string {
	if w < 0 || w > 6 {
		return ""
	}
	return weekdayNames[w]
}

// RuleInput est la saisie du formulaire d'ajout de plages : la même plage
// horaire est ajoutée à chaque jour coché.
type RuleInput struct {
	Weekdays []string // "0".."6"
	Start    string   // "08:00"
	End      string   // "12:00"
	TypeIDs  []string // vide = non précisé (invalide s'il existe des motifs actifs)
}

// RuleView est une plage affichée dans la grille.
type RuleView struct {
	ID         uuid.UUID
	Weekday    int
	Start, End string
	AllTypes   bool
	TypeNames  []string
}

// DayView regroupe les plages d'un jour.
type DayView struct {
	Weekday int
	Rules   []RuleView
}

// AvailabilityView est la grille hebdomadaire et les motifs sélectionnables.
type AvailabilityView struct {
	Days  [7]DayView
	Types []sqlcgen.AppointmentType // motifs actifs
}

// Availability retourne la grille lundi→dimanche du praticien connecté.
func (s *Practice) Availability(ctx context.Context, a Actor) (AvailabilityView, error) {
	pr, err := s.configPractitioner(ctx, a)
	if err != nil {
		return AvailabilityView{}, err
	}
	q := sqlcgen.New(s.Pool)
	rules, err := q.ListAvailabilityRules(ctx, pr.ID)
	if err != nil {
		return AvailabilityView{}, fmt.Errorf("liste des plages: %w", err)
	}
	all, err := s.AppointmentTypes(ctx, a)
	if err != nil {
		return AvailabilityView{}, err
	}
	names := map[uuid.UUID]string{}
	var v AvailabilityView
	for _, t := range all {
		names[t.ID] = t.Name
		if t.Active {
			v.Types = append(v.Types, t)
		}
	}
	for i := range v.Days {
		v.Days[i].Weekday = i
	}
	for _, r := range rules {
		w := int(r.Weekday)
		if w < 0 || w > 6 {
			continue
		}
		rv := RuleView{ID: r.ID, Weekday: w, Start: formatClock(r.StartTime), End: formatClock(r.EndTime), AllTypes: r.AppointmentTypeIds == nil}
		for _, id := range r.AppointmentTypeIds {
			if n, ok := names[id]; ok {
				rv.TypeNames = append(rv.TypeNames, n)
			}
		}
		v.Days[w].Rules = append(v.Days[w].Rules, rv)
	}
	return v, nil
}

// AddRules ajoute la plage à chaque jour demandé, à partir d'aujourd'hui (jour du
// cabinet), sans fin. Tout ou rien : un chevauchement annule l'ensemble.
func (s *Practice) AddRules(ctx context.Context, a Actor, in RuleInput) error {
	var errs domain.ValidationErrors
	add := func(field, msg string) { errs = append(errs, domain.ErrValidation{Field: field, Msg: msg}) }

	var days []int
	seen := map[int]bool{}
	for _, w := range in.Weekdays {
		n, ok := parseIntRange(w, 0, 6)
		if !ok {
			add("weekday", "Jour de la semaine invalide.")
			break
		}
		if !seen[n] {
			seen[n] = true
			days = append(days, n)
		}
	}
	if len(days) == 0 && len(errs) == 0 {
		add("weekday", "Choisissez au moins un jour.")
	}
	start, okS := parseClock(in.Start)
	if !okS {
		add("start", "L'heure de début doit être au format HH:MM.")
	}
	end, okE := parseClock(in.End)
	if !okE {
		add("end", "L'heure de fin doit être au format HH:MM.")
	}
	if okS && okE && end <= start {
		add("end", "L'heure de fin doit être après l'heure de début.")
	}
	var typeIDs []uuid.UUID
	for _, id := range in.TypeIDs {
		u, err := uuid.Parse(strings.TrimSpace(id))
		if err != nil {
			add("types", "Motif invalide.")
			break
		}
		typeIDs = append(typeIDs, u)
	}
	if len(errs) > 0 {
		return errs
	}

	pr, err := s.configPractitioner(ctx, a)
	if err != nil {
		return err
	}
	loc, err := s.location(ctx, a.PracticeID)
	if err != nil {
		return err
	}
	now := s.Clock.Now().In(loc)
	validFrom := pgtype.Date{Time: time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC), Valid: true}

	err = store.WithTx(ctx, s.Pool, func(q *sqlcgen.Queries, _ pgx.Tx) error {
		// Motifs : tous les motifs actifs cochés = « tous » (NULL, inclut les futurs motifs).
		var restrict []uuid.UUID
		active, err := q.ListAppointmentTypes(ctx, sqlcgen.ListAppointmentTypesParams{PracticeID: a.PracticeID, PageSize: pageSize})
		if err != nil {
			return fmt.Errorf("liste des motifs: %w", err)
		}
		if len(active) > 0 {
			ok := map[uuid.UUID]bool{}
			for _, t := range active {
				ok[t.ID] = true
			}
			picked := map[uuid.UUID]bool{}
			for _, id := range typeIDs {
				if !ok[id] {
					return domain.ErrValidation{Field: "types", Msg: "Motif inconnu ou désactivé."}
				}
				picked[id] = true
			}
			switch {
			case len(picked) == 0:
				return domain.ErrValidation{Field: "types", Msg: "Choisissez au moins un motif."}
			case len(picked) < len(active):
				for _, t := range active {
					if picked[t.ID] {
						restrict = append(restrict, t.ID)
					}
				}
			}
		}
		existing, err := q.ListAvailabilityRules(ctx, pr.ID)
		if err != nil {
			return fmt.Errorf("liste des plages: %w", err)
		}
		for _, w := range days {
			for _, r := range existing {
				if int(r.Weekday) != w || (r.ValidUntil.Valid && r.ValidUntil.Time.Before(validFrom.Time)) {
					continue
				}
				rs, re := int64(r.StartTime.Microseconds), int64(r.EndTime.Microseconds)
				if start*1e6 < re && rs < end*1e6 {
					return overlapError(w, in.Start, in.End, r)
				}
			}
			_, err := q.CreateAvailabilityRule(ctx, sqlcgen.CreateAvailabilityRuleParams{
				PractitionerID: pr.ID, Weekday: int16(w), //nolint:gosec // 0..6
				StartTime: pgtype.Time{Microseconds: start * 1e6, Valid: true},
				EndTime:   pgtype.Time{Microseconds: end * 1e6, Valid: true},
				ValidFrom: validFrom, AppointmentTypeIds: restrict,
			})
			if store.IsExclusionViolation(err) {
				return domain.ErrValidation{Field: "start", Msg: fmt.Sprintf("%s : la plage %s–%s chevauche une plage existante.", Capitalize(WeekdayName(w)), in.Start, in.End)}
			}
			if err != nil {
				return fmt.Errorf("création plage: %w", err)
			}
		}
		return nil
	})
	return err
}

func overlapError(weekday int, start, end string, existing sqlcgen.AvailabilityRule) error {
	return domain.ErrValidation{Field: "start", Msg: fmt.Sprintf("%s : la plage %s–%s chevauche la plage existante %s–%s.",
		Capitalize(WeekdayName(weekday)), start, end, formatClock(existing.StartTime), formatClock(existing.EndTime))}
}

// DeleteRule supprime une plage du praticien connecté (sans effet si elle n'existe pas).
func (s *Practice) DeleteRule(ctx context.Context, a Actor, id uuid.UUID) error {
	pr, err := s.configPractitioner(ctx, a)
	if err != nil {
		return err
	}
	if err := sqlcgen.New(s.Pool).DeleteAvailabilityRule(ctx, sqlcgen.DeleteAvailabilityRuleParams{ID: id, PractitionerID: pr.ID}); err != nil {
		return fmt.Errorf("suppression plage: %w", err)
	}
	return nil
}

// ClosureInput est la saisie d'une absence. Sans heures : journées entières,
// de StartDate à EndDate incluse (EndDate vide = StartDate).
type ClosureInput struct {
	StartDate, StartTime string // "2026-10-20", "08:00"
	EndDate, EndTime     string
	Reason               string
}

// ClosureView est une absence à venir, dans le fuseau du cabinet.
type ClosureView struct {
	ID         uuid.UUID
	Start, End time.Time // End exclu
	AllDay     bool
	LastDay    time.Time // dernier jour couvert (journées entières)
	Reason     string
}

// Closures liste les absences à venir (non terminées) du praticien connecté.
func (s *Practice) Closures(ctx context.Context, a Actor) ([]ClosureView, error) {
	pr, err := s.configPractitioner(ctx, a)
	if err != nil {
		return nil, err
	}
	loc, err := s.location(ctx, a.PracticeID)
	if err != nil {
		return nil, err
	}
	rows, err := sqlcgen.New(s.Pool).ListUpcomingClosures(ctx, sqlcgen.ListUpcomingClosuresParams{
		PractitionerID: pr.ID, After: s.Clock.Now(), PageSize: pageSize,
	})
	if err != nil {
		return nil, fmt.Errorf("liste des absences: %w", err)
	}
	out := make([]ClosureView, 0, len(rows))
	for _, r := range rows {
		st, en := r.During.Lower.Time.In(loc), r.During.Upper.Time.In(loc)
		v := ClosureView{ID: r.ID, Start: st, End: en, Reason: r.Reason}
		if isMidnight(st) && isMidnight(en) {
			v.AllDay, v.LastDay = true, en.AddDate(0, 0, -1)
		}
		out = append(out, v)
	}
	return out, nil
}

func isMidnight(t time.Time) bool { return t.Hour() == 0 && t.Minute() == 0 && t.Second() == 0 }

// closureRange valide la saisie d'une absence et la convertit en plage [from, to)
// dans le fuseau du cabinet. Partagée par AddClosure et l'annulation groupée.
func (s *Practice) closureRange(ctx context.Context, a Actor, in ClosureInput) (from, to time.Time, reason string, err error) {
	var errs domain.ValidationErrors
	add := func(field, msg string) { errs = append(errs, domain.ErrValidation{Field: field, Msg: msg}) }
	reason = strings.TrimSpace(in.Reason)
	if tooLong(reason, 200) {
		add("reason", "Le motif de l'absence est trop long (200 caractères maximum).")
	}
	startDate, err := time.Parse("2006-01-02", strings.TrimSpace(in.StartDate))
	if err != nil {
		add("start_date", "La date de début est requise (format AAAA-MM-JJ).")
	}
	endDate := startDate
	if d := strings.TrimSpace(in.EndDate); d != "" {
		if endDate, err = time.Parse("2006-01-02", d); err != nil {
			add("end_date", "La date de fin doit être au format AAAA-MM-JJ.")
		}
	}
	hasS, hasE := strings.TrimSpace(in.StartTime) != "", strings.TrimSpace(in.EndTime) != ""
	var sMin, eMin int64
	if hasS != hasE {
		add("start_time", "Renseignez les deux heures, ou aucune pour des journées entières.")
	} else if hasS {
		var ok bool
		if sMin, ok = parseClock(in.StartTime); !ok {
			add("start_time", "L'heure de début doit être au format HH:MM.")
		}
		if eMin, ok = parseClock(in.EndTime); !ok {
			add("end_time", "L'heure de fin doit être au format HH:MM.")
		}
	}
	if len(errs) > 0 {
		return time.Time{}, time.Time{}, "", errs
	}
	loc, err := s.location(ctx, a.PracticeID)
	if err != nil {
		return time.Time{}, time.Time{}, "", err
	}
	at := func(d time.Time, sec int64) time.Time {
		return time.Date(d.Year(), d.Month(), d.Day(), int(sec/3600), int(sec/60%60), 0, 0, loc)
	}
	if hasS {
		from, to = at(startDate, sMin), at(endDate, eMin)
	} else {
		from, to = at(startDate, 0), at(endDate, 0).AddDate(0, 0, 1)
	}
	switch {
	case !to.After(from):
		return time.Time{}, time.Time{}, "", domain.ErrValidation{Field: "end_date", Msg: "La fin de l'absence doit être après son début."}
	case !to.After(s.Clock.Now()):
		return time.Time{}, time.Time{}, "", domain.ErrValidation{Field: "start_date", Msg: "L'absence doit se terminer dans le futur."}
	}
	return from, to, reason, nil
}

// AddClosure enregistre une absence (kind « closed »), dans le fuseau du cabinet.
// Elle n'annule aucun rendez-vous (voir Calendar.CancelRange).
func (s *Practice) AddClosure(ctx context.Context, a Actor, in ClosureInput) error {
	from, to, reason, err := s.closureRange(ctx, a, in)
	if err != nil {
		return err
	}
	pr, err := s.configPractitioner(ctx, a)
	if err != nil {
		return err
	}
	if _, err := sqlcgen.New(s.Pool).CreateAvailabilityException(ctx, sqlcgen.CreateAvailabilityExceptionParams{
		PractitionerID: pr.ID, StartsAt: from, EndsAt: to, Kind: "closed", Reason: reason,
	}); err != nil {
		return fmt.Errorf("création absence: %w", err)
	}
	return nil
}

// DeleteClosure supprime une absence du praticien connecté.
func (s *Practice) DeleteClosure(ctx context.Context, a Actor, id uuid.UUID) error {
	pr, err := s.configPractitioner(ctx, a)
	if err != nil {
		return err
	}
	if err := sqlcgen.New(s.Pool).DeleteAvailabilityException(ctx, sqlcgen.DeleteAvailabilityExceptionParams{ID: id, PractitionerID: pr.ID}); err != nil {
		return fmt.Errorf("suppression absence: %w", err)
	}
	return nil
}

// parseClock lit « HH:MM » (00:00–23:59) et retourne les secondes depuis minuit.
func parseClock(s string) (int64, bool) {
	h, m, ok := strings.Cut(strings.TrimSpace(s), ":")
	if !ok || len(h) != 2 || len(m) != 2 {
		return 0, false
	}
	hh, err1 := strconv.Atoi(h)
	mm, err2 := strconv.Atoi(m)
	if err1 != nil || err2 != nil || hh < 0 || hh > 23 || mm < 0 || mm > 59 {
		return 0, false
	}
	return int64(hh*3600 + mm*60), true
}

func formatClock(t pgtype.Time) string {
	sec := t.Microseconds / 1e6
	return fmt.Sprintf("%02d:%02d", sec/3600, sec/60%60)
}

// Capitalize met la première lettre en majuscule.
func Capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
