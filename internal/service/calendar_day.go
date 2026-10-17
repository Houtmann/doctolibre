package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"doctolibre/internal/domain"
	"doctolibre/internal/store/sqlcgen"
)

// MyDayAppt est un rendez-vous de « Ma journée » (T072).
type MyDayAppt struct {
	ID         uuid.UUID
	Start, End time.Time
	Patient    string
	Phone      string
	TypeName   string
	Status     string
	StatusName string
	AtHome     bool
	Address    string        // adresse de la visite à domicile
	Travel     time.Duration // trajet réservé avant le soin (domicile)
	// InSeries : le RDV appartient à une série ; SeriesOrdinal vaut 0 pour une
	// séance « absent » (non comptée), SeriesTotal est le nombre prescrit.
	InSeries                   bool
	SeriesOrdinal, SeriesTotal int
}

// MyDaySummary est le récapitulatif de la journée (hors annulés).
type MyDaySummary struct {
	Done, NoShow, Upcoming int
	HomeVisits             int
}

// MyDayView est la page « Ma journée » d'un praticien.
type MyDayView struct {
	Date, Title                   string // « 2006-01-02 », « jeudi 15 octobre »
	PrevDate, NextDate, TodayDate string
	IsToday                       bool
	// Practitioner : praticien affiché (ID nul = aucun choisi) ; Choices : praticiens visibles.
	Practitioner CalPractitioner
	Choices      []CalPractitioner
	Appts        []MyDayAppt
	Summary      MyDaySummary
	// HomeAddresses : adresses des visites à domicile, dans l'ordre de la journée.
	HomeAddresses []string
}

// Day retourne la journée d'un praticien (heure du cabinet) : RDV non annulés
// par heure de début, avec téléphone, adresse et rang dans la série.
// practitionerID nul = le praticien de l'acteur ; date vide = aujourd'hui (jour du cabinet).
// Sans praticien déterminable (secrétariat), la vue est vide et Choices propose la liste.
func (c *Calendar) Day(ctx context.Context, a Actor, practitionerID uuid.UUID, date string) (MyDayView, error) {
	loc, err := c.practice().location(ctx, a.PracticeID)
	if err != nil {
		return MyDayView{}, err
	}
	now := c.Clock.Now().In(loc)
	ref := midnight(now, loc)
	if d := strings.TrimSpace(date); d != "" {
		t, err := time.Parse("2006-01-02", d)
		if err != nil {
			return MyDayView{}, domain.ErrValidation{Field: "date", Msg: "La date doit être au format AAAA-MM-JJ."}
		}
		ref = time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, loc)
	}
	v := MyDayView{
		Date: ref.Format("2006-01-02"), Title: frenchDay(ref),
		PrevDate: ref.AddDate(0, 0, -1).Format("2006-01-02"), NextDate: ref.AddDate(0, 0, 1).Format("2006-01-02"),
		TodayDate: midnight(now, loc).Format("2006-01-02"),
	}
	v.IsToday = v.Date == v.TodayDate

	if practitionerID == uuid.Nil {
		practitionerID = a.PractitionerID
	}
	if practitionerID != uuid.Nil && !a.mayAccessPractitioner(practitionerID) {
		return MyDayView{}, domain.ErrForbidden
	}
	prs, err := c.ViewablePractitioners(ctx, a)
	if err != nil {
		return MyDayView{}, err
	}
	for _, p := range prs {
		cp := CalPractitioner{ID: p.ID, Name: PractitionerName(p), Color: domain.ColorOrDefault(p.Color)}
		v.Choices = append(v.Choices, cp)
		if p.ID == practitionerID {
			v.Practitioner = cp
		}
	}
	if practitionerID == uuid.Nil && len(v.Choices) == 1 {
		v.Practitioner = v.Choices[0]
		practitionerID = v.Practitioner.ID
	}
	if v.Practitioner.ID == uuid.Nil {
		if practitionerID != uuid.Nil {
			return MyDayView{}, domain.ErrNotFound // inactif ou d'un autre cabinet
		}
		return v, nil
	}

	rows, err := sqlcgen.New(c.Pool).ListDayAppointments(ctx, sqlcgen.ListDayAppointmentsParams{
		PracticeID: a.PracticeID, PractitionerID: practitionerID, FromAt: ref, ToAt: ref.AddDate(0, 0, 1),
	})
	if err != nil {
		return MyDayView{}, fmt.Errorf("liste du jour: %w", err)
	}
	for _, r := range rows {
		ap := MyDayAppt{
			ID: r.ID, Start: r.StartsAt.In(loc), End: r.EndsAt.In(loc),
			Patient: r.FirstName + " " + r.LastName, Phone: r.Phone.String, TypeName: r.TypeName,
			Status: r.Status, StatusName: statusLabels[r.Status], AtHome: r.TypeAtHome, Address: r.Address.String,
			InSeries: r.CarePlanID.Valid, SeriesOrdinal: int(r.SeriesOrdinal), SeriesTotal: int(r.SeriesPrescribed),
		}
		if r.TypeAtHome {
			ap.Travel = r.StartsAt.Sub(r.BlockedStart)
			v.Summary.HomeVisits++
			if ap.Address != "" {
				v.HomeAddresses = append(v.HomeAddresses, ap.Address)
			}
		}
		switch r.Status {
		case "done":
			v.Summary.Done++
		case "no_show":
			v.Summary.NoShow++
		default:
			v.Summary.Upcoming++
		}
		v.Appts = append(v.Appts, ap)
	}
	return v, nil
}
