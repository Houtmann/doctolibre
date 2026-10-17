package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"doctolibre/internal/booking"
	"doctolibre/internal/domain"
	"doctolibre/internal/outbox"
	"doctolibre/internal/store"
	"doctolibre/internal/store/sqlcgen"
)

// patientSearchLimit borne les résultats de la recherche de patient.
const patientSearchLimit = 10

// Calendar porte l'agenda du praticien : lecture jour/semaine, création,
// déplacement, annulation et statut des rendez-vous. Chaque action émet un
// événement vers Events.
type Calendar struct {
	Pool   *pgxpool.Pool
	Clock  domain.Clock
	Events EventSink // nil = NopSink ; appelé après commit
	// BaseURL : adresse publique pour les liens de gestion des messages (vide = sans lien).
	BaseURL string
	// ManageKey dérive les jetons des liens de gestion des RDV (obligatoire).
	ManageKey ManageKey
	// OnCreated est appelé (source "staff") après le commit d'un rendez-vous créé ; nil = rien.
	OnCreated func(source string)
}

func (c *Calendar) notifier() notifier {
	return notifier{clock: c.Clock, baseURL: c.BaseURL, key: c.ManageKey}
}

func (c *Calendar) practice() *Practice { return &Practice{Pool: c.Pool, Clock: c.Clock} }

func (c *Calendar) emit(ctx context.Context, e Event) {
	e.At = c.Clock.Now()
	if c.Events == nil {
		return
	}
	c.Events.Emit(ctx, e)
}

// --- Lecture de l'agenda ---

// Selection désigne les agendas demandés. Vide = l'agenda du praticien connecté
// (tous les praticiens visibles pour le secrétariat, qui n'en a pas).
type Selection struct {
	IDs []uuid.UUID
	// All : tous les praticiens actifs que l'utilisateur peut voir.
	All bool
}

// CalAppt est un RDV de la liste du jour (repli sans JavaScript de l'agenda).
type CalAppt struct {
	ID           uuid.UUID
	Start, End   time.Time
	Practitioner string
	Patient      string
	TypeName     string
	Status       string
	StatusName   string // libellé français du statut
	AtHome       bool   // visite à domicile
}

// CalPractitioner est un praticien affiché dans l'agenda.
type CalPractitioner struct {
	ID    uuid.UUID
	Name  string
	Color string // #rrggbb garanti
}

// PractitionerName retourne « Prénom Nom » (ou le nom seul).
func PractitionerName(p sqlcgen.Practitioner) string {
	return strings.TrimSpace(p.FirstName + " " + p.LastName)
}

// CheckTarget vérifie que le praticien visé (a.Target) existe dans le cabinet et
// est accessible à l'acteur ; sans cible, ne fait rien.
func (c *Calendar) CheckTarget(ctx context.Context, a Actor) error {
	if a.Target == uuid.Nil {
		return nil
	}
	_, err := c.practice().Practitioner(ctx, a)
	return err
}

// ViewablePractitioners retourne les praticiens actifs dont l'agenda est visible par a.
func (c *Calendar) ViewablePractitioners(ctx context.Context, a Actor) ([]sqlcgen.Practitioner, error) {
	return c.practice().TeamPractitioners(ctx, a)
}

// selectPractitioners résout une Selection en praticiens du cabinet
// (ErrForbidden si l'un n'est pas accessible à l'acteur, ErrNotFound s'il est d'un autre cabinet).
func (c *Calendar) selectPractitioners(ctx context.Context, a Actor, in Selection) ([]sqlcgen.Practitioner, error) {
	q := sqlcgen.New(c.Pool)
	switch {
	case in.All:
		return c.ViewablePractitioners(ctx, a)
	case len(in.IDs) > 0:
		var out []sqlcgen.Practitioner
		seen := map[uuid.UUID]bool{}
		for _, id := range in.IDs {
			if seen[id] {
				continue
			}
			seen[id] = true
			if !a.mayAccessPractitioner(id) {
				return nil, domain.ErrForbidden
			}
			p, err := q.GetPractitioner(ctx, sqlcgen.GetPractitionerParams{ID: id, PracticeID: a.PracticeID})
			if errors.Is(err, pgx.ErrNoRows) {
				return nil, domain.ErrNotFound
			}
			if err != nil {
				return nil, fmt.Errorf("lecture praticien: %w", err)
			}
			out = append(out, p)
		}
		return out, nil
	case a.Role == domain.RoleSecretary:
		return c.ViewablePractitioners(ctx, a)
	}
	pr, err := c.practice().Practitioner(ctx, a)
	if err != nil {
		return nil, err
	}
	return []sqlcgen.Practitioner{pr}, nil
}

// --- Disponibilités (grisage et contrôle « hors horaires ») ---

func splitExceptions(excs []sqlcgen.AvailabilityException) (closed, open []booking.Interval) {
	for _, e := range excs {
		if !e.During.Lower.Valid || !e.During.Upper.Valid {
			continue
		}
		iv := booking.Interval{Start: e.During.Lower.Time, End: e.During.Upper.Time}
		if e.Kind == "closed" {
			closed = append(closed, iv)
		} else {
			open = append(open, iv)
		}
	}
	return closed, open
}

func civil(t time.Time) int { return t.Year()*10000 + int(t.Month())*100 + t.Day() }

// dayOpen retourne les plages hebdomadaires qui s'appliquent au jour d (heure
// du cabinet) plus les ouvertures exceptionnelles. typeID == uuid.Nil : tous
// motifs confondus. Les absences ne sont PAS retranchées ici.
func dayOpen(d time.Time, loc *time.Location, rules []sqlcgen.AvailabilityRule, open []booking.Interval, typeID uuid.UUID) []booking.Interval {
	wd := (int(d.Weekday()) + 6) % 7
	var out []booking.Interval
	out = append(out, open...)
	for _, r := range rules {
		if int(r.Weekday) != wd || !r.ValidFrom.Valid || civil(r.ValidFrom.Time) > civil(d) {
			continue
		}
		if r.ValidUntil.Valid && civil(r.ValidUntil.Time) < civil(d) {
			continue
		}
		if typeID != uuid.Nil && r.AppointmentTypeIds != nil && !containsUUID(r.AppointmentTypeIds, typeID) {
			continue
		}
		s, e := int(r.StartTime.Microseconds/6e7), int(r.EndTime.Microseconds/6e7)
		out = append(out, booking.Interval{Start: wallTime(d, s, loc), End: wallTime(d, e, loc)})
	}
	return booking.Union(out)
}

func containsUUID(ids []uuid.UUID, id uuid.UUID) bool {
	for _, x := range ids {
		if x == id {
			return true
		}
	}
	return false
}

// withinOpeningHours indique si [start, end) tient dans les horaires du
// praticien pour ce motif (plages hebdo + ouvertures − absences).
func withinOpeningHours(ctx context.Context, q *sqlcgen.Queries, practitionerID, typeID uuid.UUID, start, end time.Time, loc *time.Location) (bool, error) {
	rules, err := q.ListAvailabilityRules(ctx, practitionerID)
	if err != nil {
		return false, fmt.Errorf("liste des plages: %w", err)
	}
	day := midnight(start.In(loc), loc)
	excs, err := q.ListAvailabilityExceptions(ctx, sqlcgen.ListAvailabilityExceptionsParams{
		PractitionerID: practitionerID, FromAt: day, ToAt: day.AddDate(0, 0, 2),
	})
	if err != nil {
		return false, fmt.Errorf("liste des absences: %w", err)
	}
	closed, open := splitExceptions(excs)
	var avail []booking.Interval
	for i := 0; i < 2; i++ { // le jour du RDV et le suivant (RDV à cheval sur minuit)
		avail = append(avail, dayOpen(day.AddDate(0, 0, i), loc, rules, open, typeID)...)
	}
	avail = booking.Subtract(booking.Union(avail), closed)
	for _, iv := range avail {
		if !start.Before(iv.Start) && !end.After(iv.End) {
			return true, nil
		}
	}
	return false, nil
}

// --- Aides de calcul d'heure murale ---

func midnight(t time.Time, loc *time.Location) time.Time {
	y, m, d := t.In(loc).Date()
	return time.Date(y, m, d, 0, 0, 0, 0, loc)
}

// wallTime construit l'heure murale « minutes depuis minuit » du jour d ;
// 1440 donne minuit le lendemain (time.Date normalise).
func wallTime(d time.Time, minutes int, loc *time.Location) time.Time {
	return time.Date(d.Year(), d.Month(), d.Day(), minutes/60, minutes%60, 0, 0, loc)
}

func frenchDay(t time.Time) string {
	day := fmt.Sprint(t.Day())
	if t.Day() == 1 {
		day = "1er"
	}
	return fmt.Sprintf("%s %s %s", weekdayNames[(int(t.Weekday())+6)%7], day, monthNames[t.Month()-1])
}

var monthNames = [...]string{"janvier", "février", "mars", "avril", "mai", "juin", "juillet", "août", "septembre", "octobre", "novembre", "décembre"}

func intervalDuration(i pgtype.Interval) time.Duration {
	return time.Duration(intervalMicros(i)) * time.Microsecond
}

// --- Création ---

// NewAppointmentInput est la saisie du formulaire de création. Soit PatientID,
// soit les champs NewXxx (création rapide d'un patient).
type NewAppointmentInput struct {
	PatientID                                     string
	NewFirstName, NewLastName, NewPhone, NewEmail string
	TypeID                                        string
	Date, Time                                    string // heure du cabinet
	Force                                         bool   // autorise le hors horaires
	Address                                       string // requise pour un motif à domicile
}

// ErrSlotTakenMsg est le message affiché quand un RDV en chevauche un autre.
const ErrSlotTakenMsg = "Ce créneau n'est pas disponible : il chevauche un autre rendez-vous (temps de préparation et de remise en état compris)."

// Create crée un RDV « staff » pour le praticien connecté. Le hors horaires est
// refusé sauf Force ; un chevauchement est toujours refusé (contrainte EXCLUDE).
func (c *Calendar) Create(ctx context.Context, a Actor, in NewAppointmentInput) (sqlcgen.Appointment, error) {
	var errs domain.ValidationErrors
	add := func(field, msg string) { errs = append(errs, domain.ErrValidation{Field: field, Msg: msg}) }

	patientID, err := uuid.Parse(strings.TrimSpace(in.PatientID))
	hasPatient := err == nil
	if strings.TrimSpace(in.PatientID) != "" && !hasPatient {
		add("patient_id", "Patient invalide.")
	}
	in.NewFirstName, in.NewLastName = strings.TrimSpace(in.NewFirstName), strings.TrimSpace(in.NewLastName)
	in.NewPhone, in.NewEmail = strings.TrimSpace(in.NewPhone), strings.TrimSpace(in.NewEmail)
	quick := !hasPatient && (in.NewFirstName != "" || in.NewLastName != "" || in.NewPhone != "" || in.NewEmail != "")
	var phone string
	switch {
	case hasPatient:
	case quick:
		if in.NewLastName == "" {
			add("new_last_name", "Le nom du patient est requis.")
		}
		if in.NewFirstName == "" {
			add("new_first_name", "Le prénom du patient est requis.")
		}
		if tooLong(in.NewFirstName, 80) || tooLong(in.NewLastName, 80) {
			add("new_last_name", "Le nom et le prénom sont limités à 80 caractères.")
		}
		if in.NewPhone == "" && in.NewEmail == "" {
			add("new_phone", "Renseignez un téléphone ou un email.")
		}
		if in.NewPhone != "" {
			var perr error
			if phone, perr = domain.NormalizePhone(in.NewPhone); perr != nil {
				add("new_phone", "Numéro de téléphone invalide.")
			}
		}
		if in.NewEmail != "" && (tooLong(in.NewEmail, 254) || !strings.Contains(in.NewEmail, "@") || strings.ContainsAny(in.NewEmail, " \t")) {
			add("new_email", "Adresse email invalide.")
		}
	case len(errs) == 0:
		add("patient_id", "Choisissez un patient ou créez-en un.")
	}
	typeID, err := uuid.Parse(strings.TrimSpace(in.TypeID))
	if err != nil {
		add("type_id", "Choisissez un motif.")
	}
	loc, start, startErr := c.parseLocal(ctx, a, in.Date, in.Time)
	if startErr != nil {
		errs = append(errs, domain.FieldErrors(startErr)...)
	}
	in.Address = strings.TrimSpace(in.Address)
	if tooLong(in.Address, maxAddressLen) || strings.ContainsFunc(in.Address, unicode.IsControl) {
		add("address", "L'adresse n'est pas valide (300 caractères maximum, sans retour à la ligne).")
	}
	if len(errs) > 0 {
		return sqlcgen.Appointment{}, errs
	}
	pr, err := c.practice().Practitioner(ctx, a)
	if err != nil {
		return sqlcgen.Appointment{}, err
	}

	// Jeton du lien de gestion du patient (T015) : seul son haché est stocké.
	apptID, token, tokenHash, err := c.ManageKey.mint()
	if err != nil {
		return sqlcgen.Appointment{}, err
	}
	var out sqlcgen.Appointment
	err = store.WithTx(ctx, c.Pool, func(q *sqlcgen.Queries, tx pgx.Tx) error {
		t, err := q.GetAppointmentType(ctx, sqlcgen.GetAppointmentTypeParams{ID: typeID, PracticeID: a.PracticeID})
		if errors.Is(err, pgx.ErrNoRows) || (err == nil && !t.Active) {
			return domain.ErrValidation{Field: "type_id", Msg: "Motif inconnu ou désactivé."}
		}
		if err != nil {
			return fmt.Errorf("lecture motif: %w", err)
		}
		if t.AtHome && in.Address == "" {
			return domain.ErrValidation{Field: "address", Msg: "L'adresse de la visite à domicile est requise."}
		}
		address := ""
		if t.AtHome {
			address = in.Address
		}
		end := start.Add(intervalDuration(t.Duration))
		if !in.Force {
			ok, err := withinOpeningHours(ctx, q, pr.ID, typeID, start, end, loc)
			if err != nil {
				return err
			}
			if !ok {
				return domain.ErrValidation{Field: "date", Msg: "Ce créneau est hors des horaires d'ouverture ; cochez « Hors horaires » pour le forcer."}
			}
		}
		if !hasPatient {
			p, err := q.CreatePatient(ctx, sqlcgen.CreatePatientParams{
				PracticeID: a.PracticeID, FirstName: in.NewFirstName, LastName: in.NewLastName,
				Phone:    pgtype.Text{String: phone, Valid: phone != ""},
				Email:    pgtype.Text{String: in.NewEmail, Valid: in.NewEmail != ""},
				SmsOptIn: true, EmailOptIn: true,
				Address: pgtype.Text{String: address, Valid: address != ""},
			})
			if err != nil {
				return fmt.Errorf("création patient: %w", err)
			}
			patientID = p.ID
		} else if _, err := q.GetPatient(ctx, sqlcgen.GetPatientParams{ID: patientID, PracticeID: a.PracticeID}); errors.Is(err, pgx.ErrNoRows) {
			return domain.ErrValidation{Field: "patient_id", Msg: "Patient introuvable."}
		} else if err != nil {
			return fmt.Errorf("lecture patient: %w", err)
		}
		bb, ba := domain.HomeBuffers(t.AtHome, intervalDuration(t.BufferBefore), intervalDuration(t.BufferAfter), intervalDuration(t.TravelBuffer))
		out, err = store.CreateAppointment(ctx, q, store.NewAppointment{
			ID: apptID, PracticeID: a.PracticeID, PractitionerID: pr.ID, PatientID: patientID, AppointmentTypeID: typeID,
			Start: start, End: end, BufferBefore: bb, BufferAfter: ba, Address: address,
			Source: "staff", ManageTokenHash: tokenHash,
		})
		if err != nil {
			return err
		}
		if err := audit(ctx, q, a, EventAppointmentCreated, out.ID, map[string]string{"start": start.UTC().Format(time.RFC3339), "forced": fmt.Sprint(in.Force)}); err != nil {
			return err
		}
		return c.notifier().booked(ctx, tx, q, out, token)
	})
	if err != nil {
		return sqlcgen.Appointment{}, err
	}
	if c.OnCreated != nil {
		c.OnCreated("staff")
	}
	c.emit(ctx, Event{Kind: EventAppointmentCreated, PracticeID: a.PracticeID, AppointmentID: out.ID})
	return out, nil
}

// parseLocal lit une date « AAAA-MM-JJ » et une heure « HH:MM » du cabinet.
func (c *Calendar) parseLocal(ctx context.Context, a Actor, date, clock string) (*time.Location, time.Time, error) {
	var errs domain.ValidationErrors
	d, err := time.Parse("2006-01-02", strings.TrimSpace(date))
	if err != nil {
		errs = append(errs, domain.ErrValidation{Field: "date", Msg: "La date doit être au format AAAA-MM-JJ."})
	}
	sec, ok := parseClock(clock)
	if !ok {
		errs = append(errs, domain.ErrValidation{Field: "time", Msg: "L'heure doit être au format HH:MM."})
	}
	if len(errs) > 0 {
		return nil, time.Time{}, errs
	}
	loc, err := c.practice().location(ctx, a.PracticeID)
	if err != nil {
		return nil, time.Time{}, err
	}
	return loc, time.Date(d.Year(), d.Month(), d.Day(), int(sec/3600), int(sec/60%60), 0, 0, loc), nil
}

// --- Déplacement, annulation, statut ---

// MoveInput est la saisie du formulaire de déplacement.
type MoveInput struct {
	Date, Time string
	Force      bool
}

// Move déplace un RDV à venir (statut « booked ») en conservant sa durée et ses buffers.
func (c *Calendar) Move(ctx context.Context, a Actor, id uuid.UUID, in MoveInput) error {
	loc, start, err := c.parseLocal(ctx, a, in.Date, in.Time)
	if err != nil {
		return err
	}
	var from time.Time
	err = store.WithTx(ctx, c.Pool, func(q *sqlcgen.Queries, tx pgx.Tx) error {
		ap, err := q.GetAppointment(ctx, sqlcgen.GetAppointmentParams{ID: id, PracticeID: a.PracticeID})
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("lecture rendez-vous: %w", err)
		}
		if !a.mayAccessPractitioner(ap.PractitionerID) {
			return domain.ErrForbidden
		}
		if ap.Status != "booked" {
			return domain.ErrValidation{Field: "date", Msg: "Seul un rendez-vous à venir peut être déplacé."}
		}
		from = ap.During.Lower.Time
		end := start.Add(ap.During.Upper.Time.Sub(from))
		if !in.Force {
			ok, err := withinOpeningHours(ctx, q, ap.PractitionerID, ap.AppointmentTypeID, start, end, loc)
			if err != nil {
				return err
			}
			if !ok {
				return domain.ErrValidation{Field: "date", Msg: "Ce créneau est hors des horaires d'ouverture ; cochez « Hors horaires » pour le forcer."}
			}
		}
		moved, err := store.MoveAppointment(ctx, q, a.PracticeID, id, start, end, intervalDuration(ap.BufferBefore), intervalDuration(ap.BufferAfter))
		if err != nil {
			return err
		}
		if err := audit(ctx, q, a, EventAppointmentMoved, id, map[string]string{
			"from": from.UTC().Format(time.RFC3339), "to": start.UTC().Format(time.RFC3339), "forced": fmt.Sprint(in.Force),
		}); err != nil {
			return err
		}
		// Le jeton de gestion n'est pas connu du cabinet : le message n'a pas de lien.
		return c.notifier().rescheduled(ctx, tx, q, moved, from, "")
	})
	if err != nil {
		return err
	}
	c.emit(ctx, Event{Kind: EventAppointmentMoved, PracticeID: a.PracticeID, AppointmentID: id, Detail: from.UTC().Format(time.RFC3339)})
	return nil
}

// Cancel annule un RDV à venir avec un motif facultatif (200 caractères max).
func (c *Calendar) Cancel(ctx context.Context, a Actor, id uuid.UUID, reason string) error {
	reason = strings.TrimSpace(reason)
	if tooLong(reason, 200) {
		return domain.ErrValidation{Field: "reason", Msg: "Le motif d'annulation est trop long (200 caractères maximum)."}
	}
	err := store.WithTx(ctx, c.Pool, func(q *sqlcgen.Queries, tx pgx.Tx) error {
		if a.Role != "" {
			ap, err := q.GetAppointment(ctx, sqlcgen.GetAppointmentParams{ID: id, PracticeID: a.PracticeID})
			if errors.Is(err, pgx.ErrNoRows) {
				return domain.ErrNotFound
			}
			if err != nil {
				return fmt.Errorf("lecture rendez-vous: %w", err)
			}
			if !a.mayAccessPractitioner(ap.PractitionerID) {
				return domain.ErrForbidden
			}
		}
		cancelled, err := q.CancelAppointment(ctx, sqlcgen.CancelAppointmentParams{
			ID: id, PracticeID: a.PracticeID,
			CancelledAt:  pgtype.Timestamptz{Time: c.Clock.Now(), Valid: true},
			CancelledBy:  pgtype.Text{String: "staff", Valid: true},
			CancelReason: pgtype.Text{String: reason, Valid: reason != ""},
		})
		if errors.Is(err, pgx.ErrNoRows) {
			if _, gerr := q.GetAppointment(ctx, sqlcgen.GetAppointmentParams{ID: id, PracticeID: a.PracticeID}); gerr != nil {
				if errors.Is(gerr, pgx.ErrNoRows) {
					return domain.ErrNotFound
				}
				return fmt.Errorf("lecture rendez-vous: %w", gerr)
			}
			return domain.ErrValidation{Field: "reason", Msg: "Seul un rendez-vous à venir peut être annulé."}
		}
		if err != nil {
			return fmt.Errorf("annulation: %w", err)
		}
		if err := audit(ctx, q, a, EventAppointmentCancelled, id, map[string]string{"has_reason": fmt.Sprint(reason != "")}); err != nil {
			return err
		}
		return c.notifier().cancelled(ctx, tx, q, cancelled, true)
	})
	if err != nil {
		return err
	}
	c.emit(ctx, Event{Kind: EventAppointmentCancelled, PracticeID: a.PracticeID, AppointmentID: id})
	return nil
}

// SetStatus marque un RDV non annulé comme honoré (done), absent (no_show) ou
// de nouveau à venir (booked).
func (c *Calendar) SetStatus(ctx context.Context, a Actor, id uuid.UUID, status string) error {
	switch status {
	case "done", "no_show", "booked":
	default:
		return domain.ErrValidation{Field: "status", Msg: "Statut inconnu."}
	}
	err := store.WithTx(ctx, c.Pool, func(q *sqlcgen.Queries, tx pgx.Tx) error {
		ap, err := q.GetAppointment(ctx, sqlcgen.GetAppointmentParams{ID: id, PracticeID: a.PracticeID})
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("lecture rendez-vous: %w", err)
		}
		if !a.mayAccessPractitioner(ap.PractitionerID) {
			return domain.ErrForbidden
		}
		if ap.Status == "cancelled" {
			return domain.ErrValidation{Field: "status", Msg: "Un rendez-vous annulé ne peut pas changer de statut."}
		}
		if _, err := q.SetAppointmentStatus(ctx, sqlcgen.SetAppointmentStatusParams{ID: id, PracticeID: a.PracticeID, Status: status,
			ChangedBy: pgtype.UUID{Bytes: a.UserID, Valid: a.UserID != uuid.Nil},
			ChangedAt: pgtype.Timestamptz{Time: c.Clock.Now(), Valid: true},
		}); err != nil {
			return fmt.Errorf("changement de statut: %w", err)
		}
		if status != "booked" {
			// Honoré ou absent : plus de rappel à envoyer.
			if err := outbox.CancelForAppointment(ctx, tx, c.Clock, id); err != nil {
				return err
			}
		}
		return audit(ctx, q, a, EventAppointmentStatus, id, map[string]string{"status": status})
	})
	if err != nil {
		return err
	}
	c.emit(ctx, Event{Kind: EventAppointmentStatus, PracticeID: a.PracticeID, AppointmentID: id, Detail: status})
	return nil
}

func audit(ctx context.Context, q *sqlcgen.Queries, a Actor, action string, id uuid.UUID, meta map[string]string) error {
	b, err := json.Marshal(meta)
	if err != nil {
		return fmt.Errorf("meta audit: %w", err)
	}
	if err := q.CreateAuditLog(ctx, sqlcgen.CreateAuditLogParams{
		UserID: pgtype.UUID{Bytes: a.UserID, Valid: true}, PracticeID: pgtype.UUID{Bytes: a.PracticeID, Valid: true}, Actor: "user", Action: action,
		Entity: "appointment", EntityID: pgtype.UUID{Bytes: id, Valid: true}, Meta: b,
	}); err != nil {
		return fmt.Errorf("journal d'audit: %w", err)
	}
	return nil
}

// --- Détail d'un RDV ---

// HistoryEntry est une ligne de l'historique d'un RDV.
type HistoryEntry struct {
	At   time.Time
	Text string
}

// AppointmentDetail est la fiche d'un RDV.
type AppointmentDetail struct {
	ID                 uuid.UUID
	Start, End         time.Time
	Status             string
	StatusLabel        string
	Source             string
	SourceLabel        string
	Patient            string
	Phone, Email       string
	TypeName           string
	CancelReason       string
	CancelledAt        time.Time
	CreatedAt          time.Time
	History            []HistoryEntry
	AtHome             bool
	Address            string // adresse de la visite à domicile
	CanMove, CanCancel bool
	MoveDate, MoveTime string // valeurs par défaut du formulaire de déplacement
}

var statusLabels = map[string]string{"booked": "À venir", "done": "Honoré", "no_show": "Absent", "cancelled": "Annulé"}

// Detail retourne la fiche d'un RDV du cabinet (ErrNotFound sinon).
func (c *Calendar) Detail(ctx context.Context, a Actor, id uuid.UUID) (AppointmentDetail, error) {
	loc, err := c.practice().location(ctx, a.PracticeID)
	if err != nil {
		return AppointmentDetail{}, err
	}
	q := sqlcgen.New(c.Pool)
	r, err := q.GetAppointmentDetail(ctx, sqlcgen.GetAppointmentDetailParams{ID: id, PracticeID: a.PracticeID})
	if errors.Is(err, pgx.ErrNoRows) {
		return AppointmentDetail{}, domain.ErrNotFound
	}
	if err != nil {
		return AppointmentDetail{}, fmt.Errorf("lecture rendez-vous: %w", err)
	}
	if !a.mayAccessPractitioner(r.PractitionerID) {
		return AppointmentDetail{}, domain.ErrForbidden
	}
	d := AppointmentDetail{
		ID: r.ID, Start: r.StartsAt.In(loc), End: r.EndsAt.In(loc), Status: r.Status, StatusLabel: statusLabels[r.Status],
		Source: r.Source, Patient: r.FirstName + " " + r.LastName, Phone: r.Phone.String, Email: r.Email.String,
		TypeName: r.TypeName, AtHome: r.TypeAtHome, Address: r.Address.String, CancelReason: r.CancelReason.String, CreatedAt: r.CreatedAt.In(loc),
		CanMove: r.Status == "booked", CanCancel: r.Status == "booked",
	}
	d.SourceLabel = map[string]string{"online": "Réservation en ligne", "staff": "Saisi par le cabinet"}[r.Source]
	if r.CancelledAt.Valid {
		d.CancelledAt = r.CancelledAt.Time.In(loc)
	}
	d.MoveDate, d.MoveTime = d.Start.Format("2006-01-02"), d.Start.Format("15:04")

	logs, err := q.ListAuditLogForEntity(ctx, sqlcgen.ListAuditLogForEntityParams{
		Entity: "appointment", EntityID: pgtype.UUID{Bytes: id, Valid: true}, PageSize: 50,
	})
	if err != nil {
		return AppointmentDetail{}, fmt.Errorf("historique: %w", err)
	}
	for _, l := range logs {
		var meta map[string]string
		_ = json.Unmarshal(l.Meta, &meta)
		d.History = append(d.History, HistoryEntry{At: l.At.In(loc), Text: historyText(l.Action, meta, loc)})
	}
	return d, nil
}

func historyText(action string, meta map[string]string, loc *time.Location) string {
	at := func(k string) string {
		t, err := time.Parse(time.RFC3339, meta[k])
		if err != nil {
			return "?"
		}
		return frenchDay(t.In(loc)) + " à " + t.In(loc).Format("15h04")
	}
	switch action {
	case EventAppointmentCreated:
		return "Rendez-vous créé pour le " + at("start")
	case EventAppointmentMoved:
		return "Déplacé du " + at("from") + " au " + at("to")
	case EventAppointmentCancelled:
		return "Rendez-vous annulé"
	case EventAppointmentStatus:
		return "Statut : " + statusLabels[meta["status"]]
	}
	return action
}

// --- Aides du formulaire de création ---

// ActiveTypes retourne les motifs actifs du cabinet (formulaire de création).
func (c *Calendar) ActiveTypes(ctx context.Context, a Actor) ([]sqlcgen.AppointmentType, error) {
	all, err := c.practice().AppointmentTypes(ctx, a)
	if err != nil {
		return nil, err
	}
	out := all[:0:0]
	for _, t := range all {
		if t.Active {
			out = append(out, t)
		}
	}
	return out, nil
}

// SearchPatients cherche des patients du cabinet par préfixe de nom, prénom,
// téléphone ou email (2 caractères minimum, 10 résultats maximum).
func (c *Calendar) SearchPatients(ctx context.Context, a Actor, query string) ([]sqlcgen.Patient, error) {
	query = strings.TrimSpace(query)
	if len([]rune(query)) < 2 {
		return nil, nil
	}
	esc := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(query)
	ps, err := sqlcgen.New(c.Pool).SearchPatients(ctx, sqlcgen.SearchPatientsParams{PracticeID: a.PracticeID, Prefix: esc, PageSize: patientSearchLimit})
	if err != nil {
		return nil, fmt.Errorf("recherche de patients: %w", err)
	}
	return ps, nil
}

// Patient retourne un patient du cabinet (ErrNotFound sinon).
func (c *Calendar) Patient(ctx context.Context, a Actor, id uuid.UUID) (sqlcgen.Patient, error) {
	p, err := sqlcgen.New(c.Pool).GetPatient(ctx, sqlcgen.GetPatientParams{ID: id, PracticeID: a.PracticeID})
	if errors.Is(err, pgx.ErrNoRows) {
		return sqlcgen.Patient{}, domain.ErrNotFound
	}
	if err != nil {
		return sqlcgen.Patient{}, fmt.Errorf("lecture patient: %w", err)
	}
	return p, nil
}
