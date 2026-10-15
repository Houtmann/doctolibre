package service

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// Types d'événements émis par le service d'agenda.
const (
	EventAppointmentCreated   = "appointment.created"
	EventAppointmentMoved     = "appointment.moved"
	EventAppointmentCancelled = "appointment.cancelled"
	EventAppointmentStatus    = "appointment.status_changed"
)

// Event décrit un fait métier à propager (notifications, T023). Il ne contient
// que des identifiants : jamais de nom, téléphone ou email.
type Event struct {
	Kind          string
	PracticeID    uuid.UUID
	AppointmentID uuid.UUID
	At            time.Time
	// Detail : complément court (ex. nouveau statut, ancien début RFC 3339).
	Detail string
}

// EventSink reçoit les événements métier, après validation de la transaction.
// L'implémentation ne doit pas bloquer : les notifications réelles passent par
// l'outbox (T023).
type EventSink interface {
	Emit(ctx context.Context, e Event)
}

// NopSink ignore tous les événements.
type NopSink struct{}

// Emit ne fait rien.
func (NopSink) Emit(context.Context, Event) {}
