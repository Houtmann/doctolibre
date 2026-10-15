package service

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"doctolibre/internal/domain"
	"doctolibre/internal/store/sqlcgen"
)

// PatientDetails est la saisie brute du formulaire de réservation.
type PatientDetails struct {
	FirstName, LastName string
	Phone, Email        string
	BirthDate           string // "2006-01-02", optionnelle
	SMSOptIn            bool
	EmailOptIn          bool
}

// details est la saisie validée et normalisée.
type details struct {
	firstName, lastName string
	phone               string // E.164 ou ""
	email               string // minuscules ou ""
	birth               pgtype.Date
	smsOptIn            bool
	emailOptIn          bool
}

// recipient retourne la coordonnée qui reçoit le code pour ce canal.
func (d details) recipient(channel string) string {
	if channel == channelEmail {
		return d.email
	}
	return d.phone
}

// binding lie un code à un créneau et à des coordonnées précises : rien de ce qui
// a été vérifié ne peut être modifié entre l'envoi du code et la réservation.
func (d details) binding(practitioner, typeID uuid.UUID, start time.Time, channel string) string {
	return sha256Hex(practitioner.String(), typeID.String(), fmt.Sprint(start.UTC().Unix()), channel,
		d.firstName, d.lastName, d.phone, d.email, fmt.Sprint(d.birth.Valid, d.birth.Time.Format("2006-01-02")),
		fmt.Sprint(d.smsOptIn), fmt.Sprint(d.emailOptIn))
}

const maxNameLen = 80

func validName(s string) bool {
	if s == "" || utf8.RuneCountInString(s) > maxNameLen {
		return false
	}
	for _, r := range s {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

// parseDetails valide la saisie pour le canal de vérification choisi.
// Les erreurs sont des domain.ValidationErrors (une par champ).
func parseDetails(in PatientDetails, channel string, now time.Time) (details, error) {
	var errs domain.ValidationErrors
	add := func(field, msg string) { errs = append(errs, domain.ErrValidation{Field: field, Msg: msg}) }

	d := details{
		firstName:  strings.TrimSpace(in.FirstName),
		lastName:   strings.TrimSpace(in.LastName),
		smsOptIn:   in.SMSOptIn,
		emailOptIn: in.EmailOptIn,
	}
	if !validName(d.firstName) {
		add("first_name", "Le prénom est requis (80 caractères maximum).")
	}
	if !validName(d.lastName) {
		add("last_name", "Le nom est requis (80 caractères maximum).")
	}
	if p := strings.TrimSpace(in.Phone); p != "" {
		n, err := domain.NormalizePhone(p)
		if err != nil {
			add("phone", "Le numéro de téléphone n'est pas valide (exemple : 06 12 34 56 78).")
		}
		d.phone = n
	}
	if e := strings.TrimSpace(in.Email); e != "" {
		a, err := mail.ParseAddress(e)
		if err != nil || a.Address != e || len(e) > 254 || a.Name != "" {
			add("email", "L'adresse email n'est pas valide.")
		} else {
			d.email = strings.ToLower(e)
		}
	}
	if d.phone == "" && d.email == "" && !hasField(errs, "phone") && !hasField(errs, "email") {
		add("phone", "Indiquez au moins un numéro de téléphone ou une adresse email.")
	}
	switch channel {
	case channelSMS:
		if d.phone == "" && !hasField(errs, "phone") {
			add("phone", "Un numéro de téléphone est nécessaire pour recevoir le code par SMS.")
		}
	case channelEmail:
		if d.email == "" && !hasField(errs, "email") {
			add("email", "Une adresse email est nécessaire pour recevoir le code par email.")
		}
	default:
		add("channel", "Choisissez comment recevoir le code de vérification.")
	}
	if b := strings.TrimSpace(in.BirthDate); b != "" {
		t, err := time.Parse("2006-01-02", b)
		if err != nil || t.After(now) || t.Year() < 1900 {
			add("birth_date", "La date de naissance n'est pas valide (format AAAA-MM-JJ).")
		} else {
			d.birth = pgtype.Date{Time: t, Valid: true}
		}
	}
	if len(errs) > 0 {
		return details{}, errs
	}
	return d, nil
}

func hasField(errs domain.ValidationErrors, field string) bool {
	for _, e := range errs {
		if e.Field == field {
			return true
		}
	}
	return false
}

// FindOrCreatePatient retrouve le patient du cabinet ou le crée. Un patient
// existant est retrouvé par (téléphone E.164, prénom, nom) ou (email, prénom,
// nom), sans tenir compte de la casse : le nom est exigé en plus du contact car
// une famille partage souvent un numéro, et un contact non vérifié ne doit pas
// rattacher un rendez-vous à la fiche d'un tiers. La fiche existante n'est jamais
// modifiée par une réservation en ligne.
func FindOrCreatePatient(ctx context.Context, q *sqlcgen.Queries, practiceID uuid.UUID, in PatientDetails, now time.Time) (sqlcgen.Patient, error) {
	d, err := parseDetails(in, channelFor(in), now)
	if err != nil {
		return sqlcgen.Patient{}, err
	}
	return findOrCreatePatient(ctx, q, practiceID, d)
}

// channelFor choisit un canal valide pour la seule validation des coordonnées.
func channelFor(in PatientDetails) string {
	if strings.TrimSpace(in.Phone) != "" {
		return channelSMS
	}
	return channelEmail
}

func findOrCreatePatient(ctx context.Context, q *sqlcgen.Queries, practiceID uuid.UUID, d details) (sqlcgen.Patient, error) {
	if d.phone != "" {
		p, err := q.FindPatientByPhone(ctx, sqlcgen.FindPatientByPhoneParams{
			PracticeID: practiceID, Phone: pgtype.Text{String: d.phone, Valid: true}, FirstName: d.firstName, LastName: d.lastName,
		})
		if err == nil {
			return p, nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return sqlcgen.Patient{}, fmt.Errorf("recherche patient (téléphone): %w", err)
		}
	}
	if d.email != "" {
		p, err := q.FindPatientByEmail(ctx, sqlcgen.FindPatientByEmailParams{
			PracticeID: practiceID, Email: pgtype.Text{String: d.email, Valid: true}, FirstName: d.firstName, LastName: d.lastName,
		})
		if err == nil {
			return p, nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return sqlcgen.Patient{}, fmt.Errorf("recherche patient (email): %w", err)
		}
	}
	p, err := q.CreatePatient(ctx, sqlcgen.CreatePatientParams{
		PracticeID: practiceID, FirstName: d.firstName, LastName: d.lastName, BirthDate: d.birth,
		Phone:    pgtype.Text{String: d.phone, Valid: d.phone != ""},
		Email:    pgtype.Text{String: d.email, Valid: d.email != ""},
		SmsOptIn: d.smsOptIn && d.phone != "", EmailOptIn: d.emailOptIn && d.email != "",
	})
	if err != nil {
		return sqlcgen.Patient{}, fmt.Errorf("création patient: %w", err)
	}
	return p, nil
}
