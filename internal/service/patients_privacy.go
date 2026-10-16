package service

import (
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"doctolibre/internal/domain"
	"doctolibre/internal/store"
	"doctolibre/internal/store/sqlcgen"
)

// Erase supprime ou anonymise la fiche id (et les doublons fusionnés dedans) :
// « deleted » si le patient n'a jamais eu de rendez-vous, « anonymized » sinon.
// Des rendez-vous à venir bloquent l'opération (ErrValidation).
func (s *Patients) Erase(ctx context.Context, a Actor, id uuid.UUID) (string, error) {
	var mode string
	err := store.WithTx(ctx, s.Pool, func(q *sqlcgen.Queries, _ pgx.Tx) error {
		var err error
		mode, err = erasePatient(ctx, q, a.PracticeID, uuid.NullUUID{UUID: a.UserID, Valid: true}, id, s.Clock.Now(), true)
		return err
	})
	return mode, err
}

// AuditView journalise la consultation d'une fiche (action patient_view).
func (s *Patients) AuditView(ctx context.Context, a Actor, id uuid.UUID) error {
	return store.WithTx(ctx, s.Pool, func(q *sqlcgen.Queries, _ pgx.Tx) error {
		return writeAudit(ctx, q, a.PracticeID, uuid.NullUUID{UUID: a.UserID, Valid: true}, ActionPatientView, "patient", id, map[string]string{})
	})
}

// Export : toutes les données d'un patient (droit d'accès, portabilité).
// Les dates sont en RFC 3339, dans le fuseau du cabinet.

// PatientExport regroupe la fiche, les rendez-vous, les séries et les messages.
type PatientExport struct {
	ExportedAt   string             `json:"exported_at"`
	Patient      ExportedPatient    `json:"patient"`
	Appointments []ExportedAppt     `json:"appointments"`
	CarePlans    []ExportedCarePlan `json:"care_plans"`
	Waitlist     []ExportedWaitlist `json:"waitlist"`
	Messages     []ExportedMessage  `json:"messages"`
}

// ExportedPatient est la fiche du patient.
type ExportedPatient struct {
	ID         string `json:"id"`
	FirstName  string `json:"first_name"`
	LastName   string `json:"last_name"`
	BirthDate  string `json:"birth_date,omitempty"`
	Phone      string `json:"phone,omitempty"`
	Email      string `json:"email,omitempty"`
	Address    string `json:"address,omitempty"`
	SMSOptIn   bool   `json:"sms_opt_in"`
	EmailOptIn bool   `json:"email_opt_in"`
	NotesAdmin string `json:"notes_admin,omitempty"`
	CreatedAt  string `json:"created_at"`
}

// ExportedAppt est un rendez-vous du patient.
type ExportedAppt struct {
	ID           string `json:"id"`
	Start        string `json:"start"`
	End          string `json:"end"`
	Status       string `json:"status"`
	Source       string `json:"source"`
	Type         string `json:"type"`
	Address      string `json:"address,omitempty"`
	CancelReason string `json:"cancel_reason,omitempty"`
	CancelledAt  string `json:"cancelled_at,omitempty"`
	CreatedAt    string `json:"created_at"`
}

// ExportedCarePlan est une série de séances.
type ExportedCarePlan struct {
	ID                 string `json:"id"`
	Label              string `json:"label,omitempty"`
	Type               string `json:"type"`
	SessionsPrescribed int    `json:"sessions_prescribed"`
	FrequencyPerWeek   int    `json:"frequency_per_week"`
	StartDate          string `json:"start_date"`
	Status             string `json:"status"`
}

// ExportedWaitlist est une inscription en liste d'attente.
type ExportedWaitlist struct {
	ID           string `json:"id"`
	Type         string `json:"type"`
	Practitioner string `json:"practitioner,omitempty"`
	Earliest     string `json:"earliest"`
	Latest       string `json:"latest"`
	Weekdays     []int  `json:"weekdays"`
	TimeFrom     string `json:"time_from,omitempty"`
	TimeTo       string `json:"time_to,omitempty"`
	Status       string `json:"status"`
	CreatedAt    string `json:"created_at"`
}

// ExportedMessage est un email ou SMS envoyé au patient (sans son contenu).
type ExportedMessage struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	Template  string `json:"template"`
	Status    string `json:"status"`
	Recipient string `json:"recipient,omitempty"`
	CreatedAt string `json:"created_at"`
	SentAt    string `json:"sent_at,omitempty"`
}

// Export rassemble les données du patient et journalise l'export (format : "json" ou "csv").
func (s *Patients) Export(ctx context.Context, a Actor, id uuid.UUID, format string) (PatientExport, error) {
	loc, err := (&Practice{Pool: s.Pool, Clock: s.Clock}).location(ctx, a.PracticeID)
	if err != nil {
		return PatientExport{}, err
	}
	ts := func(t time.Time) string { return t.In(loc).Format(time.RFC3339) }
	tsn := func(t pgtype.Timestamptz) string {
		if !t.Valid {
			return ""
		}
		return ts(t.Time)
	}
	var out PatientExport
	err = store.WithTx(ctx, s.Pool, func(q *sqlcgen.Queries, _ pgx.Tx) error {
		p, err := q.GetPatient(ctx, sqlcgen.GetPatientParams{ID: id, PracticeID: a.PracticeID})
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("lecture patient: %w", err)
		}
		out = PatientExport{ExportedAt: ts(s.Clock.Now())}
		out.Patient = ExportedPatient{
			ID: p.ID.String(), FirstName: p.FirstName, LastName: p.LastName, Phone: p.Phone.String, Email: p.Email.String,
			Address: p.Address.String, SMSOptIn: p.SmsOptIn, EmailOptIn: p.EmailOptIn, NotesAdmin: p.NotesAdmin, CreatedAt: ts(p.CreatedAt),
		}
		if p.BirthDate.Valid {
			out.Patient.BirthDate = p.BirthDate.Time.Format("2006-01-02")
		}
		appts, err := q.ExportPatientAppointments(ctx, sqlcgen.ExportPatientAppointmentsParams{PatientID: id, PracticeID: a.PracticeID})
		if err != nil {
			return fmt.Errorf("rendez-vous du patient: %w", err)
		}
		out.Appointments = make([]ExportedAppt, 0, len(appts))
		for _, r := range appts {
			out.Appointments = append(out.Appointments, ExportedAppt{
				ID: r.ID.String(), Start: ts(r.StartsAt), End: ts(r.EndsAt), Status: r.Status, Source: r.Source, Type: r.TypeName,
				Address: r.Address.String, CancelReason: r.CancelReason.String, CancelledAt: tsn(r.CancelledAt), CreatedAt: ts(r.CreatedAt),
			})
		}
		plans, err := q.ListCarePlansForPatient(ctx, sqlcgen.ListCarePlansForPatientParams{PracticeID: a.PracticeID, PatientID: id})
		if err != nil {
			return fmt.Errorf("séries du patient: %w", err)
		}
		out.CarePlans = make([]ExportedCarePlan, 0, len(plans))
		for _, c := range plans {
			ep := ExportedCarePlan{
				ID: c.ID.String(), Label: c.Label, Type: c.TypeName, SessionsPrescribed: int(c.SessionsPrescribed),
				FrequencyPerWeek: int(c.FrequencyPerWeek), Status: c.Status,
			}
			if c.StartDate.Valid {
				ep.StartDate = c.StartDate.Time.Format("2006-01-02")
			}
			out.CarePlans = append(out.CarePlans, ep)
		}
		wl, err := q.ExportPatientWaitlist(ctx, sqlcgen.ExportPatientWaitlistParams{PracticeID: a.PracticeID, PatientID: id})
		if err != nil {
			return fmt.Errorf("liste d'attente du patient: %w", err)
		}
		out.Waitlist = make([]ExportedWaitlist, 0, len(wl))
		for _, w := range wl {
			ew := ExportedWaitlist{
				ID: w.ID.String(), Type: w.TypeName, Earliest: w.Earliest.Time.Format("2006-01-02"), Latest: w.Latest.Time.Format("2006-01-02"),
				Weekdays: make([]int, 0, len(w.Weekdays)), Status: w.Status, CreatedAt: ts(w.CreatedAt),
			}
			if w.PractitionerFirstName.Valid {
				ew.Practitioner = strings.TrimSpace(w.PractitionerFirstName.String + " " + w.PractitionerLastName.String)
			}
			for _, d := range w.Weekdays {
				ew.Weekdays = append(ew.Weekdays, int(d))
			}
			if w.TimeFrom.Valid {
				ew.TimeFrom = time.UnixMicro(w.TimeFrom.Microseconds).UTC().Format("15:04")
			}
			if w.TimeTo.Valid {
				ew.TimeTo = time.UnixMicro(w.TimeTo.Microseconds).UTC().Format("15:04")
			}
			out.Waitlist = append(out.Waitlist, ew)
		}
		var recipients []string
		if p.Phone.Valid {
			recipients = append(recipients, strings.ToLower(p.Phone.String))
		}
		if p.Email.Valid {
			recipients = append(recipients, strings.ToLower(p.Email.String))
		}
		msgs, err := q.ExportPatientMessages(ctx, sqlcgen.ExportPatientMessagesParams{PracticeID: a.PracticeID, PatientID: id, Recipients: recipients})
		if err != nil {
			return fmt.Errorf("messages du patient: %w", err)
		}
		out.Messages = make([]ExportedMessage, 0, len(msgs))
		for _, m := range msgs {
			out.Messages = append(out.Messages, ExportedMessage{
				ID: m.ID.String(), Kind: m.Kind, Template: m.Template, Status: m.Status, Recipient: m.Recipient,
				CreatedAt: ts(m.CreatedAt), SentAt: tsn(m.SentAt),
			})
		}
		return writeAudit(ctx, q, a.PracticeID, uuid.NullUUID{UUID: a.UserID, Valid: true}, ActionPatientExported, "patient", id,
			map[string]string{"format": format})
	})
	return out, err
}

// csvSafe neutralise l'injection de formules des tableurs : une cellule qui
// commencerait par = @ - ou une tabulation, ou par + suivi d'autre chose que des
// chiffres et des espaces (un numéro « +33612345678 » reste intact), est
// préfixée d'une apostrophe.
func csvSafe(s string) string {
	if s == "" {
		return s
	}
	switch s[0] {
	case '=', '@', '-', '\t', '\r':
		return "'" + s
	case '+':
		if len(s) > 1 && strings.Trim(s[1:], "0123456789 ") == "" {
			return s
		}
		return "'" + s
	}
	return s
}

// WriteCSV écrit un tableau : une ligne par rendez-vous, avec les coordonnées
// du patient répétées (une seule ligne, rendez-vous vides, s'il n'en a aucun).
func (e PatientExport) WriteCSV(w io.Writer) error {
	cw := csv.NewWriter(w)
	header := []string{
		"patient_id", "nom", "prenom", "date_naissance", "telephone", "email", "adresse", "sms_autorises", "email_autorises", "note_administrative",
		"rdv_id", "rdv_debut", "rdv_fin", "rdv_statut", "rdv_origine", "rdv_motif", "rdv_adresse", "rdv_motif_annulation",
	}
	if err := cw.Write(header); err != nil {
		return err
	}
	p := e.Patient
	base := []string{p.ID, p.LastName, p.FirstName, p.BirthDate, p.Phone, p.Email, p.Address, fmt.Sprint(p.SMSOptIn), fmt.Sprint(p.EmailOptIn), p.NotesAdmin}
	write := func(extra []string) error {
		row := append(append([]string{}, base...), extra...)
		for i := range row {
			row[i] = csvSafe(row[i])
		}
		return cw.Write(row)
	}
	if len(e.Appointments) == 0 {
		if err := write(make([]string, 8)); err != nil {
			return err
		}
	}
	for _, a := range e.Appointments {
		if err := write([]string{a.ID, a.Start, a.End, a.Status, a.Source, a.Type, a.Address, a.CancelReason}); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}
