package service

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"doctolibre/internal/domain"
	"doctolibre/internal/store"
	"doctolibre/internal/store/sqlcgen"
)

// NotificationInput est la saisie des réglages de notification (l'heure arrive
// en texte pour pouvoir réafficher une saisie invalide).
type NotificationInput struct {
	ConfirmationChannel string
	ReminderChannel     string
	ChangeChannel       string
	ReminderDayBefore   bool
	ReminderHour        string // heure d'envoi du rappel de la veille, 0-23
	ReminderTwoHours    bool
	ReminderSeries      bool
	Signature           string
}

// SMSEstimate est l'estimation du nombre de SMS par mois avec les réglages donnés.
type SMSEstimate struct {
	// Appointments : rendez-vous par mois (moyenne des 4 dernières semaines).
	Appointments int
	Confirmation int
	Reminders    int
	PerMonth     int
}

func notificationInputOf(pc sqlcgen.Practice) NotificationInput {
	return NotificationInput{
		ConfirmationChannel: pc.NotifyConfirmationChannel, ReminderChannel: pc.NotifyReminderChannel,
		ChangeChannel: pc.NotifyChangeChannel, ReminderDayBefore: pc.ReminderDayBefore,
		ReminderHour: fmt.Sprint(pc.ReminderDayBeforeHour), ReminderTwoHours: pc.ReminderTwoHours,
		ReminderSeries: pc.ReminderSeries, Signature: pc.NotifySignature,
	}
}

// Notifications retourne les réglages de notification du cabinet.
func (s *Practice) Notifications(ctx context.Context, a Actor) (NotificationInput, error) {
	pc, err := sqlcgen.New(s.Pool).GetPractice(ctx, a.PracticeID)
	if errors.Is(err, pgx.ErrNoRows) {
		return NotificationInput{}, domain.ErrNotFound
	}
	if err != nil {
		return NotificationInput{}, fmt.Errorf("lecture cabinet: %w", err)
	}
	return notificationInputOf(pc), nil
}

func validChannel(c string) bool {
	switch c {
	case ChannelEmail, ChannelSMS, ChannelBoth, ChannelNone:
		return true
	}
	return false
}

// UpdateNotifications valide puis enregistre les réglages de notification.
func (s *Practice) UpdateNotifications(ctx context.Context, a Actor, in NotificationInput) error {
	var errs domain.ValidationErrors
	add := func(field, msg string) { errs = append(errs, domain.ErrValidation{Field: field, Msg: msg}) }
	in.Signature = strings.TrimSpace(in.Signature)
	for _, c := range []struct{ field, v string }{
		{"confirmation_channel", in.ConfirmationChannel}, {"reminder_channel", in.ReminderChannel}, {"change_channel", in.ChangeChannel},
	} {
		if !validChannel(c.v) {
			add(c.field, "Choisissez un canal : email, SMS, les deux ou aucun.")
		}
	}
	hour, ok := parseIntRange(in.ReminderHour, 0, 23)
	if !ok {
		add("reminder_hour", "L'heure d'envoi du rappel doit être comprise entre 0 et 23.")
	}
	if tooLong(in.Signature, 200) || strings.ContainsAny(in.Signature, "\r\n") {
		add("signature", "La signature tient sur une ligne de 200 caractères maximum.")
	}
	if len(errs) == 0 {
		// Un canal désactivé (T086) n'est pas proposé : le service le refuse aussi.
		on, err := channelsOf(ctx, sqlcgen.New(s.Pool), a.PracticeID)
		if err != nil {
			return err
		}
		for _, c := range []struct{ field, v string }{
			{"confirmation_channel", in.ConfirmationChannel}, {"reminder_channel", in.ReminderChannel}, {"change_channel", in.ChangeChannel},
		} {
			if normalizeChannel(c.v, on) != c.v {
				add(c.field, "Ce canal est désactivé pour votre cabinet : choisissez un canal actif ou « Aucun ».")
			}
		}
	}
	if len(errs) > 0 {
		return errs
	}
	return store.WithTx(ctx, s.Pool, func(q *sqlcgen.Queries, _ pgx.Tx) error {
		if _, err := q.GetPractice(ctx, a.PracticeID); errors.Is(err, pgx.ErrNoRows) {
			return domain.ErrNotFound
		} else if err != nil {
			return fmt.Errorf("lecture cabinet: %w", err)
		}
		if err := q.SetPracticeNotifications(ctx, sqlcgen.SetPracticeNotificationsParams{
			ID: a.PracticeID, NotifyConfirmationChannel: in.ConfirmationChannel, NotifyReminderChannel: in.ReminderChannel,
			NotifyChangeChannel: in.ChangeChannel, ReminderDayBefore: in.ReminderDayBefore,
			ReminderDayBeforeHour: int16(hour), //nolint:gosec // borné à 0-23
			ReminderTwoHours:      in.ReminderTwoHours, ReminderSeries: in.ReminderSeries, NotifySignature: in.Signature,
		}); err != nil {
			return fmt.Errorf("réglages de notification: %w", err)
		}
		return nil
	})
}

// smsFor retourne combien de rendez-vous reçoivent un SMS pour un message envoyé
// selon le canal pref : « sms » et « both » touchent tout patient joignable par
// SMS ; « email » ne se replie sur le SMS que sans email utilisable.
func smsFor(pref string, capable, smsOnly int64) int64 {
	switch pref {
	case ChannelSMS, ChannelBoth:
		return capable
	case ChannelEmail:
		return smsOnly
	}
	return 0
}

// EstimateSMS estime le nombre de SMS par mois pour les réglages in, à partir des
// rendez-vous des 4 dernières semaines (hors annulations et déplacements, rares).
// C'est une estimation : elle ignore les canaux non vérifiés des réservations en ligne.
func (s *Practice) EstimateSMS(ctx context.Context, a Actor, in NotificationInput) (SMSEstimate, error) {
	now := s.Clock.Now()
	vol, err := sqlcgen.New(s.Pool).NotificationVolume(ctx, sqlcgen.NotificationVolumeParams{
		PracticeID: a.PracticeID, FromAt: now.Add(-4 * 7 * 24 * time.Hour), ToAt: now,
	})
	if err != nil {
		return SMSEstimate{}, fmt.Errorf("volume de rendez-vous: %w", err)
	}
	perMonth := func(n int64) int { return int(math.Round(float64(n) * 52 / 12 / 4)) }
	est := SMSEstimate{Appointments: perMonth(vol.Total)}
	est.Confirmation = perMonth(smsFor(in.ConfirmationChannel, vol.SmsCapable, vol.SmsOnly))
	rem := smsFor(in.ReminderChannel, vol.SmsCapable, vol.SmsOnly)
	var n int64
	if in.ReminderDayBefore {
		n += rem
	}
	if in.ReminderTwoHours {
		n += rem
	}
	est.Reminders = perMonth(n)
	est.PerMonth = est.Confirmation + est.Reminders
	return est, nil
}
