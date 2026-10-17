package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"doctolibre/internal/domain"
	"doctolibre/internal/store"
	"doctolibre/internal/store/sqlcgen"
)

// Paramètres des fiches patients.
const (
	PatientPageSize = 25
	maxPatientNote  = 500
	minPhoneDigits  = 6
)

// Patients porte les fiches patients du cabinet : recherche, édition, création
// avec détection de doublon et fusion.
type Patients struct {
	Pool  *pgxpool.Pool
	Clock domain.Clock
}

// PatientList est une page de résultats (keyset). Next est vide sur la dernière page.
type PatientList struct {
	Patients []sqlcgen.Patient
	Next     string
}

// List retourne une page de patients, filtrée par q si non vide (nom, prénom,
// téléphone, email ; insensible à la casse et aux accents). after est le curseur Next.
func (s *Patients) List(ctx context.Context, a Actor, q, after string) (PatientList, error) {
	q = strings.TrimSpace(q)
	last, first, id := decodeCursor(after)
	var rows []sqlcgen.Patient
	var err error
	if q == "" {
		rows, err = sqlcgen.New(s.Pool).ListPatientsPage(ctx, sqlcgen.ListPatientsPageParams{
			PracticeID: a.PracticeID, AfterLastName: last, AfterFirstName: first, AfterID: id, PageSize: PatientPageSize + 1,
		})
	} else {
		phone := ""
		if digitCount(q) >= minPhoneDigits {
			if n, perr := domain.NormalizePhone(q); perr == nil {
				phone = n
			}
		}
		esc := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(q)
		rows, err = sqlcgen.New(s.Pool).SearchPatientsPage(ctx, sqlcgen.SearchPatientsPageParams{
			PracticeID: a.PracticeID, Term: esc, Phone: phone,
			AfterLastName: last, AfterFirstName: first, AfterID: id, PageSize: PatientPageSize + 1,
		})
	}
	if err != nil {
		return PatientList{}, fmt.Errorf("liste des patients: %w", err)
	}
	out := PatientList{Patients: rows}
	if len(rows) > PatientPageSize {
		out.Patients = rows[:PatientPageSize]
		l := out.Patients[PatientPageSize-1]
		out.Next = encodeCursor(l.LastName, l.FirstName, l.ID)
	}
	return out, nil
}

func digitCount(s string) int {
	n := 0
	for _, r := range s {
		if r >= '0' && r <= '9' {
			n++
		}
	}
	return n
}

func encodeCursor(last, first string, id uuid.UUID) string {
	return base64.RawURLEncoding.EncodeToString([]byte(last + "\x00" + first + "\x00" + id.String()))
}

// decodeCursor lit un curseur ; un curseur invalide repart du début.
func decodeCursor(c string) (last, first string, id uuid.UUID) {
	b, err := base64.RawURLEncoding.DecodeString(c)
	if err != nil {
		return "", "", uuid.Nil
	}
	parts := strings.Split(string(b), "\x00")
	if len(parts) != 3 {
		return "", "", uuid.Nil
	}
	u, err := uuid.Parse(parts[2])
	if err != nil {
		return "", "", uuid.Nil
	}
	return parts[0], parts[1], u
}

// PatientAppt est un RDV d'un patient.
type PatientAppt struct {
	ID          uuid.UUID
	Start, End  time.Time
	Status      string
	StatusLabel string
	TypeName    string
}

// PatientDetail est la fiche d'un patient avec ses RDV.
type PatientDetail struct {
	Patient  sqlcgen.Patient
	Upcoming []PatientAppt // du plus proche au plus lointain
	History  []PatientAppt // du plus récent au plus ancien
	NoShows  int
	// OnlineBlocked : le seuil d'absences du cabinet est atteint (badge « Réservation en ligne bloquée »).
	OnlineBlocked bool
}

// Detail retourne la fiche (ErrNotFound si absente ou hors du cabinet).
func (s *Patients) Detail(ctx context.Context, a Actor, id uuid.UUID) (PatientDetail, error) {
	q := sqlcgen.New(s.Pool)
	p, err := q.GetPatient(ctx, sqlcgen.GetPatientParams{ID: id, PracticeID: a.PracticeID})
	if errors.Is(err, pgx.ErrNoRows) {
		return PatientDetail{}, domain.ErrNotFound
	}
	if err != nil {
		return PatientDetail{}, fmt.Errorf("lecture patient: %w", err)
	}
	rows, err := q.ListPatientAppointments(ctx, sqlcgen.ListPatientAppointmentsParams{PatientID: id, PracticeID: a.PracticeID})
	if err != nil {
		return PatientDetail{}, fmt.Errorf("rendez-vous du patient: %w", err)
	}
	loc, err := (&Practice{Pool: s.Pool, Clock: s.Clock}).location(ctx, a.PracticeID)
	if err != nil {
		return PatientDetail{}, err
	}
	now := s.Clock.Now()
	d := PatientDetail{Patient: p}
	for _, r := range rows {
		ap := PatientAppt{ID: r.ID, Start: r.StartsAt.In(loc), End: r.EndsAt.In(loc), Status: r.Status, StatusLabel: statusLabels[r.Status], TypeName: r.TypeName}
		if r.Status == "no_show" {
			d.NoShows++
		}
		if r.Status == "booked" && !r.StartsAt.Before(now) {
			d.Upcoming = append([]PatientAppt{ap}, d.Upcoming...) // la requête est décroissante
		} else {
			d.History = append(d.History, ap)
		}
	}
	pc, err := q.GetPractice(ctx, a.PracticeID)
	if err != nil {
		return PatientDetail{}, fmt.Errorf("lecture cabinet: %w", err)
	}
	if d.OnlineBlocked, err = onlineBookingBlocked(ctx, q, pc, id, now); err != nil {
		return PatientDetail{}, err
	}
	return d, nil
}

func validNote(note string) (string, error) {
	note = strings.TrimSpace(note)
	if utf8.RuneCountInString(note) > maxPatientNote {
		return "", domain.ErrValidation{Field: "notes_admin", Msg: fmt.Sprintf("La note est limitée à %d caractères.", maxPatientNote)}
	}
	return note, nil
}

// validate regroupe les erreurs des coordonnées et de la note.
func (s *Patients) validate(in PatientDetails, note string) (details, string, error) {
	d, err := parseDetails(in, channelFor(in), s.Clock.Now())
	var errs domain.ValidationErrors
	if err != nil {
		errs = append(errs, domain.FieldErrors(err)...)
	}
	n, nerr := validNote(note)
	if nerr != nil {
		errs = append(errs, domain.FieldErrors(nerr)...)
	}
	if len(errs) > 0 {
		return details{}, "", errs
	}
	return d, n, nil
}

func auditPatient(ctx context.Context, q *sqlcgen.Queries, a Actor, action string, id uuid.UUID, meta map[string]string) error {
	b, err := json.Marshal(meta)
	if err != nil {
		return fmt.Errorf("meta audit: %w", err)
	}
	if err := q.CreateAuditLog(ctx, sqlcgen.CreateAuditLogParams{
		UserID: pgtype.UUID{Bytes: a.UserID, Valid: true}, PracticeID: pgtype.UUID{Bytes: a.PracticeID, Valid: true}, Actor: "user", Action: action,
		Entity: "patient", EntityID: pgtype.UUID{Bytes: id, Valid: true}, Meta: b,
	}); err != nil {
		return fmt.Errorf("journal d'audit: %w", err)
	}
	return nil
}

// Update modifie les coordonnées, les préférences et la note d'un patient.
func (s *Patients) Update(ctx context.Context, a Actor, id uuid.UUID, in PatientDetails, note string) (sqlcgen.Patient, error) {
	d, n, err := s.validate(in, note)
	if err != nil {
		return sqlcgen.Patient{}, err
	}
	var out sqlcgen.Patient
	err = store.WithTx(ctx, s.Pool, func(q *sqlcgen.Queries, _ pgx.Tx) error {
		var err error
		out, err = q.UpdatePatient(ctx, sqlcgen.UpdatePatientParams{
			ID: id, PracticeID: a.PracticeID, FirstName: d.firstName, LastName: d.lastName, BirthDate: d.birth,
			Phone:    pgtype.Text{String: d.phone, Valid: d.phone != ""},
			Email:    pgtype.Text{String: d.email, Valid: d.email != ""},
			SmsOptIn: d.smsOptIn && d.phone != "", EmailOptIn: d.emailOptIn && d.email != "", NotesAdmin: n,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("mise à jour patient: %w", err)
		}
		return auditPatient(ctx, q, a, "patient.updated", id, map[string]string{})
	})
	return out, err
}

// Create crée une fiche. Sans force, si un patient actif partage le téléphone
// ou l'email, rien n'est créé et les doublons possibles sont retournés.
func (s *Patients) Create(ctx context.Context, a Actor, in PatientDetails, note string, force bool) (sqlcgen.Patient, []sqlcgen.Patient, error) {
	d, n, err := s.validate(in, note)
	if err != nil {
		return sqlcgen.Patient{}, nil, err
	}
	var out sqlcgen.Patient
	var dups []sqlcgen.Patient
	err = store.WithTx(ctx, s.Pool, func(q *sqlcgen.Queries, _ pgx.Tx) error {
		if !force {
			var err error
			dups, err = q.FindPatientDuplicates(ctx, sqlcgen.FindPatientDuplicatesParams{PracticeID: a.PracticeID, Phone: d.phone, Email: d.email})
			if err != nil {
				return fmt.Errorf("recherche de doublons: %w", err)
			}
			if len(dups) > 0 {
				return nil
			}
		}
		var err error
		out, err = q.CreatePatient(ctx, sqlcgen.CreatePatientParams{
			PracticeID: a.PracticeID, FirstName: d.firstName, LastName: d.lastName, BirthDate: d.birth,
			Phone:    pgtype.Text{String: d.phone, Valid: d.phone != ""},
			Email:    pgtype.Text{String: d.email, Valid: d.email != ""},
			SmsOptIn: d.smsOptIn && d.phone != "", EmailOptIn: d.emailOptIn && d.email != "", NotesAdmin: n,
		})
		if err != nil {
			return fmt.Errorf("création patient: %w", err)
		}
		return auditPatient(ctx, q, a, "patient.created", out.ID, map[string]string{})
	})
	if err != nil {
		return sqlcgen.Patient{}, nil, err
	}
	return out, dups, nil
}

// Merge fusionne la fiche from (le doublon) dans la fiche into (conservée) :
// les RDV sont réassignés, from est soft-deleted, l'opt-out le plus restrictif
// et les coordonnées manquantes sont repris, et l'opération est journalisée.
// Retourne le nombre de RDV réassignés.
func (s *Patients) Merge(ctx context.Context, a Actor, from, into uuid.UUID) (int64, error) {
	if from == into {
		return 0, domain.ErrValidation{Field: "into", Msg: "Choisissez une autre fiche pour la fusion."}
	}
	var moved int64
	err := store.WithTx(ctx, s.Pool, func(q *sqlcgen.Queries, _ pgx.Tx) error {
		// Verrous dans l'ordre des identifiants : pas d'interblocage entre deux fusions croisées.
		ids := []uuid.UUID{from, into}
		if strings.Compare(from.String(), into.String()) > 0 {
			ids = []uuid.UUID{into, from}
		}
		got := map[uuid.UUID]sqlcgen.Patient{}
		for _, id := range ids {
			p, err := q.LockPatient(ctx, sqlcgen.LockPatientParams{ID: id, PracticeID: a.PracticeID})
			if errors.Is(err, pgx.ErrNoRows) {
				return domain.ErrNotFound
			}
			if err != nil {
				return fmt.Errorf("verrou patient: %w", err)
			}
			got[id] = p
		}
		src := got[from]
		var err error
		moved, err = q.ReassignPatientAppointments(ctx, sqlcgen.ReassignPatientAppointmentsParams{IntoID: into, FromID: from, PracticeID: a.PracticeID})
		if err != nil {
			return fmt.Errorf("réassignation des rendez-vous: %w", err)
		}
		// Les liens de série du doublon sont révoqués avant le transfert (T055).
		if err := q.RevokeCarePlanTokens(ctx, sqlcgen.RevokeCarePlanTokensParams{PatientID: from, PracticeID: a.PracticeID}); err != nil {
			return fmt.Errorf("révocation des liens de série: %w", err)
		}
		if _, err := q.ReassignPatientCarePlans(ctx, sqlcgen.ReassignPatientCarePlansParams{IntoID: into, FromID: from, PracticeID: a.PracticeID}); err != nil {
			return fmt.Errorf("réassignation des séries: %w", err)
		}
		if _, err := q.ReassignPatientWaitlist(ctx, sqlcgen.ReassignPatientWaitlistParams{IntoID: into, FromID: from, PracticeID: a.PracticeID}); err != nil {
			return fmt.Errorf("réassignation de la liste d'attente: %w", err)
		}
		if err := q.MergePatientPrefs(ctx, sqlcgen.MergePatientPrefsParams{
			ID: into, PracticeID: a.PracticeID, SmsOptIn: src.SmsOptIn, EmailOptIn: src.EmailOptIn,
			Phone: src.Phone.String, Email: src.Email.String,
		}); err != nil {
			return fmt.Errorf("fusion des préférences: %w", err)
		}
		if err := q.AbsorbPatient(ctx, sqlcgen.AbsorbPatientParams{IntoID: pgtype.UUID{Bytes: into, Valid: true}, FromID: from, PracticeID: a.PracticeID}); err != nil {
			return fmt.Errorf("soft-delete du doublon: %w", err)
		}
		return auditPatient(ctx, q, a, "patient.merged", into, map[string]string{"merged": from.String(), "appointments": fmt.Sprint(moved)})
	})
	return moved, err
}
