package outbox

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"doctolibre/internal/domain"
	"doctolibre/internal/store"
	"doctolibre/internal/store/sqlcgen"
)

// DefaultBackoff : délai avant la tentative n+1 après l'échec de la tentative
// n. Au-delà (6e échec), le message passe en failed.
var DefaultBackoff = []time.Duration{time.Minute, 5 * time.Minute, 30 * time.Minute, 2 * time.Hour, 6 * time.Hour}

const (
	defaultBatchSize    = 20
	defaultPollInterval = 30 * time.Second
	defaultStuckAfter   = 10 * time.Minute
	defaultSendTimeout  = 30 * time.Second
	finishTimeout       = 10 * time.Second
	listenRetryDelay    = 5 * time.Second
	maxLastErrorLen     = 500
)

// Dispatcher envoie un message (SMTP, SMS…). Appelé hors transaction.
// Une erreur enveloppée par Permanent n'est pas retentée ; toute autre erreur
// est temporaire (retry avec backoff).
type Dispatcher interface {
	Send(ctx context.Context, m Msg) (providerRef string, err error)
}

// DispatcherFunc adapte une fonction en Dispatcher.
type DispatcherFunc func(ctx context.Context, m Msg) (string, error)

// Send implémente Dispatcher.
func (f DispatcherFunc) Send(ctx context.Context, m Msg) (string, error) { return f(ctx, m) }

type permanentError struct{ err error }

func (e permanentError) Error() string { return e.err.Error() }
func (e permanentError) Unwrap() error { return e.err }

// Permanent marque err comme non retentable (adresse invalide, numéro refusé…).
func Permanent(err error) error {
	if err == nil {
		return nil
	}
	return permanentError{err: err}
}

// IsPermanent indique si err (ou une erreur qu'elle enveloppe) est permanente.
func IsPermanent(err error) bool {
	var p permanentError
	return errors.As(err, &p)
}

type cancelError struct{ err error }

func (e cancelError) Error() string { return e.err.Error() }
func (e cancelError) Unwrap() error { return e.err }

// Cancel marque err comme « message à annuler » : le message passe en cancelled,
// sans retente (canal désactivé par le cabinet). Un renvoi manuel reste possible.
func Cancel(err error) error {
	if err == nil {
		return nil
	}
	return cancelError{err: err}
}

// IsCancelled indique si err (ou une erreur qu'elle enveloppe) demande l'annulation du message.
func IsCancelled(err error) bool {
	var c cancelError
	return errors.As(err, &c)
}

// Worker consomme la file : claim en transaction courte, envoi hors
// transaction, mise à jour du statut.
type Worker struct {
	Pool       *pgxpool.Pool
	Dispatcher Dispatcher
	Clock      domain.Clock
	Logger     *slog.Logger

	BatchSize    int             // défaut 20
	PollInterval time.Duration   // polling de secours, défaut 30 s
	StuckAfter   time.Duration   // sending plus ancien => pending, défaut 10 min
	SendTimeout  time.Duration   // délai max d'un envoi, défaut 30 s
	Backoff      []time.Duration // défaut DefaultBackoff
}

func (w *Worker) batchSize() int32 {
	if w.BatchSize > 0 {
		return int32(min(w.BatchSize, 1000)) //nolint:gosec // borné
	}
	return defaultBatchSize
}

func durOr(d, def time.Duration) time.Duration {
	if d > 0 {
		return d
	}
	return def
}

func (w *Worker) backoff() []time.Duration {
	if w.Backoff != nil {
		return w.Backoff
	}
	return DefaultBackoff
}

func (w *Worker) log() *slog.Logger {
	if w.Logger != nil {
		return w.Logger
	}
	return slog.Default()
}

// Run boucle jusqu'à l'annulation de ctx. À l'annulation, l'envoi en cours est
// terminé (et son statut enregistré) ; les messages réservés mais non envoyés
// sont rendus à la file. Retourne nil sur arrêt normal.
func (w *Worker) Run(ctx context.Context) error {
	wake := make(chan struct{}, 1)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		w.listen(ctx, wake)
	}()
	defer wg.Wait()

	ticker := time.NewTicker(durOr(w.PollInterval, defaultPollInterval))
	defer ticker.Stop()
	for {
		for ctx.Err() == nil {
			n, err := w.RunOnce(ctx)
			if err != nil {
				if ctx.Err() == nil {
					w.log().Error("outbox: cycle en échec", "error", err)
				}
				break
			}
			if n < int(w.batchSize()) {
				break
			}
		}
		select {
		case <-ctx.Done():
			return nil
		case <-wake:
		case <-ticker.C:
		}
	}
}

// listen maintient un LISTEN sur Channel et signale wake à chaque NOTIFY.
// Sans cette connexion, le polling de secours suffit.
func (w *Worker) listen(ctx context.Context, wake chan<- struct{}) {
	for ctx.Err() == nil {
		if err := w.listenOnce(ctx, wake); err != nil && ctx.Err() == nil {
			w.log().Warn("outbox: LISTEN interrompu, retry", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(listenRetryDelay):
		}
	}
}

func (w *Worker) listenOnce(ctx context.Context, wake chan<- struct{}) error {
	pc, err := w.Pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire listen conn: %w", err)
	}
	conn := pc.Hijack() // connexion en LISTEN : ne retourne pas dans le pool
	defer func() {
		cctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), finishTimeout)
		defer cancel()
		_ = conn.Close(cctx)
	}()
	if _, err := conn.Exec(ctx, "LISTEN "+Channel); err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	// Un message a pu être posé avant le LISTEN : un tour de boucle de rattrapage.
	select {
	case wake <- struct{}{}:
	default:
	}
	for {
		if _, err := conn.WaitForNotification(ctx); err != nil {
			return fmt.Errorf("wait notification: %w", err)
		}
		select {
		case wake <- struct{}{}:
		default:
		}
	}
}

// RunOnce exécute un cycle : récupération des messages bloqués, claim d'un
// lot, envoi. Retourne le nombre de messages réservés.
func (w *Worker) RunOnce(ctx context.Context) (int, error) {
	stuck, send := durOr(w.StuckAfter, defaultStuckAfter), durOr(w.SendTimeout, defaultSendTimeout)
	if stuck < 2*send {
		return 0, fmt.Errorf("outbox: StuckAfter (%s) doit être >= 2 x SendTimeout (%s)", stuck, send)
	}
	now := w.Clock.Now()
	var claimed []sqlcgen.OutboxMessage
	err := store.WithTx(ctx, w.Pool, func(q *sqlcgen.Queries, _ pgx.Tx) error {
		n, err := q.RecoverStuckOutbox(ctx, sqlcgen.RecoverStuckOutboxParams{
			Now: now, Cutoff: now.Add(-durOr(w.StuckAfter, defaultStuckAfter)),
		})
		if err != nil {
			return fmt.Errorf("recover stuck: %w", err)
		}
		if n > 0 {
			w.log().Warn("outbox: messages bloqués remis en file", "count", n)
		}
		claimed, err = q.ClaimOutboxMessages(ctx, sqlcgen.ClaimOutboxMessagesParams{Now: now, BatchSize: w.batchSize()})
		if err != nil {
			return fmt.Errorf("claim: %w", err)
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	// Tx commitée : les messages sont 'sending'. L'envoi se fait hors transaction.
	for i, row := range claimed {
		if ctx.Err() != nil {
			w.release(ctx, claimed[i:])
			break
		}
		w.deliver(ctx, row)
	}
	return len(claimed), nil
}

// release rend à la file des messages réservés mais non envoyés (arrêt).
func (w *Worker) release(ctx context.Context, rows []sqlcgen.OutboxMessage) {
	fctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), finishTimeout)
	defer cancel()
	q := sqlcgen.New(w.Pool)
	for _, r := range rows {
		_, err := q.ReleaseOutboxMessage(fctx, sqlcgen.ReleaseOutboxMessageParams{ID: r.ID, Now: w.Clock.Now(), Attempts: r.Attempts})
		if err != nil {
			w.log().Error("outbox: release en échec", "message_id", r.ID, "error", err)
		}
	}
}

func (w *Worker) deliver(ctx context.Context, row sqlcgen.OutboxMessage) {
	// Touch gardé : le lot est envoyé séquentiellement, un autre worker a pu
	// récupérer ce message entre-temps. 0 ligne => ne pas envoyer.
	tctx, tcancel := context.WithTimeout(context.WithoutCancel(ctx), finishTimeout)
	owned, terr := sqlcgen.New(w.Pool).TouchOutboxMessage(tctx, sqlcgen.TouchOutboxMessageParams{
		ID: row.ID, Attempts: row.Attempts, Now: w.Clock.Now(),
	})
	tcancel()
	if terr != nil {
		// Le message reste 'sending' : la récupération le remettra en file.
		w.log().Error("outbox: touch en échec, envoi abandonné", "message_id", row.ID, "error", terr)
		return
	}
	if owned == 0 {
		w.log().Warn("outbox: message récupéré par un autre worker, envoi abandonné", "message_id", row.ID)
		return
	}
	// Détaché de ctx : un arrêt gracieux laisse l'envoi en cours se terminer.
	sctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), durOr(w.SendTimeout, defaultSendTimeout))
	defer cancel()
	ref, sendErr := w.Dispatcher.Send(sctx, toMsg(row))

	fctx, fcancel := context.WithTimeout(context.WithoutCancel(ctx), finishTimeout)
	defer fcancel()
	q := sqlcgen.New(w.Pool)
	now := w.Clock.Now()
	log := w.log().With("message_id", row.ID, "kind", row.Kind, "attempt", row.Attempts)

	var (
		n       int64
		err     error
		outcome string
	)
	switch {
	case sendErr == nil:
		outcome = "sent"
		n, err = q.MarkOutboxSent(fctx, sqlcgen.MarkOutboxSentParams{ID: row.ID, Attempts: row.Attempts, Now: now, ProviderRef: textOf(ref)})
	case IsCancelled(sendErr):
		outcome = "cancelled"
		n, err = q.MarkOutboxCancelled(fctx, sqlcgen.MarkOutboxCancelledParams{ID: row.ID, Attempts: row.Attempts, Now: now, LastError: textOf(truncate(sendErr.Error()))})
	case IsPermanent(sendErr) || int(row.Attempts) > len(w.backoff()):
		outcome = "failed"
		n, err = q.MarkOutboxFailed(fctx, sqlcgen.MarkOutboxFailedParams{ID: row.ID, Attempts: row.Attempts, Now: now, LastError: textOf(truncate(sendErr.Error()))})
	default:
		outcome = "retry"
		n, err = q.MarkOutboxRetry(fctx, sqlcgen.MarkOutboxRetryParams{
			ID: row.ID, Attempts: row.Attempts, Now: now,
			RunAt:     now.Add(w.backoff()[row.Attempts-1]),
			LastError: textOf(truncate(sendErr.Error())),
		})
	}
	switch {
	case err != nil:
		// Le message reste 'sending' : la récupération le remettra en file.
		log.Error("outbox: mise à jour du statut en échec", "outcome", outcome, "error", err)
	case n == 0:
		log.Warn("outbox: statut modifié pendant l'envoi, mise à jour ignorée", "outcome", outcome)
	default:
		// Le texte de l'erreur peut contenir un destinataire : il reste en base (last_error).
		log.Info("outbox: message traité", "outcome", outcome)
	}
}

func toMsg(r sqlcgen.OutboxMessage) Msg {
	m := Msg{
		ID: r.ID, PracticeID: r.PracticeID, Kind: r.Kind, Template: r.Template,
		Payload: r.Payload, Recipient: r.Recipient, RunAt: r.RunAt, Attempts: int(r.Attempts),
	}
	if r.AppointmentID.Valid {
		m.AppointmentID = uuid.UUID(r.AppointmentID.Bytes)
	}
	if r.DedupKey.Valid {
		m.DedupKey = r.DedupKey.String
	}
	return m
}

func truncate(s string) string {
	if len(s) > maxLastErrorLen {
		return strings.ToValidUTF8(s[:maxLastErrorLen], "")
	}
	return s
}

func textOf(s string) pgtype.Text { return pgtype.Text{String: s, Valid: true} }
