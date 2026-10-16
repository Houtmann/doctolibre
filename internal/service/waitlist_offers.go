package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"doctolibre/internal/domain"
	"doctolibre/internal/notify"
	"doctolibre/internal/store/sqlcgen"
)

// Paramètres des propositions de la liste d'attente.
const (
	// WaitlistOfferTTL : durée de validité du lien /w/{jeton}, à compter de l'envoi.
	WaitlistOfferTTL = 2 * time.Hour
	// waitlistMaxOffers : nombre maximal de patients prévenus pour un même créneau libéré.
	waitlistMaxOffers = 5
	// waitlistFetch : candidats lus (plus que le maximum : certains sont écartés
	// faute de canal utilisable ou de compatibilité exacte avec le moteur de créneaux).
	waitlistFetch = 25

	// TemplateWaitlist est le template (email et SMS) d'une proposition de créneau.
	TemplateWaitlist = "waitlist"
)

// slotFreed propose le créneau qui commence à start (praticien de appt) aux
// inscrits de la liste d'attente compatibles, dans la transaction métier de
// l'annulation ou du déplacement : un rollback n'envoie rien. Les messages
// respectent les canaux du cabinet (réglage « changements ») et l'opt-out du
// patient. Sans adresse publique (BaseURL vide), il n'y a pas de lien à envoyer
// et rien n'est fait. Un créneau libéré trop tard (délai minimal de réservation
// du cabinet) n'est proposé à personne.
func (n notifier) slotFreed(ctx context.Context, tx pgx.Tx, q *sqlcgen.Queries, appt sqlcgen.Appointment, start time.Time) error {
	if n.baseURL == "" {
		return nil
	}
	pc, err := q.GetPractice(ctx, appt.PracticeID)
	if err != nil {
		return fmt.Errorf("lecture cabinet: %w", err)
	}
	now := n.clock.Now()
	if start.Sub(now) < microsToDuration(intervalMicros(pc.MinBookingNotice)) {
		return nil
	}
	loc, err := time.LoadLocation(pc.Timezone)
	if err != nil {
		return fmt.Errorf("fuseau %q: %w", pc.Timezone, err)
	}
	local := start.In(loc)
	cands, err := q.ListWaitlistCandidates(ctx, sqlcgen.ListWaitlistCandidatesParams{
		PracticeID:     appt.PracticeID,
		PractitionerID: pgtype.UUID{Bytes: appt.PractitionerID, Valid: true},
		Day:            pgtype.Date{Time: time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, time.UTC), Valid: true},
		Weekday:        int16((int(local.Weekday()) + 6) % 7),
		At:             pgtype.Time{Microseconds: int64(local.Hour()*60+local.Minute()) * int64(time.Minute/time.Microsecond), Valid: true},
		ExcludePatient: appt.PatientID,
		SlotStart:      start,
		MaxRows:        waitlistFetch,
	})
	if err != nil {
		return fmt.Errorf("candidats de la liste d'attente: %w", err)
	}
	offered := 0
	for _, e := range cands {
		if offered >= waitlistMaxOffers {
			break
		}
		ok, err := n.offerTo(ctx, tx, q, appt, start, e)
		if err != nil {
			return err
		}
		if ok {
			offered++
		}
	}
	return nil
}

// offerTo propose le créneau à une inscription ; false si elle n'est finalement
// pas concernée (créneau incompatible avec son motif, aucun canal utilisable).
func (n notifier) offerTo(ctx context.Context, tx pgx.Tx, q *sqlcgen.Queries, appt sqlcgen.Appointment, start time.Time, e sqlcgen.WaitlistEntry) (bool, error) {
	// Le moteur de créneaux a le dernier mot : durée du motif, buffers, absences, délai minimal.
	sc, err := loadSlotContext(ctx, q, appt.PractitionerID, e.AppointmentTypeID)
	if errors.Is(err, domain.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if sc.typ.AtHome {
		return false, nil
	}
	slot, err := sc.check(ctx, q, n.clock.Now(), start)
	if errors.Is(err, domain.ErrSlotTaken) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	pat, err := q.GetPatient(ctx, sqlcgen.GetPatientParams{ID: e.PatientID, PracticeID: e.PracticeID})
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("lecture patient: %w", err)
	}
	rec := recipient{
		FirstName: pat.FirstName, Phone: pat.Phone.String, Email: pat.Email.String,
		SMSOptIn: pat.SmsOptIn, EmailOptIn: pat.EmailOptIn, Verified: e.VerifiedChannel.String,
	}
	channels := pickChannels(sc.pc.NotifyChangeChannel, rec)
	if len(channels) == 0 {
		return false, nil
	}

	// L'échéance part de l'envoi réel : un SMS reporté (plage silencieuse) ne doit pas
	// arriver déjà périmé.
	now := n.clock.Now()
	sendAt := now
	for _, ch := range channels {
		if ch == ChannelSMS {
			if sched := notify.SMSRunAt(now, sc.loc, false); sched.Before(start.Add(-minLeadBeforeStart)) && sched.After(sendAt) {
				sendAt = sched
			}
		}
	}
	expires := sendAt.Add(WaitlistOfferTTL)

	token, err := newManageToken()
	if err != nil {
		return false, err
	}
	offerID, err := q.CreateWaitlistOffer(ctx, sqlcgen.CreateWaitlistOfferParams{
		EntryID: e.ID, PracticeID: e.PracticeID, PractitionerID: appt.PractitionerID, SlotStart: start,
		TokenHash: ManageTokenHash(token), ExpiresAt: expires, CreatedAt: now,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil // déjà proposé
	}
	if err != nil {
		return false, fmt.Errorf("création de la proposition: %w", err)
	}

	// Message sans rendez-vous associé : l'annulation ou le déplacement de l'ancien
	// rendez-vous ne doit pas annuler la proposition.
	ghost := sqlcgen.Appointment{
		PracticeID: e.PracticeID, PractitionerID: appt.PractitionerID, AppointmentTypeID: sc.typ.ID, PatientID: pat.ID,
		During: pgtype.Range[pgtype.Timestamptz]{
			Lower: pgtype.Timestamptz{Time: slot.Start, Valid: true}, Upper: pgtype.Timestamptz{Time: slot.End, Valid: true},
			LowerType: pgtype.Inclusive, UpperType: pgtype.Exclusive, Valid: true,
		},
	}
	v := apptView{appt: ghost, practice: sc.pc, practitioner: sc.pr, typ: sc.typ, to: rec, loc: sc.loc}
	data := n.data(v, "")
	data["offer_url"] = strings.TrimRight(n.baseURL, "/") + "/w/" + token
	data["valid_until"] = clockText(expires.In(sc.loc))
	if _, err := n.enqueue(ctx, tx, v, sc.pc.NotifyChangeChannel, TemplateWaitlist, data, time.Time{}, TemplateWaitlist, offerID.String()); err != nil {
		return false, err
	}
	return true, nil
}
