package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"doctolibre/internal/domain"
	"doctolibre/internal/store"
	"doctolibre/internal/store/sqlcgen"
)

// bulkCancelMax borne le nombre de rendez-vous annulés d'un coup (une absence
// de plusieurs semaines se découpe en plusieurs actions).
const bulkCancelMax = 300

// auditClosureBulkCancel : action du journal d'audit pour l'absence créée avec annulation.
const auditClosureBulkCancel = "availability.closure_cancel"

// BulkCancelItem est un rendez-vous touché par une absence, tel qu'affiché à l'aperçu.
type BulkCancelItem struct {
	ID         uuid.UUID
	Start, End time.Time // fuseau du cabinet
	TypeName   string
	Patient    string // affiché dans la page uniquement, jamais journalisé
}

// BulkCancelPreview est l'aperçu d'une annulation groupée : rien n'est modifié.
type BulkCancelPreview struct {
	From, To     time.Time
	Reason       string
	Practitioner string
	Items        []BulkCancelItem
}

// bulkTargets liste les rendez-vous à annuler : statut « booked », soin
// intersectant [from, to) et commençant après now (un rendez-vous déjà commencé
// ne reçoit pas de message d'annulation). Plus de bulkCancelMax → erreur de validation.
func bulkTargets(ctx context.Context, q *sqlcgen.Queries, practitionerID uuid.UUID, from, to, now time.Time) ([]sqlcgen.Appointment, error) {
	rows, err := q.ListAppointmentsForPractitioner(ctx, sqlcgen.ListAppointmentsForPractitionerParams{PractitionerID: practitionerID, FromAt: from, ToAt: to})
	if err != nil {
		return nil, fmt.Errorf("liste des rendez-vous: %w", err)
	}
	var out []sqlcgen.Appointment
	for _, ap := range rows {
		lo, hi := ap.During.Lower.Time, ap.During.Upper.Time
		if ap.Status != "booked" || !lo.Before(to) || !hi.After(from) || !lo.After(now) {
			continue
		}
		out = append(out, ap)
	}
	if len(out) > bulkCancelMax {
		return nil, domain.ErrValidation{Field: "end_date", Msg: fmt.Sprintf("Plus de %d rendez-vous sont concernés : réduisez la période.", bulkCancelMax)}
	}
	return out, nil
}

// CancelPreview liste les rendez-vous qu'annulerait CancelRange pour cette absence.
// Lecture seule. Mêmes droits que la gestion des absences (admin sur tout praticien,
// praticien sur son agenda ou sur celui dont il est remplaçant ; pas le secrétariat).
func (c *Calendar) CancelPreview(ctx context.Context, a Actor, in ClosureInput) (BulkCancelPreview, error) {
	p := c.practice()
	from, to, reason, err := p.closureRange(ctx, a, in)
	if err != nil {
		return BulkCancelPreview{}, err
	}
	pr, err := p.configPractitioner(ctx, a)
	if err != nil {
		return BulkCancelPreview{}, err
	}
	loc, err := p.location(ctx, a.PracticeID)
	if err != nil {
		return BulkCancelPreview{}, err
	}
	q := sqlcgen.New(c.Pool)
	appts, err := bulkTargets(ctx, q, pr.ID, from, to, c.Clock.Now())
	if err != nil {
		return BulkCancelPreview{}, err
	}
	out := BulkCancelPreview{From: from.In(loc), To: to.In(loc), Reason: reason, Practitioner: PractitionerName(pr)}
	types := map[uuid.UUID]string{}
	for _, ap := range appts {
		name, ok := types[ap.AppointmentTypeID]
		if !ok {
			t, err := q.GetAppointmentType(ctx, sqlcgen.GetAppointmentTypeParams{ID: ap.AppointmentTypeID, PracticeID: a.PracticeID})
			if err != nil {
				return BulkCancelPreview{}, fmt.Errorf("lecture motif: %w", err)
			}
			name = t.Name
			types[ap.AppointmentTypeID] = name
		}
		pat, err := q.GetPatient(ctx, sqlcgen.GetPatientParams{ID: ap.PatientID, PracticeID: a.PracticeID})
		if err != nil {
			return BulkCancelPreview{}, fmt.Errorf("lecture patient: %w", err)
		}
		out.Items = append(out.Items, BulkCancelItem{
			ID: ap.ID, Start: ap.During.Lower.Time.In(loc), End: ap.During.Upper.Time.In(loc),
			TypeName: name, Patient: strings.TrimSpace(pat.FirstName + " " + pat.LastName),
		})
	}
	return out, nil
}

// CancelRange crée l'absence (kind « closed ») puis annule les rendez-vous à venir
// de la plage, dans UNE transaction : l'absence est créée avant les annulations pour
// que la liste d'attente (slotFreed) ne propose aucun créneau de la plage fermée.
// Chaque annulation est auditée et enfile son message (motif + lien de gestion) dans
// la même transaction ; aucun appel réseau ici. Retourne le nombre de rendez-vous annulés.
func (c *Calendar) CancelRange(ctx context.Context, a Actor, in ClosureInput) (int, error) {
	p := c.practice()
	from, to, reason, err := p.closureRange(ctx, a, in)
	if err != nil {
		return 0, err
	}
	pr, err := p.configPractitioner(ctx, a)
	if err != nil {
		return 0, err
	}
	var done []uuid.UUID
	err = store.WithTx(ctx, c.Pool, func(q *sqlcgen.Queries, tx pgx.Tx) error {
		done = done[:0]
		exc, err := q.CreateAvailabilityException(ctx, sqlcgen.CreateAvailabilityExceptionParams{
			PractitionerID: pr.ID, StartsAt: from, EndsAt: to, Kind: "closed", Reason: reason,
		})
		if err != nil {
			return fmt.Errorf("création absence: %w", err)
		}
		now := c.Clock.Now()
		appts, err := bulkTargets(ctx, q, pr.ID, from, to, now)
		if err != nil {
			return err
		}
		for _, ap := range appts {
			cancelled, err := q.CancelAppointment(ctx, sqlcgen.CancelAppointmentParams{
				ID: ap.ID, PracticeID: a.PracticeID,
				CancelledAt:  pgtype.Timestamptz{Time: now, Valid: true},
				CancelledBy:  pgtype.Text{String: "staff", Valid: true},
				CancelReason: pgtype.Text{String: reason, Valid: reason != ""},
			})
			if errors.Is(err, pgx.ErrNoRows) {
				continue // annulé entre-temps
			}
			if err != nil {
				return fmt.Errorf("annulation: %w", err)
			}
			if err := audit(ctx, q, a, EventAppointmentCancelled, ap.ID, map[string]string{"bulk": "true", "has_reason": fmt.Sprint(reason != "")}); err != nil {
				return err
			}
			if err := c.notifier().cancelledFor(ctx, tx, q, cancelled, true, reason); err != nil {
				return err
			}
			done = append(done, ap.ID)
		}
		if err := q.CreateAuditLog(ctx, sqlcgen.CreateAuditLogParams{
			UserID: pgtype.UUID{Bytes: a.UserID, Valid: true}, PracticeID: pgtype.UUID{Bytes: a.PracticeID, Valid: true}, Actor: "user",
			Action: auditClosureBulkCancel, Entity: "availability_exception", EntityID: pgtype.UUID{Bytes: exc.ID, Valid: true},
			Meta: []byte(fmt.Sprintf(`{"cancelled":%d}`, len(done))),
		}); err != nil {
			return fmt.Errorf("journal d'audit: %w", err)
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	for _, id := range done {
		c.emit(ctx, Event{Kind: EventAppointmentCancelled, PracticeID: a.PracticeID, AppointmentID: id})
	}
	return len(done), nil
}
