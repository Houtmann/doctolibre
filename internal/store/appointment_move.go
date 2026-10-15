package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"doctolibre/internal/domain"
	"doctolibre/internal/store/sqlcgen"
)

// MoveAppointment déplace un RDV « booked » du cabinet sur [start, end). Les
// buffers (before, after) sont ceux copiés sur le RDV à sa création ;
// during_blocked est recalculé via domain.BlockedRange. Un chevauchement
// (appointment_no_overlap) devient domain.ErrSlotTaken ; un RDV introuvable,
// d'un autre cabinet ou plus « booked » devient domain.ErrNotFound.
func MoveAppointment(ctx context.Context, q *sqlcgen.Queries, practiceID, id uuid.UUID, start, end time.Time, before, after time.Duration) (sqlcgen.Appointment, error) {
	bs, be := domain.BlockedRange(start, end, before, after)
	a, err := q.MoveAppointment(ctx, sqlcgen.MoveAppointmentParams{
		ID: id, PracticeID: practiceID,
		StartsAt: start, EndsAt: end, BlockedStart: bs, BlockedEnd: be,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return sqlcgen.Appointment{}, domain.ErrNotFound
	}
	if err != nil {
		if IsExclusionViolation(err) && constraintName(err) == "appointment_no_overlap" {
			return sqlcgen.Appointment{}, domain.ErrSlotTaken
		}
		return sqlcgen.Appointment{}, fmt.Errorf("move appointment: %w", err)
	}
	return a, nil
}
