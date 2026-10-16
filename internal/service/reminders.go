package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"doctolibre/internal/domain"
	"doctolibre/internal/store"
	"doctolibre/internal/store/sqlcgen"
)

// Paramètres de la tâche de rappels.
const (
	// DefaultReminderEvery : période de la boucle ; un rappel part donc au plus
	// ~5 minutes après son heure théorique.
	DefaultReminderEvery = 5 * time.Minute
	// reminderLookahead : le plus ancien rappel « la veille » (à 0 h) part moins de
	// 48 h avant le rendez-vous.
	reminderLookahead = 49 * time.Hour
	reminderPageSize  = 200
	practicePageSize  = 100
)

// Clés de déduplication des rappels (outbox_message.dedup_key = <clé>:<RDV>:<canal>:<début>).
const (
	dedupReminderDay = "reminder-24h"
	dedupReminderH2  = "reminder-2h"
)

// Reminders enfile les rappels (la veille à l'heure du cabinet, et 2 h avant si
// activé) peu avant leur envoi, en relisant à ce moment les contacts, l'opt-out
// du patient et le statut du rendez-vous. Idempotent : la clé de déduplication
// de l'outbox empêche tout doublon, même avec plusieurs processus. Les messages
// d'un rendez-vous annulé ou déplacé sont annulés par ces actions elles-mêmes
// (outbox.CancelForAppointment), et le verrou du rendez-vous sérialise les deux.
type Reminders struct {
	Pool   *pgxpool.Pool
	Clock  domain.Clock
	Logger *slog.Logger
	// BaseURL : adresse publique pour le lien de gestion des rappels (vide = sans lien).
	BaseURL string
	// ManageKey recalcule les jetons des liens de gestion (obligatoire).
	ManageKey ManageKey
	// Every : période de Run (défaut DefaultReminderEvery).
	Every time.Duration
}

// Run appelle RunOnce périodiquement jusqu'à l'annulation de ctx.
func (r *Reminders) Run(ctx context.Context) error {
	every := r.Every
	if every <= 0 {
		every = DefaultReminderEvery
	}
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		if n, err := r.RunOnce(ctx); err != nil {
			if ctx.Err() == nil {
				r.log().Error("rappels: tour en échec", "error", err)
			}
		} else if n > 0 {
			r.log().Info("rappels: messages enfilés", "count", n)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
		}
	}
}

func (r *Reminders) log() *slog.Logger {
	if r.Logger != nil {
		return r.Logger
	}
	return slog.Default()
}

// dayBeforeAt : la veille du rendez-vous à hour h (heure murale du cabinet ;
// AddDate et time.Date absorbent les changements d'heure).
func dayBeforeAt(start time.Time, hour int, loc *time.Location) time.Time {
	d := start.In(loc).AddDate(0, 0, -1)
	return time.Date(d.Year(), d.Month(), d.Day(), hour, 0, 0, 0, loc)
}

// reminderDue décide si un rappel prévu à sendAt doit être enfilé à now.
//   - pas encore l'heure : non (l'opt-out sera relu au dernier moment) ;
//   - rendez-vous créé ou modifié après l'heure d'envoi : non (le patient vient
//     de recevoir une confirmation, un rappel immédiat serait du bruit) ;
//   - sinon oui, tout de suite (rattrapage après une panne du worker).
func reminderDue(sendAt, now, lastTouch time.Time) bool {
	return !sendAt.After(now) && !lastTouch.After(sendAt)
}

// RunOnce examine les rendez-vous des 49 prochaines heures de tous les cabinets
// et retourne le nombre de messages enfilés.
func (r *Reminders) RunOnce(ctx context.Context) (int, error) {
	if r.Pool == nil {
		return 0, errors.New("rappels: pas de base de données")
	}
	now := r.Clock.Now()
	q := sqlcgen.New(r.Pool)
	total := 0
	after := uuid.Nil
	for {
		practices, err := q.ListPracticesPage(ctx, sqlcgen.ListPracticesPageParams{AfterID: after, PageSize: practicePageSize})
		if err != nil {
			return total, fmt.Errorf("liste des cabinets: %w", err)
		}
		for _, pc := range practices {
			n, err := r.runPractice(ctx, q, pc, now)
			total += n
			if err != nil {
				return total, err
			}
		}
		if len(practices) < practicePageSize {
			return total, nil
		}
		after = practices[len(practices)-1].ID
	}
}

func (r *Reminders) runPractice(ctx context.Context, q *sqlcgen.Queries, pc sqlcgen.Practice, now time.Time) (int, error) {
	if !pc.ReminderDayBefore && !pc.ReminderTwoHours {
		return 0, nil
	}
	if pc.NotifyReminderChannel == ChannelNone {
		return 0, nil
	}
	loc, err := time.LoadLocation(pc.Timezone)
	if err != nil {
		r.log().Error("rappels: fuseau invalide", "practice_id", pc.ID)
		return 0, nil // un cabinet mal configuré ne bloque pas les autres
	}
	n := notifier{clock: r.Clock, baseURL: r.BaseURL, key: r.ManageKey}
	total := 0
	afterStart, afterID := now.Add(-time.Second), uuid.Nil
	for {
		rows, err := q.ListReminderCandidates(ctx, sqlcgen.ListReminderCandidatesParams{
			PracticeID: pc.ID, FromAt: now, ToAt: now.Add(reminderLookahead),
			AfterStart: afterStart, AfterID: afterID, PageSize: reminderPageSize,
		})
		if err != nil {
			return total, fmt.Errorf("liste des rendez-vous à rappeler: %w", err)
		}
		for _, c := range rows {
			if c.CarePlanID.Valid && !pc.ReminderSeries {
				continue
			}
			to := recipient{
				FirstName: c.FirstName, Phone: c.Phone.String, Email: c.Email.String,
				SMSOptIn: c.SmsOptIn, EmailOptIn: c.EmailOptIn, Verified: c.VerifiedChannel.String,
			}
			if len(pickChannels(pc.NotifyReminderChannel, to)) == 0 {
				continue // aucun contact utilisable : rien à verrouiller
			}
			for _, kind := range reminderKinds(pc, c.StartsAt, loc) {
				if (kind.dedup == dedupReminderDay && c.DayQueued) || (kind.dedup == dedupReminderH2 && c.H2Queued) {
					continue
				}
				if !reminderDue(kind.sendAt, now, c.UpdatedAt) {
					continue
				}
				k, err := r.enqueueOne(ctx, n, c, kind)
				total += k
				if err != nil {
					return total, err
				}
			}
		}
		if len(rows) < reminderPageSize {
			return total, nil
		}
		last := rows[len(rows)-1]
		afterStart, afterID = last.StartsAt, last.ID
	}
}

type reminderKind struct {
	dedup  string
	sendAt time.Time
}

func reminderKinds(pc sqlcgen.Practice, start time.Time, loc *time.Location) []reminderKind {
	var out []reminderKind
	if pc.ReminderDayBefore {
		out = append(out, reminderKind{dedupReminderDay, dayBeforeAt(start, int(pc.ReminderDayBeforeHour), loc)})
	}
	if pc.ReminderTwoHours {
		out = append(out, reminderKind{dedupReminderH2, start.Add(-2 * time.Hour)})
	}
	return out
}

// enqueueOne enfile un rappel dans sa propre transaction, après avoir verrouillé
// le rendez-vous et revérifié son statut et son heure.
func (r *Reminders) enqueueOne(ctx context.Context, n notifier, c sqlcgen.ListReminderCandidatesRow, kind reminderKind) (int, error) {
	queued := 0
	err := store.WithTx(ctx, r.Pool, func(q *sqlcgen.Queries, tx pgx.Tx) error {
		appt, err := q.LockAppointment(ctx, c.ID)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("verrou du rendez-vous: %w", err)
		}
		if appt.Status != "booked" || !appt.During.Lower.Time.Equal(c.StartsAt) {
			return nil // annulé ou déplacé depuis la lecture
		}
		v, err := loadView(ctx, q, appt)
		if err != nil {
			return err
		}
		// Lien recalculé ; absent pour un ancien RDV dont le haché ne correspond pas.
		data := n.data(v, n.key.forAppointment(appt))
		queued, err = n.enqueue(ctx, tx, v, v.practice.NotifyReminderChannel, TemplateReminder, data, kind.sendAt,
			kind.dedup, fmt.Sprint(c.StartsAt.Unix()))
		return err
	})
	if err != nil {
		return 0, fmt.Errorf("rappel du rendez-vous %s: %w", c.ID, err)
	}
	return queued, nil
}
