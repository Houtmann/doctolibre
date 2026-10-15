package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"doctolibre/internal/booking"
	"doctolibre/internal/domain"
	"doctolibre/internal/store"
	"doctolibre/internal/store/sqlcgen"
)

// ErrChangeTooLate : le rendez-vous est trop proche (délai du cabinet) ou n'est
// plus modifiable ; le patient doit contacter le cabinet.
var ErrChangeTooLate = errors.New("modification trop tardive")

func (b *Booking) emit(ctx context.Context, e Event) {
	if b.Events == nil {
		return
	}
	e.At = b.Clock.Now()
	b.Events.Emit(ctx, e)
}

// lockByToken lit et verrouille le rendez-vous du lien (dans la transaction),
// puis vérifie qu'il est à venir et dans le délai du cabinet.
func (b *Booking) lockByToken(ctx context.Context, q *sqlcgen.Queries, token string) (sqlcgen.Appointment, error) {
	if token == "" || len(token) > 128 {
		return sqlcgen.Appointment{}, domain.ErrNotFound
	}
	// FOR UPDATE : sérialise annulations et déplacements concurrents du même rendez-vous.
	a, err := q.LockAppointmentByManageToken(ctx, pgtype.Text{String: ManageTokenHash(token), Valid: true})
	if errors.Is(err, pgx.ErrNoRows) {
		return sqlcgen.Appointment{}, domain.ErrNotFound
	}
	if err != nil {
		return sqlcgen.Appointment{}, fmt.Errorf("lecture rendez-vous: %w", err)
	}
	pc, err := q.GetPractice(ctx, a.PracticeID)
	if err != nil {
		return sqlcgen.Appointment{}, fmt.Errorf("lecture cabinet: %w", err)
	}
	if a.Status != "booked" || a.During.Lower.Time.Sub(b.Clock.Now()) < microsToDuration(intervalMicros(pc.MinCancelNotice)) {
		return sqlcgen.Appointment{}, ErrChangeTooLate
	}
	return a, nil
}

// CancelByPatient annule le rendez-vous du lien (cancelled_by = 'patient').
// Erreurs : domain.ErrNotFound (jeton inconnu), ErrChangeTooLate (hors délai).
func (b *Booking) CancelByPatient(ctx context.Context, token string) error {
	var appt sqlcgen.Appointment
	err := store.WithTx(ctx, b.Pool, func(q *sqlcgen.Queries, tx pgx.Tx) error {
		a, err := b.lockByToken(ctx, q, token)
		if err != nil {
			return err
		}
		appt, err = q.CancelAppointment(ctx, sqlcgen.CancelAppointmentParams{
			ID: a.ID, PracticeID: a.PracticeID,
			CancelledAt: pgtype.Timestamptz{Time: b.Clock.Now(), Valid: true},
			CancelledBy: pgtype.Text{String: "patient", Valid: true},
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrChangeTooLate
		}
		if err != nil {
			return fmt.Errorf("annulation: %w", err)
		}
		// Annulée par le patient : on supprime les rappels, sans message d'annulation.
		return b.notifier().cancelled(ctx, tx, q, appt, false)
	})
	if err != nil {
		return err
	}
	b.emit(ctx, Event{Kind: EventAppointmentCancelled, PracticeID: appt.PracticeID, AppointmentID: appt.ID})
	return nil
}

// rescheduleContext charge le contexte de créneaux du rendez-vous (le rendez-vous
// lui-même n'occupe pas ses propres créneaux).
func rescheduleContext(ctx context.Context, q *sqlcgen.Queries, a sqlcgen.Appointment) (slotContext, error) {
	sc, err := loadSlotContext(ctx, q, a.PractitionerID, a.AppointmentTypeID)
	if err != nil {
		return slotContext{}, err
	}
	sc.ignore = a.ID
	return sc, nil
}

// RescheduleSlots retourne les créneaux où le rendez-vous du lien peut être
// déplacé, début dans [from, to). Hors délai : ErrChangeTooLate. Un motif qui
// n'est plus réservable en ligne donne domain.ErrNotFound.
func (b *Booking) RescheduleSlots(ctx context.Context, token string, from, to time.Time) ([]booking.Slot, error) {
	var out []booking.Slot
	err := store.WithTx(ctx, b.Pool, func(q *sqlcgen.Queries, _ pgx.Tx) error {
		a, err := b.lockByToken(ctx, q, token)
		if err != nil {
			return err
		}
		sc, err := rescheduleContext(ctx, q, a)
		if err != nil {
			return err
		}
		out, err = sc.slots(ctx, q, b.Clock.Now(), from, to)
		return err
	})
	return out, err
}

// RescheduleByPatient déplace le rendez-vous du lien sur start. Le créneau est
// revalidé par le moteur dans la transaction (le rendez-vous est verrouillé) ;
// un échec laisse l'ancien rendez-vous intact. Le lien reste valable.
// Erreurs : domain.ErrNotFound, ErrChangeTooLate, domain.ErrSlotTaken.
func (b *Booking) RescheduleByPatient(ctx context.Context, token string, start time.Time) error {
	var appt sqlcgen.Appointment
	var from time.Time
	err := store.WithTx(ctx, b.Pool, func(q *sqlcgen.Queries, tx pgx.Tx) error {
		a, err := b.lockByToken(ctx, q, token)
		if err != nil {
			return err
		}
		from = a.During.Lower.Time
		sc, err := rescheduleContext(ctx, q, a)
		if err != nil {
			return err
		}
		slot, err := sc.check(ctx, q, b.Clock.Now(), start)
		if err != nil {
			return err
		}
		appt, err = store.MoveAppointment(ctx, q, a.PracticeID, a.ID, slot.Start, slot.End,
			microsToDuration(intervalMicros(sc.typ.BufferBefore)), microsToDuration(intervalMicros(sc.typ.BufferAfter)))
		if err != nil {
			return err
		}
		return b.notifier().rescheduled(ctx, tx, q, appt, from, token)
	})
	if err != nil {
		return err
	}
	b.emit(ctx, Event{Kind: EventAppointmentMoved, PracticeID: appt.PracticeID, AppointmentID: appt.ID, Detail: from.UTC().Format(time.RFC3339)})
	return nil
}
