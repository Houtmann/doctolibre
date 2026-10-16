package service

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"doctolibre/internal/domain"
	"doctolibre/internal/notify"
	"doctolibre/internal/outbox"
	"doctolibre/internal/store/sqlcgen"
)

// Templates des messages liés à un rendez-vous (internal/notify/templates).
const (
	TemplateConfirmation = "confirmation"
	TemplateReminder     = "reminder"
	TemplateCancellation = "cancellation"
	TemplateReschedule   = "reschedule"
	TemplateSeries       = "series"
)

// Canaux acceptés dans les réglages de notification du cabinet.
const (
	ChannelEmail = "email"
	ChannelSMS   = "sms"
	ChannelBoth  = "both"
	ChannelNone  = "none"
)

// smsNameMax borne le nom du cabinet dans un SMS (un segment GSM-7 visé).
const smsNameMax = 30

// minLeadBeforeStart : un rappel qui ne pourrait partir qu'à moins de 30 minutes
// du rendez-vous (plage silencieuse SMS comprise) n'est pas envoyé.
const minLeadBeforeStart = 30 * time.Minute

// Les messages sont enfilés par les services eux-mêmes, dans la transaction
// métier (AGENTS.md) : pas de sink post-commit, donc pas de message perdu ni
// de message pour une action annulée.

// notifier enfile les messages d'un rendez-vous. Valeur sans état : le coût de
// construction est nul.
type notifier struct {
	clock   domain.Clock
	baseURL string
}

// recipient regroupe les coordonnées d'un patient utilisables pour écrire.
type recipient struct {
	FirstName  string
	Phone      string
	Email      string
	SMSOptIn   bool
	EmailOptIn bool
	// Verified : canal vérifié par code à la réservation en ligne ; vide = RDV du
	// cabinet, tous les contacts sont utilisables.
	Verified string
}

func (r recipient) canEmail() bool {
	return r.Email != "" && r.EmailOptIn && (r.Verified == "" || r.Verified == ChannelEmail)
}

func (r recipient) canSMS() bool {
	return r.Phone != "" && r.SMSOptIn && (r.Verified == "" || r.Verified == ChannelSMS)
}

// pickChannels applique la préférence du cabinet. « email » se replie sur le SMS
// quand le patient n'a pas d'email utilisable (et inversement pour « sms ») ;
// « both » utilise tous les canaux disponibles ; « none » n'envoie rien.
func pickChannels(pref string, r recipient) []string {
	e, s := r.canEmail(), r.canSMS()
	switch pref {
	case ChannelNone:
		return nil
	case ChannelBoth:
		var out []string
		if e {
			out = append(out, ChannelEmail)
		}
		if s {
			out = append(out, ChannelSMS)
		}
		return out
	case ChannelSMS:
		if s {
			return []string{ChannelSMS}
		}
		if e {
			return []string{ChannelEmail}
		}
	default: // email
		if e {
			return []string{ChannelEmail}
		}
		if s {
			return []string{ChannelSMS}
		}
	}
	return nil
}

// apptView est ce qu'il faut pour rédiger un message sur un rendez-vous.
type apptView struct {
	appt         sqlcgen.Appointment
	practice     sqlcgen.Practice
	practitioner sqlcgen.Practitioner
	typ          sqlcgen.AppointmentType
	to           recipient
	loc          *time.Location
}

func loadView(ctx context.Context, q *sqlcgen.Queries, appt sqlcgen.Appointment) (apptView, error) {
	pc, err := q.GetPractice(ctx, appt.PracticeID)
	if err != nil {
		return apptView{}, fmt.Errorf("lecture cabinet: %w", err)
	}
	pr, err := q.GetPractitioner(ctx, sqlcgen.GetPractitionerParams{ID: appt.PractitionerID, PracticeID: appt.PracticeID})
	if err != nil {
		return apptView{}, fmt.Errorf("lecture praticien: %w", err)
	}
	typ, err := q.GetAppointmentType(ctx, sqlcgen.GetAppointmentTypeParams{ID: appt.AppointmentTypeID, PracticeID: appt.PracticeID})
	if err != nil {
		return apptView{}, fmt.Errorf("lecture motif: %w", err)
	}
	pat, err := q.GetPatient(ctx, sqlcgen.GetPatientParams{ID: appt.PatientID, PracticeID: appt.PracticeID})
	if err != nil {
		return apptView{}, fmt.Errorf("lecture patient: %w", err)
	}
	loc, err := time.LoadLocation(pc.Timezone)
	if err != nil {
		return apptView{}, fmt.Errorf("fuseau %q: %w", pc.Timezone, err)
	}
	return apptView{
		appt: appt, practice: pc, practitioner: pr, typ: typ, loc: loc,
		to: recipient{
			FirstName: pat.FirstName, Phone: pat.Phone.String, Email: pat.Email.String,
			SMSOptIn: pat.SmsOptIn, EmailOptIn: pat.EmailOptIn, Verified: appt.VerifiedChannel.String,
		},
	}, nil
}

// clockText : « 14h30 ».
func clockText(t time.Time) string { return fmt.Sprintf("%dh%02d", t.Hour(), t.Minute()) }

// data construit les variables des templates (email et SMS). Tous les champs
// sont toujours présents (les templates échouent sur une clé absente).
func (n notifier) data(v apptView, manageToken string) map[string]any {
	start := v.appt.During.Lower.Time.In(v.loc)
	signature := strings.TrimSpace(v.practice.NotifySignature)
	if signature == "" {
		signature = v.practice.Name
	}
	manage := ""
	if manageToken != "" && n.baseURL != "" {
		manage = strings.TrimRight(n.baseURL, "/") + "/r/" + url.PathEscape(manageToken)
	}
	return map[string]any{
		"practice_name": v.practice.Name, "practice_address": v.practice.Address, "practice_phone": v.practice.Phone,
		"first_name": v.to.FirstName, "practitioner": strings.TrimSpace(v.practitioner.FirstName + " " + v.practitioner.LastName),
		"type_name": v.typ.Name, "date": frenchDay(start), "date_short": start.Format("02/01"), "time": clockText(start),
		"manage_url": manage, "signature": signature,
		"old_date": "", "old_date_short": "", "old_time": "",
	}
}

// enqueue enfile le message template pour chaque canal retenu. runAt zéro =
// immédiat. dedupKind distingue les messages d'un même rendez-vous ; dedupSuffix
// rend la clé propre à un événement précis (nouvelle heure, déplacement…).
func (n notifier) enqueue(ctx context.Context, tx pgx.Tx, v apptView, pref, template string, data map[string]any, runAt time.Time, dedupKind, dedupSuffix string) (int, error) {
	start := v.appt.During.Lower.Time
	now := n.clock.Now()
	queued := 0
	for _, ch := range pickChannels(pref, v.to) {
		m := outbox.Msg{PracticeID: v.appt.PracticeID, Template: template, AppointmentID: v.appt.ID}
		at := runAt
		switch ch {
		case ChannelEmail:
			m.Kind, m.Recipient = outbox.KindEmail, v.to.Email
			payload, err := marshalPayload(data)
			if err != nil {
				return queued, err
			}
			m.Payload = payload
		case ChannelSMS:
			sd := maps.Clone(data)
			sd["practice_name"] = truncate(v.practice.Name, smsNameMax)
			body, err := notify.RenderSMS(template, sd)
			if err != nil {
				return queued, fmt.Errorf("rédaction du SMS %s: %w", template, err)
			}
			payload, err := marshalPayload(map[string]any{"body": body, "sms_opt_in": v.to.SMSOptIn})
			if err != nil {
				return queued, err
			}
			m.Kind, m.Recipient, m.Payload = outbox.KindSMS, v.to.Phone, payload
			base := at
			if base.IsZero() {
				base = now
			}
			// Pas de SMS entre 20 h et 8 h, sauf si cela dépasserait le début du rendez-vous.
			if sched := notify.SMSRunAt(base, v.loc, false); sched.Before(start.Add(-minLeadBeforeStart)) {
				at = sched
			} else {
				at = base
			}
		}
		m.RunAt = at
		m.DedupKey = fmt.Sprintf("%s:%s:%s", dedupKind, v.appt.ID, ch)
		if dedupSuffix != "" {
			m.DedupKey += ":" + dedupSuffix
		}
		if err := outbox.Enqueue(ctx, tx, m); err != nil {
			return queued, err
		}
		queued++
	}
	return queued, nil
}

// booked enfile la confirmation d'un rendez-vous créé (dans la transaction de
// création). manageToken vide = pas de lien dans le message.
func (n notifier) booked(ctx context.Context, tx pgx.Tx, q *sqlcgen.Queries, appt sqlcgen.Appointment, manageToken string) error {
	v, err := loadView(ctx, q, appt)
	if err != nil {
		return err
	}
	_, err = n.enqueue(ctx, tx, v, v.practice.NotifyConfirmationChannel, TemplateConfirmation, n.data(v, manageToken), time.Time{}, TemplateConfirmation, "")
	return err
}

// seriesBooked enfile UNE confirmation pour toute une série de séances créée d'un
// coup (pas N messages). Un seul rendez-vous : confirmation ordinaire. Le message
// est rattaché à la première séance. Pas de lien de gestion (jetons non générés
// pour les séances d'une série).
func (n notifier) seriesBooked(ctx context.Context, tx pgx.Tx, q *sqlcgen.Queries, appts []sqlcgen.Appointment) error {
	switch len(appts) {
	case 0:
		return nil
	case 1:
		return n.booked(ctx, tx, q, appts[0], "")
	}
	sort.Slice(appts, func(i, j int) bool { return appts[i].During.Lower.Time.Before(appts[j].During.Lower.Time) })
	v, err := loadView(ctx, q, appts[0])
	if err != nil {
		return err
	}
	data := n.data(v, "")
	sessions := make([]string, len(appts))
	for i, a := range appts {
		t := a.During.Lower.Time.In(v.loc)
		sessions[i] = frenchDay(t) + " à " + clockText(t)
	}
	data["count"], data["sessions"] = len(appts), sessions
	_, err = n.enqueue(ctx, tx, v, v.practice.NotifyConfirmationChannel, TemplateSeries, data, time.Time{}, "series-confirmation", "")
	return err
}

// cancelled annule les messages en attente du rendez-vous et, si l'annulation
// vient du cabinet, prévient le patient. Le créneau libéré est proposé à la liste
// d'attente (T031).
func (n notifier) cancelled(ctx context.Context, tx pgx.Tx, q *sqlcgen.Queries, appt sqlcgen.Appointment, byStaff bool) error {
	if err := outbox.CancelForAppointment(ctx, tx, n.clock, appt.ID); err != nil {
		return err
	}
	if byStaff {
		v, err := loadView(ctx, q, appt)
		if err != nil {
			return err
		}
		if _, err := n.enqueue(ctx, tx, v, v.practice.NotifyChangeChannel, TemplateCancellation, n.data(v, ""), time.Time{}, TemplateCancellation, ""); err != nil {
			return err
		}
	}
	return n.slotFreed(ctx, tx, q, appt, appt.During.Lower.Time)
}

// rescheduled annule les anciens messages en attente (rappels de l'ancienne heure
// compris) et prévient le patient du nouveau créneau. Les nouveaux rappels sont
// enfilés par la tâche périodique (Reminders), à partir de la nouvelle heure.
func (n notifier) rescheduled(ctx context.Context, tx pgx.Tx, q *sqlcgen.Queries, appt sqlcgen.Appointment, oldStart time.Time, manageToken string) error {
	if err := outbox.CancelForAppointment(ctx, tx, n.clock, appt.ID); err != nil {
		return err
	}
	v, err := loadView(ctx, q, appt)
	if err != nil {
		return err
	}
	data := n.data(v, manageToken)
	old := oldStart.In(v.loc)
	data["old_date"], data["old_date_short"], data["old_time"] = frenchDay(old), old.Format("02/01"), clockText(old)
	_, err = n.enqueue(ctx, tx, v, v.practice.NotifyChangeChannel, TemplateReschedule, data, time.Time{},
		TemplateReschedule, fmt.Sprint(appt.UpdatedAt.UnixMicro()))
	if err != nil {
		return err
	}
	// L'ancien créneau est libre : il est proposé à la liste d'attente (T031).
	return n.slotFreed(ctx, tx, q, appt, oldStart)
}

func marshalPayload(v any) ([]byte, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("payload de message: %w", err)
	}
	return b, nil
}
