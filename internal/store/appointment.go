package store

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"doctolibre/internal/domain"
	"doctolibre/internal/store/sqlcgen"
)

// NewAppointment décrit un RDV à créer. Les buffers sont ceux du motif au
// moment de la création (copiés, pas référencés).
type NewAppointment struct {
	PracticeID        uuid.UUID
	PractitionerID    uuid.UUID
	PatientID         uuid.UUID
	AppointmentTypeID uuid.UUID
	Start, End        time.Time
	BufferBefore      time.Duration
	BufferAfter       time.Duration
	Source            string // online | staff
	ManageTokenHash   string // vide = aucun
}

// CreateAppointment calcule during_blocked via domain.BlockedRange puis insère.
// Un chevauchement (contrainte appointment_no_overlap) devient domain.ErrSlotTaken.
func CreateAppointment(ctx context.Context, q *sqlcgen.Queries, n NewAppointment) (sqlcgen.Appointment, error) {
	bs, be := domain.BlockedRange(n.Start, n.End, n.BufferBefore, n.BufferAfter)
	a, err := q.CreateAppointment(ctx, sqlcgen.CreateAppointmentParams{
		PracticeID:        n.PracticeID,
		PractitionerID:    n.PractitionerID,
		PatientID:         n.PatientID,
		AppointmentTypeID: n.AppointmentTypeID,
		StartsAt:          n.Start,
		EndsAt:            n.End,
		BufferBefore:      toInterval(n.BufferBefore),
		BufferAfter:       toInterval(n.BufferAfter),
		BlockedStart:      bs,
		BlockedEnd:        be,
		Status:            "booked",
		Source:            n.Source,
		ManageTokenHash:   pgtype.Text{String: n.ManageTokenHash, Valid: n.ManageTokenHash != ""},
	})
	if err != nil {
		if IsExclusionViolation(err) && constraintName(err) == "appointment_no_overlap" {
			return sqlcgen.Appointment{}, domain.ErrSlotTaken
		}
		return sqlcgen.Appointment{}, fmt.Errorf("create appointment: %w", err)
	}
	return a, nil
}

func toInterval(d time.Duration) pgtype.Interval {
	return pgtype.Interval{Microseconds: d.Microseconds(), Valid: true}
}
