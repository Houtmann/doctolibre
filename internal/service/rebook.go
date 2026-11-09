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

// RebookWindowDays est la durée pendant laquelle un RDV annulé permet de reprendre rendez-vous (T098).
const RebookWindowDays = 30

// ErrRebookClosed : la reprise n'est pas (ou plus) permise pour ce lien : RDV non annulé,
// annulé depuis plus de RebookWindowDays, motif ou praticien indisponible en ligne.
var ErrRebookClosed = errors.New("reprise de rendez-vous impossible")

// ErrAlreadyRebooked : ce RDV annulé a déjà donné lieu à une reprise (un seul rebook par RDV).
var ErrAlreadyRebooked = errors.New("rendez-vous déjà repris")

// rebookWindowOK indique si l'annulation est assez récente (borne incluse).
func rebookWindowOK(a sqlcgen.Appointment, now time.Time) bool {
	return a.Status == "cancelled" && a.CancelledAt.Valid && !now.After(a.CancelledAt.Time.Add(RebookWindowDays*24*time.Hour))
}

// lockRebookable lit et verrouille le RDV annulé du lien et vérifie qu'il permet une reprise
// (statut, fenêtre, pas déjà repris). Le jeton d'un RDV annulé reste valable pour cette seule action.
func (b *Booking) lockRebookable(ctx context.Context, q *sqlcgen.Queries, token string) (sqlcgen.Appointment, error) {
	if token == "" || len(token) > 128 {
		return sqlcgen.Appointment{}, domain.ErrNotFound
	}
	a, err := q.LockAppointmentByManageToken(ctx, pgtype.Text{String: ManageTokenHash(token), Valid: true})
	if errors.Is(err, pgx.ErrNoRows) {
		return sqlcgen.Appointment{}, domain.ErrNotFound
	}
	if err != nil {
		return sqlcgen.Appointment{}, fmt.Errorf("lecture rendez-vous: %w", err)
	}
	if !rebookWindowOK(a, b.Clock.Now()) {
		return sqlcgen.Appointment{}, ErrRebookClosed
	}
	if a.RebookedAppointmentID.Valid {
		return sqlcgen.Appointment{}, ErrAlreadyRebooked
	}
	return a, nil
}

// rebookContext charge le contexte de créneaux (praticien, motif actif et réservable en ligne)
// et applique le garde-fou d'absences répétées (T054). Un motif ou un praticien indisponible
// donne ErrRebookClosed ; le refus T054 donne domain.ErrOnlineBookingBlocked.
func rebookContext(ctx context.Context, q *sqlcgen.Queries, a sqlcgen.Appointment, now time.Time) (slotContext, error) {
	sc, err := loadSlotContext(ctx, q, a.PractitionerID, a.AppointmentTypeID)
	if errors.Is(err, domain.ErrNotFound) {
		return slotContext{}, ErrRebookClosed
	}
	if err != nil {
		return slotContext{}, err
	}
	if blocked, err := onlineBookingBlocked(ctx, q, sc.pc, a.PatientID, now); err != nil {
		return slotContext{}, err
	} else if blocked {
		return slotContext{}, domain.ErrOnlineBookingBlocked
	}
	return sc, nil
}

// RebookSlots retourne les créneaux où reprendre rendez-vous (même praticien, même motif),
// début dans [from, to). Erreurs : domain.ErrNotFound, ErrRebookClosed, ErrAlreadyRebooked,
// domain.ErrOnlineBookingBlocked.
func (b *Booking) RebookSlots(ctx context.Context, token string, from, to time.Time) ([]booking.Slot, error) {
	var out []booking.Slot
	err := store.WithTx(ctx, b.Pool, func(q *sqlcgen.Queries, _ pgx.Tx) error {
		a, err := b.lockRebookable(ctx, q, token)
		if err != nil {
			return err
		}
		sc, err := rebookContext(ctx, q, a, b.Clock.Now())
		if err != nil {
			return err
		}
		out, err = sc.slots(ctx, q, b.Clock.Now(), from, to)
		return err
	})
	return out, err
}

// RebookResult est le nouveau rendez-vous créé par une reprise.
type RebookResult struct {
	Appointment sqlcgen.Appointment
	Token       string // secret du lien du nouveau rendez-vous
}

// RebookByPatient reprend rendez-vous sur start, sans OTP : le jeton du RDV annulé prouve déjà
// le contrôle du canal (comme pour le déplacement). Patient, praticien, motif, adresse et
// canal vérifié sont ceux du RDV annulé. Tout se passe dans une transaction qui verrouille le
// RDV annulé : créneau revalidé, RDV créé, lien posé par un UPDATE conditionnel (un seul
// rebook, même en concurrence), confirmation enfilée.
// Erreurs : domain.ErrNotFound, ErrRebookClosed, ErrAlreadyRebooked, domain.ErrOnlineBookingBlocked,
// domain.ErrValidation (consignes non confirmées), domain.ErrSlotTaken.
func (b *Booking) RebookByPatient(ctx context.Context, token string, start time.Time, instructionsAck bool) (RebookResult, error) {
	apptID, newToken, tokenHash, err := b.ManageKey.mint()
	if err != nil {
		return RebookResult{}, err
	}
	now := b.Clock.Now()
	var appt sqlcgen.Appointment
	err = store.WithTx(ctx, b.Pool, func(q *sqlcgen.Queries, tx pgx.Tx) error {
		old, err := b.lockRebookable(ctx, q, token)
		if err != nil {
			return err
		}
		sc, err := rebookContext(ctx, q, old, now)
		if err != nil {
			return err
		}
		if err := checkInstructionsAck(sc.typ, instructionsAck); err != nil {
			return err
		}
		slot, err := sc.check(ctx, q, now, start)
		if err != nil {
			return err
		}
		address := ""
		if sc.typ.AtHome {
			address = old.Address.String
		}
		bb, ba := sc.buffers()
		appt, err = store.CreateAppointment(ctx, q, store.NewAppointment{
			ID: apptID, PracticeID: sc.pc.ID, PractitionerID: sc.pr.ID, PatientID: old.PatientID, AppointmentTypeID: sc.typ.ID,
			Start: slot.Start, End: slot.End, BufferBefore: bb, BufferAfter: ba,
			Address: address, Source: "online", ManageTokenHash: tokenHash,
			VerifiedChannel: old.VerifiedChannel.String,
		})
		if err != nil {
			return err
		}
		n, err := q.MarkAppointmentRebooked(ctx, sqlcgen.MarkAppointmentRebookedParams{
			NewID: pgtype.UUID{Bytes: appt.ID, Valid: true}, ID: old.ID, PracticeID: old.PracticeID,
		})
		if err != nil {
			return fmt.Errorf("lien de reprise: %w", err)
		}
		if n == 0 {
			return ErrAlreadyRebooked // annulé avec la transaction : pas de doublon
		}
		return b.notifier().booked(ctx, tx, q, appt, newToken)
	})
	if err != nil {
		return RebookResult{}, err
	}
	if b.OnCreated != nil {
		b.OnCreated("online")
	}
	b.emit(ctx, Event{Kind: EventAppointmentCreated, PracticeID: appt.PracticeID, AppointmentID: appt.ID})
	return RebookResult{Appointment: appt, Token: newToken}, nil
}

// rebookPermitted indique si le bouton « Reprendre rendez-vous » peut pointer vers la reprise
// directe : mêmes conditions que RebookSlots (fenêtre, pas déjà repris, motif et praticien
// réservables, patient non bloqué). Lecture seule, sans verrou.
func rebookPermitted(ctx context.Context, q *sqlcgen.Queries, a sqlcgen.Appointment, pc sqlcgen.Practice,
	pr sqlcgen.Practitioner, typ sqlcgen.AppointmentType, now time.Time) (bool, error) {
	if !rebookWindowOK(a, now) || a.RebookedAppointmentID.Valid || !pr.Active ||
		!typ.Active || !typ.OnlineBookable || (typ.AtHome && !homeVisitsOnline(pc)) {
		return false, nil
	}
	blocked, err := onlineBookingBlocked(ctx, q, pc, a.PatientID, now)
	return !blocked, err
}
