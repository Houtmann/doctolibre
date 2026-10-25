package service

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"doctolibre/internal/domain"
	"doctolibre/internal/store"
	"doctolibre/internal/store/sqlcgen"
)

// TemplateWaitlistJoined est le template (email et SMS) du récapitulatif d'inscription.
const TemplateWaitlistJoined = "waitlist_joined"

// waitlistTokenLabel sépare la clé des jetons de désinscription (/wl/{jeton}) des
// autres jetons dérivés de la même ManageKey.
const waitlistTokenLabel = "waitlist-token/v1"

func (k ManageKey) waitlistKey() []byte {
	m := hmac.New(sha256.New, k)
	m.Write([]byte(waitlistTokenLabel)) // hash.Hash.Write ne renvoie jamais d'erreur
	return m.Sum(nil)
}

// WaitlistToken retourne le jeton dérivé du lien de désinscription d'une inscription.
func (k ManageKey) WaitlistToken(id uuid.UUID) string {
	return DeriveManageToken(k.waitlistKey(), id, 1)
}

// mintWaitlist retourne le jeton et le haché à stocker pour l'inscription id ;
// "" sans clé (le message partira sans lien). Le jeton n'est jamais stocké ni journalisé.
func (k ManageKey) mintWaitlist(id uuid.UUID) (token, hash string) {
	if len(k) == 0 {
		return "", ""
	}
	t := k.WaitlistToken(id)
	return t, ManageTokenHash(t)
}

// forWaitlistEntry retourne le jeton à mettre dans un message pour une inscription
// existante, ou "" s'il ne correspond pas au haché stocké (inscription antérieure à
// T097, clé absente). Comparaison en temps constant.
func (k ManageKey) forWaitlistEntry(e sqlcgen.WaitlistEntry) string {
	if len(k) == 0 || !e.UnsubscribeHash.Valid {
		return ""
	}
	t := k.WaitlistToken(e.ID)
	if subtle.ConstantTimeCompare([]byte(ManageTokenHash(t)), []byte(e.UnsubscribeHash.String)) != 1 {
		return ""
	}
	return t
}

// leaveURL est l'adresse absolue du lien de désinscription ("" sans BaseURL ou sans jeton).
func leaveURL(baseURL, token string) string {
	if baseURL == "" || token == "" {
		return ""
	}
	return strings.TrimRight(baseURL, "/") + "/wl/" + url.PathEscape(token)
}

// windowText décrit la fenêtre souhaitée : « du 12 novembre au 12 décembre, lundi,
// mercredi, début entre 9h00 et 12h00 ».
func windowText(earliest, latest pgtype.Date, weekdays []int16, from, to pgtype.Time) string {
	day := func(d pgtype.Date) string { return fmt.Sprintf("%d %s", d.Time.Day(), monthNames[d.Time.Month()-1]) }
	s := "du " + day(earliest) + " au " + day(latest)
	if len(weekdays) > 0 {
		names := make([]string, 0, len(weekdays))
		for _, d := range weekdays {
			names = append(names, WeekdayName(int(d)))
		}
		s += ", " + strings.Join(names, ", ")
	}
	if from.Valid && to.Valid {
		clock := func(t pgtype.Time) string {
			m := int(time.Duration(t.Microseconds) * time.Microsecond / time.Minute)
			return fmt.Sprintf("%dh%02d", m/60, m%60)
		}
		s += ", début entre " + clock(from) + " et " + clock(to)
	}
	return s
}

// waitlistJoined enfile le récapitulatif d'une inscription (dans la transaction de
// Join) : motif, praticien, fenêtre souhaitée et lien de désinscription. Il part sur
// le canal vérifié par le code, sous réserve du réglage « confirmations » du cabinet.
func (n notifier) waitlistJoined(ctx context.Context, tx pgx.Tx, q *sqlcgen.Queries, pc sqlcgen.Practice, loc *time.Location, pat sqlcgen.Patient, e sqlcgen.WaitlistEntry, token string) error {
	typ, err := q.GetAppointmentType(ctx, sqlcgen.GetAppointmentTypeParams{ID: e.AppointmentTypeID, PracticeID: e.PracticeID})
	if err != nil {
		return fmt.Errorf("lecture motif: %w", err)
	}
	var pr sqlcgen.Practitioner
	practitioner := "n'importe quel praticien"
	if e.PractitionerID.Valid {
		pr, err = q.GetPractitioner(ctx, sqlcgen.GetPractitionerParams{ID: e.PractitionerID.Bytes, PracticeID: e.PracticeID})
		if err != nil {
			return fmt.Errorf("lecture praticien: %w", err)
		}
		practitioner = strings.TrimSpace(pr.FirstName + " " + pr.LastName)
	}
	// Message sans rendez-vous associé ; la fin de la fenêtre sert d'« heure de début »
	// pour que les SMS respectent la plage silencieuse.
	end := time.Date(e.Latest.Time.Year(), e.Latest.Time.Month(), e.Latest.Time.Day(), 23, 0, 0, 0, loc)
	ghost := sqlcgen.Appointment{
		PracticeID: e.PracticeID, PractitionerID: pr.ID, AppointmentTypeID: typ.ID, PatientID: pat.ID,
		During: pgtype.Range[pgtype.Timestamptz]{
			Lower: pgtype.Timestamptz{Time: end, Valid: true}, Upper: pgtype.Timestamptz{Time: end.Add(time.Hour), Valid: true},
			LowerType: pgtype.Inclusive, UpperType: pgtype.Exclusive, Valid: true,
		},
	}
	rec := recipient{
		FirstName: pat.FirstName, Phone: pat.Phone.String, Email: pat.Email.String,
		SMSOptIn: pat.SmsOptIn, EmailOptIn: pat.EmailOptIn, Verified: e.VerifiedChannel.String,
	}
	v := apptView{appt: ghost, practice: pc, practitioner: pr, typ: typ, to: rec, loc: loc}
	data := n.data(v, "")
	data["practitioner"] = practitioner
	data["window"] = windowText(e.Earliest, e.Latest, e.Weekdays, e.TimeFrom, e.TimeTo)
	data["leave_url"] = leaveURL(n.baseURL, token)
	_, err = n.enqueue(ctx, tx, v, pc.NotifyConfirmationChannel, TemplateWaitlistJoined, data, time.Time{}, TemplateWaitlistJoined, e.ID.String())
	return err
}

// LeaveView est l'inscription retrouvée par son lien /wl/{jeton}.
type LeaveView struct {
	Practice     sqlcgen.Practice
	Practitioner string // vide = n'importe quel praticien
	Type         string
	Window       string
}

// activeEntryByLeaveToken retrouve l'inscription ACTIVE du jeton. Jeton inconnu, mal
// formé, inscription close ou retirée : domain.ErrNotFound, sans distinction.
func (w *Waitlist) activeEntryByLeaveToken(ctx context.Context, q *sqlcgen.Queries, token string) (sqlcgen.WaitlistEntry, error) {
	if token == "" || len(token) > 128 {
		return sqlcgen.WaitlistEntry{}, domain.ErrNotFound
	}
	hash := ManageTokenHash(token)
	e, err := q.GetWaitlistEntryByUnsubscribeHash(ctx, pgtype.Text{String: hash, Valid: true})
	if errors.Is(err, pgx.ErrNoRows) {
		return sqlcgen.WaitlistEntry{}, domain.ErrNotFound
	}
	if err != nil {
		return sqlcgen.WaitlistEntry{}, fmt.Errorf("lecture inscription: %w", err)
	}
	if !e.UnsubscribeHash.Valid || subtle.ConstantTimeCompare([]byte(hash), []byte(e.UnsubscribeHash.String)) != 1 || e.Status != "active" {
		return sqlcgen.WaitlistEntry{}, domain.ErrNotFound
	}
	return e, nil
}

// LeaveView décrit l'inscription pour la page de confirmation. Lecture seule.
func (w *Waitlist) LeaveView(ctx context.Context, token string) (LeaveView, error) {
	q := sqlcgen.New(w.Pool)
	e, err := w.activeEntryByLeaveToken(ctx, q, token)
	if err != nil {
		return LeaveView{}, err
	}
	pc, err := q.GetPractice(ctx, e.PracticeID)
	if err != nil {
		return LeaveView{}, fmt.Errorf("lecture cabinet: %w", err)
	}
	typ, err := q.GetAppointmentType(ctx, sqlcgen.GetAppointmentTypeParams{ID: e.AppointmentTypeID, PracticeID: e.PracticeID})
	if err != nil {
		return LeaveView{}, fmt.Errorf("lecture motif: %w", err)
	}
	v := LeaveView{Practice: pc, Type: typ.Name, Window: windowText(e.Earliest, e.Latest, e.Weekdays, e.TimeFrom, e.TimeTo)}
	if e.PractitionerID.Valid {
		pr, err := q.GetPractitioner(ctx, sqlcgen.GetPractitionerParams{ID: e.PractitionerID.Bytes, PracticeID: e.PracticeID})
		if err != nil {
			return LeaveView{}, fmt.Errorf("lecture praticien: %w", err)
		}
		v.Practitioner = strings.TrimSpace(pr.FirstName + " " + pr.LastName)
	}
	return v, nil
}

// Leave désinscrit le patient (statut « cancelled »). domain.ErrNotFound si le lien
// est inconnu ou déjà utilisé.
func (w *Waitlist) Leave(ctx context.Context, token string) error {
	return store.WithTx(ctx, w.Pool, func(q *sqlcgen.Queries, _ pgx.Tx) error {
		e, err := w.activeEntryByLeaveToken(ctx, q, token)
		if err != nil {
			return err
		}
		n, err := q.CancelWaitlistEntry(ctx, sqlcgen.CancelWaitlistEntryParams{ID: e.ID, PracticeID: e.PracticeID})
		if err != nil {
			return fmt.Errorf("désinscription: %w", err)
		}
		if n == 0 {
			return domain.ErrNotFound // désinscrite entre-temps
		}
		slog.InfoContext(ctx, "désinscription de la liste d'attente par le patient",
			slog.String("practice_id", e.PracticeID.String()), slog.String("entry_id", e.ID.String()))
		return nil
	})
}
