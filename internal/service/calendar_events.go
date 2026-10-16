package service

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"doctolibre/internal/booking"
	"doctolibre/internal/domain"
	"doctolibre/internal/store/sqlcgen"
)

// MaxEventsDays borne la plage demandée à l'endpoint JSON de l'agenda (jours calendaires).
const MaxEventsDays = 42

// EventsInput est la demande de l'endpoint JSON de l'agenda. Start et End sont
// des dates ISO 8601 : avec décalage (RFC 3339), elles désignent un instant ;
// sans décalage (« 2026-10-19T08:00:00 », « 2026-10-19 08:00 », « 2026-10-19 »),
// une heure murale du fuseau du cabinet.
type EventsInput struct {
	Start, End string
	Selection
}

// AgendaAppt est un RDV de l'agenda. Pas de téléphone, email ni note : seulement
// ce que l'agenda a toujours affiché (nom du patient, motif, statut).
type AgendaAppt struct {
	ID             uuid.UUID
	PractitionerID uuid.UUID
	// Start/End : le soin ; BlockedStart/BlockedEnd : soin + buffers avant/après.
	Start, End               time.Time
	BlockedStart, BlockedEnd time.Time
	Patient                  string
	TypeName                 string
	Status                   string
	StatusName               string
	AtHome                   bool
	TypeColor                string // #rrggbb garanti (défaut si la base contient autre chose)
}

// AgendaClosed est une plage d'indisponibilité d'un praticien (hors horaires,
// absence ou jour férié), à griser.
type AgendaClosed struct {
	PractitionerID uuid.UUID
	Start, End     time.Time
}

// AgendaEvents est la réponse de Events ; toutes les heures sont dans le fuseau du cabinet.
type AgendaEvents struct {
	Appts  []AgendaAppt
	Closed []AgendaClosed
}

// parseAgendaTime lit une date ISO 8601 ; sans décalage elle est interprétée dans loc.
func parseAgendaTime(s string, loc *time.Location) (time.Time, error) {
	s = strings.TrimSpace(s)
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.In(loc), nil
	}
	for _, layout := range []string{"2006-01-02T15:04:05.999999999", "2006-01-02T15:04:05", "2006-01-02T15:04", "2006-01-02 15:04:05", "2006-01-02 15:04", "2006-01-02"} {
		if t, err := time.ParseInLocation(layout, s, loc); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("date illisible %q", s)
}

// AgendaRange retourne les RDV non annulés et les plages d'indisponibilité des
// praticiens demandés sur [Start, End). Le cabinet vient de l'acteur ; un
// praticien inaccessible à l'acteur donne ErrForbidden, d'un autre cabinet
// ErrNotFound. Plage > MaxEventsDays jours ou end <= start : ErrValidation.
func (c *Calendar) AgendaRange(ctx context.Context, a Actor, in EventsInput) (AgendaEvents, error) {
	pc, loc, err := c.practice().practiceAndLocation(ctx, a.PracticeID)
	if err != nil {
		return AgendaEvents{}, err
	}
	start, err := parseAgendaTime(in.Start, loc)
	if err != nil {
		return AgendaEvents{}, domain.ErrValidation{Field: "start", Msg: "Date de début invalide (ISO 8601 attendu)."}
	}
	end, err := parseAgendaTime(in.End, loc)
	if err != nil {
		return AgendaEvents{}, domain.ErrValidation{Field: "end", Msg: "Date de fin invalide (ISO 8601 attendu)."}
	}
	if !end.After(start) {
		return AgendaEvents{}, domain.ErrValidation{Field: "end", Msg: "La fin doit suivre le début."}
	}
	// Jours calendaires (AddDate) : une plage de 42 jours reste valable à travers un changement d'heure.
	if end.After(start.AddDate(0, 0, MaxEventsDays)) {
		return AgendaEvents{}, domain.ErrValidation{Field: "end", Msg: fmt.Sprintf("La plage ne peut pas dépasser %d jours.", MaxEventsDays)}
	}
	prs, err := c.selectPractitioners(ctx, a, in.Selection)
	if err != nil {
		return AgendaEvents{}, err
	}
	var out AgendaEvents
	if len(prs) == 0 {
		return out, nil
	}
	ids := make([]uuid.UUID, len(prs))
	for i, p := range prs {
		ids[i] = p.ID
	}
	q := sqlcgen.New(c.Pool)
	rows, err := q.ListCalendarAppointments(ctx, sqlcgen.ListCalendarAppointmentsParams{
		PracticeID: a.PracticeID, PractitionerIds: ids, FromAt: start, ToAt: end,
	})
	if err != nil {
		return AgendaEvents{}, fmt.Errorf("liste des rendez-vous: %w", err)
	}
	for _, r := range rows {
		out.Appts = append(out.Appts, apptFromRow(r, loc))
	}

	for _, pr := range prs {
		rules, err := q.ListAvailabilityRules(ctx, pr.ID)
		if err != nil {
			return AgendaEvents{}, fmt.Errorf("liste des plages: %w", err)
		}
		excs, err := q.ListAvailabilityExceptions(ctx, sqlcgen.ListAvailabilityExceptionsParams{PractitionerID: pr.ID, FromAt: start, ToAt: end})
		if err != nil {
			return AgendaEvents{}, fmt.Errorf("liste des absences: %w", err)
		}
		closed, open := splitExceptions(excs)
		if pc.CloseOnHolidays {
			for _, x := range booking.HolidayClosures(loc, midnight(start, loc), end, pc.HolidaysAlsaceMoselle, open) {
				closed = append(closed, booking.Interval{Start: x.Start, End: x.End})
			}
		}
		window := booking.Interval{Start: start, End: end}
		// Jour par jour (AddDate, jamais +24 h) : les plages hebdomadaires sont en heure murale.
		for d := midnight(start, loc); d.Before(end); d = d.AddDate(0, 0, 1) {
			day := booking.Interval{Start: d, End: d.AddDate(0, 0, 1)}
			avail := booking.Subtract(booking.Intersect(dayOpen(d, loc, rules, open, uuid.Nil), []booking.Interval{day}), closed)
			for _, gap := range booking.Intersect(booking.Subtract([]booking.Interval{day}, avail), []booking.Interval{window}) {
				out.Closed = append(out.Closed, AgendaClosed{PractitionerID: pr.ID, Start: gap.Start.In(loc), End: gap.End.In(loc)})
			}
		}
	}
	return out, nil
}

func apptFromRow(r sqlcgen.ListCalendarAppointmentsRow, loc *time.Location) AgendaAppt {
	return AgendaAppt{
		ID: r.ID, PractitionerID: r.PractitionerID,
		Start: r.StartsAt.In(loc), End: r.EndsAt.In(loc),
		BlockedStart: r.BlockedStart.In(loc), BlockedEnd: r.BlockedEnd.In(loc),
		Patient: r.LastName + " " + r.FirstName, TypeName: r.TypeName,
		Status: r.Status, StatusName: statusLabels[r.Status], AtHome: r.TypeAtHome,
		TypeColor: domain.ColorOrDefault(r.TypeColor),
	}
}

// Agenda est le contexte de la page agenda : praticiens affichés et liste du jour (repli sans JavaScript).
type Agenda struct {
	Date                          string // jour de référence « 2006-01-02 »
	Title                         string
	PrevDate, NextDate, TodayDate string
	Practitioners                 []CalPractitioner
	Appts                         []CalAppt // RDV du jour, par heure
	Holiday                       bool      // jour férié fermé
	Loc                           *time.Location
}

// AgendaDay retourne la liste du jour pour les praticiens demandés (voir Selection) ;
// date vide = aujourd'hui (jour du cabinet).
func (c *Calendar) AgendaDay(ctx context.Context, a Actor, date string, sel Selection) (Agenda, error) {
	prs, err := c.selectPractitioners(ctx, a, sel)
	if err != nil {
		return Agenda{}, err
	}
	pc, loc, err := c.practice().practiceAndLocation(ctx, a.PracticeID)
	if err != nil {
		return Agenda{}, err
	}
	now := c.Clock.Now().In(loc)
	ref := midnight(now, loc)
	if d := strings.TrimSpace(date); d != "" {
		t, err := time.Parse("2006-01-02", d)
		if err != nil {
			return Agenda{}, domain.ErrValidation{Field: "date", Msg: "La date doit être au format AAAA-MM-JJ."}
		}
		ref = time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, loc)
	}
	ag := Agenda{
		Date: ref.Format("2006-01-02"), Title: frenchDay(ref), Loc: loc,
		PrevDate:  ref.AddDate(0, 0, -1).Format("2006-01-02"),
		NextDate:  ref.AddDate(0, 0, 1).Format("2006-01-02"),
		TodayDate: midnight(now, loc).Format("2006-01-02"),
	}
	if pc.CloseOnHolidays {
		ag.Holiday = len(booking.HolidaysBetween(loc, ref, ref, pc.HolidaysAlsaceMoselle)) > 0
	}
	names := map[uuid.UUID]string{}
	ids := make([]uuid.UUID, 0, len(prs))
	for _, p := range prs {
		ag.Practitioners = append(ag.Practitioners, CalPractitioner{ID: p.ID, Name: PractitionerName(p), Color: domain.ColorOrDefault(p.Color)})
		names[p.ID] = PractitionerName(p)
		ids = append(ids, p.ID)
	}
	if len(ids) == 0 {
		return ag, nil
	}
	rows, err := sqlcgen.New(c.Pool).ListCalendarAppointments(ctx, sqlcgen.ListCalendarAppointmentsParams{
		PracticeID: a.PracticeID, PractitionerIds: ids, FromAt: ref, ToAt: ref.AddDate(0, 0, 1),
	})
	if err != nil {
		return Agenda{}, fmt.Errorf("liste des rendez-vous: %w", err)
	}
	for _, r := range rows {
		ap := apptFromRow(r, loc)
		// La plage récupérée est celle de la zone bloquée : on ne garde que les soins du jour.
		if civil(ap.Start) != civil(ref) {
			continue
		}
		ag.Appts = append(ag.Appts, CalAppt{
			ID: ap.ID, Start: ap.Start, End: ap.End, Practitioner: names[ap.PractitionerID],
			Patient: ap.Patient, TypeName: ap.TypeName, Status: ap.Status, StatusName: ap.StatusName, AtHome: ap.AtHome,
		})
	}
	slices.SortStableFunc(ag.Appts, func(x, y CalAppt) int { return x.Start.Compare(y.Start) })
	return ag, nil
}
