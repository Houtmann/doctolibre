// Package outbox implémente la file de messages (email/SMS) stockée dans
// PostgreSQL : enregistrement dans la transaction métier, envoi par un worker.
package outbox

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"doctolibre/internal/domain"
	"doctolibre/internal/metrics"
	"doctolibre/internal/store/sqlcgen"
)

// Channel est le canal LISTEN/NOTIFY qui réveille les workers.
const Channel = "outbox"

// Types de message.
const (
	KindEmail = "email"
	KindSMS   = "sms"
)

// Msg est un message à envoyer. Il contient des données personnelles
// (destinataire, payload) : ne jamais le logger, seulement son ID.
type Msg struct {
	ID            uuid.UUID
	PracticeID    uuid.UUID
	Kind          string // KindEmail | KindSMS
	Template      string
	Payload       json.RawMessage // objet JSON ; vide = {}
	Recipient     string
	AppointmentID uuid.UUID // uuid.Nil = aucun RDV
	// DedupKey rend l'enqueue idempotent (ex. "reminder-24h:<appointment_id>").
	// Vide = pas de déduplication.
	DedupKey string
	// RunAt : échéance d'envoi ; zéro = tout de suite (now() côté base).
	RunAt time.Time
	// Attempts : numéro de la tentative en cours (rempli par le worker).
	Attempts int
}

// Enqueue enregistre msg dans la transaction métier tx (rollback => aucun
// message). Un DedupKey déjà présent est ignoré silencieusement. Le NOTIFY est
// émis dans la même transaction : les workers sont réveillés au commit.
func Enqueue(ctx context.Context, tx pgx.Tx, m Msg) error {
	if m.Kind != KindEmail && m.Kind != KindSMS {
		return domain.ErrValidation{Field: "kind", Msg: "doit valoir email ou sms"}
	}
	if m.Template == "" {
		return domain.ErrValidation{Field: "template", Msg: "obligatoire"}
	}
	if m.Recipient == "" {
		return domain.ErrValidation{Field: "recipient", Msg: "obligatoire"}
	}
	payload := []byte(m.Payload)
	if len(payload) == 0 {
		payload = []byte("{}")
	}
	if !json.Valid(payload) {
		return domain.ErrValidation{Field: "payload", Msg: "JSON invalide"}
	}
	_, err := sqlcgen.New(tx).EnqueueOutboxMessage(ctx, sqlcgen.EnqueueOutboxMessageParams{
		PracticeID:    m.PracticeID,
		Kind:          m.Kind,
		Template:      m.Template,
		Payload:       payload,
		Recipient:     m.Recipient,
		AppointmentID: pgtype.UUID{Bytes: m.AppointmentID, Valid: m.AppointmentID != uuid.Nil},
		DedupKey:      pgtype.Text{String: m.DedupKey, Valid: m.DedupKey != ""},
		RunAt:         pgtype.Timestamptz{Time: m.RunAt, Valid: !m.RunAt.IsZero()},
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil // dedup_key déjà enregistré : idempotent
	}
	if err != nil {
		return fmt.Errorf("enqueue outbox message: %w", err)
	}
	if _, err := tx.Exec(ctx, "SELECT pg_notify($1, '')", Channel); err != nil {
		return fmt.Errorf("notify outbox: %w", err)
	}
	return nil
}

// CancelForAppointment annule les messages encore pending d'un RDV (annulé ou
// déplacé), dans la transaction métier.
func CancelForAppointment(ctx context.Context, tx pgx.Tx, clock domain.Clock, appointmentID uuid.UUID) error {
	_, err := sqlcgen.New(tx).CancelOutboxForAppointment(ctx, sqlcgen.CancelOutboxForAppointmentParams{
		AppointmentID: pgtype.UUID{Bytes: appointmentID, Valid: true},
		Now:           clock.Now(),
	})
	if err != nil {
		return fmt.Errorf("cancel outbox for appointment: %w", err)
	}
	return nil
}

// Queue regroupe les opérations de lecture/administration de la file hors
// transaction métier (page admin, métriques).
type Queue struct {
	Pool  *pgxpool.Pool
	Clock domain.Clock
}

var _ metrics.OutboxSource = Queue{}

// OutboxStats implémente metrics.OutboxSource.
func (qu Queue) OutboxStats(ctx context.Context) (metrics.OutboxStats, error) {
	q := sqlcgen.New(qu.Pool)
	rows, err := q.OutboxCountByStatus(ctx)
	if err != nil {
		return metrics.OutboxStats{}, fmt.Errorf("outbox count by status: %w", err)
	}
	st := metrics.OutboxStats{ByStatus: make(map[string]int64, len(rows))}
	for _, r := range rows {
		st.ByStatus[r.Status] = r.N
	}
	oldest, err := q.OutboxOldestPending(ctx)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
	case err != nil:
		return metrics.OutboxStats{}, fmt.Errorf("outbox oldest pending: %w", err)
	default:
		if age := qu.Clock.Now().Sub(oldest); age > 0 {
			st.OldestPending = age
		}
	}
	return st, nil
}

// ListRecent retourne les n derniers messages du cabinet (page admin).
func (qu Queue) ListRecent(ctx context.Context, practiceID uuid.UUID, n int32) ([]sqlcgen.OutboxMessage, error) {
	rows, err := sqlcgen.New(qu.Pool).ListRecentOutboxMessages(ctx, sqlcgen.ListRecentOutboxMessagesParams{
		PracticeID: practiceID, MaxRows: n,
	})
	if err != nil {
		return nil, fmt.Errorf("list recent outbox: %w", err)
	}
	return rows, nil
}

// Resend remet un message failed/cancelled en file ; domain.ErrNotFound si le
// message n'existe pas, n'est pas à ce cabinet ou n'est pas renvoyable.
func (qu Queue) Resend(ctx context.Context, practiceID, id uuid.UUID) error {
	tx, err := qu.Pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	n, err := sqlcgen.New(tx).ResendOutboxMessage(ctx, sqlcgen.ResendOutboxMessageParams{
		ID: id, PracticeID: practiceID, Now: qu.Clock.Now(),
	})
	if err != nil {
		return fmt.Errorf("resend outbox message: %w", err)
	}
	if n == 0 {
		return domain.ErrNotFound
	}
	if _, err := tx.Exec(ctx, "SELECT pg_notify($1, '')", Channel); err != nil {
		return fmt.Errorf("notify outbox: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}
	return nil
}
