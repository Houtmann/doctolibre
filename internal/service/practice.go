// Package service contient les règles métier : validation, transactions,
// orchestration du store. Aucune dépendance vers web ni auth.
package service

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
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

// pageSize borne les listes d'administration (quelques dizaines d'éléments au plus).
const pageSize = 200

// Practice porte la configuration du cabinet : paramètres, profil du praticien,
// motifs, disponibilités et absences.
type Practice struct {
	Pool  *pgxpool.Pool
	Clock domain.Clock
}

// Actor identifie l'utilisateur connecté qui agit ; toutes les opérations sont
// bornées à son cabinet.
type Actor struct {
	UserID     uuid.UUID
	PracticeID uuid.UUID
}

// Settings est la vue des paramètres du cabinet et du profil du praticien.
type Settings struct {
	Practice          sqlcgen.Practice
	Practitioner      sqlcgen.Practitioner
	BookingNoticeHour int
	CancelNoticeHour  int
	HorizonDays       int
}

// SettingsInput est la saisie du formulaire (les nombres arrivent en texte pour
// pouvoir réafficher une saisie invalide).
type SettingsInput struct {
	Name, Address, Phone string
	BookingNoticeHours   string
	CancelNoticeHours    string
	HorizonDays          string

	FirstName, LastName, Title, Bio, Slug string
}

var slugRe = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// Practitioner retourne le praticien lié à l'utilisateur, en le créant au premier
// accès (create-user ne crée qu'un compte). Un secrétaire n'a pas de praticien.
func (s *Practice) Practitioner(ctx context.Context, a Actor) (sqlcgen.Practitioner, error) {
	var out sqlcgen.Practitioner
	err := store.WithTx(ctx, s.Pool, func(q *sqlcgen.Queries, _ pgx.Tx) error {
		var err error
		out, err = ensurePractitioner(ctx, q, a)
		return err
	})
	return out, err
}

func ensurePractitioner(ctx context.Context, q *sqlcgen.Queries, a Actor) (sqlcgen.Practitioner, error) {
	u, err := q.GetUserForUpdate(ctx, sqlcgen.GetUserForUpdateParams{ID: a.UserID, PracticeID: a.PracticeID})
	if errors.Is(err, pgx.ErrNoRows) {
		return sqlcgen.Practitioner{}, domain.ErrNotFound
	}
	if err != nil {
		return sqlcgen.Practitioner{}, fmt.Errorf("lecture utilisateur: %w", err)
	}
	if u.Role == domain.RoleSecretary {
		return sqlcgen.Practitioner{}, domain.ErrForbidden
	}
	if u.PractitionerID.Valid {
		p, err := q.GetPractitioner(ctx, sqlcgen.GetPractitionerParams{ID: u.PractitionerID.Bytes, PracticeID: a.PracticeID})
		if errors.Is(err, pgx.ErrNoRows) {
			return sqlcgen.Practitioner{}, domain.ErrNotFound
		}
		if err != nil {
			return sqlcgen.Practitioner{}, fmt.Errorf("lecture praticien: %w", err)
		}
		return p, nil
	}
	local, _, _ := strings.Cut(u.Email, "@")
	base := domain.Slugify(local)
	slug := base
	for i := 2; ; i++ {
		_, err := q.GetPractitionerBySlug(ctx, sqlcgen.GetPractitionerBySlugParams{PracticeID: a.PracticeID, Slug: slug})
		if errors.Is(err, pgx.ErrNoRows) {
			break
		}
		if err != nil {
			return sqlcgen.Practitioner{}, fmt.Errorf("lecture slug: %w", err)
		}
		slug = base + "-" + strconv.Itoa(i)
	}
	p, err := q.CreatePractitioner(ctx, sqlcgen.CreatePractitionerParams{
		PracticeID: a.PracticeID, LastName: truncate(local, 80), Slug: slug, Color: "#2563eb",
	})
	if err != nil {
		return sqlcgen.Practitioner{}, fmt.Errorf("création praticien: %w", err)
	}
	if err := q.SetUserPractitioner(ctx, sqlcgen.SetUserPractitionerParams{
		PractitionerID: pgtype.UUID{Bytes: p.ID, Valid: true}, ID: a.UserID, PracticeID: a.PracticeID,
	}); err != nil {
		return sqlcgen.Practitioner{}, fmt.Errorf("liaison praticien: %w", err)
	}
	return p, nil
}

// Settings retourne les paramètres du cabinet et le profil du praticien connecté.
func (s *Practice) Settings(ctx context.Context, a Actor) (Settings, error) {
	pr, err := s.Practitioner(ctx, a)
	if err != nil {
		return Settings{}, err
	}
	q := sqlcgen.New(s.Pool)
	pc, err := q.GetPractice(ctx, a.PracticeID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Settings{}, domain.ErrNotFound
	}
	if err != nil {
		return Settings{}, fmt.Errorf("lecture cabinet: %w", err)
	}
	return Settings{
		Practice: pc, Practitioner: pr,
		BookingNoticeHour: intervalHours(pc.MinBookingNotice),
		CancelNoticeHour:  intervalHours(pc.MinCancelNotice),
		HorizonDays:       intervalDays(pc.BookingHorizon),
	}, nil
}

// UpdateSettings valide puis enregistre les paramètres du cabinet et le profil.
func (s *Practice) UpdateSettings(ctx context.Context, a Actor, in SettingsInput) error {
	var errs domain.ValidationErrors
	add := func(field, msg string) { errs = append(errs, domain.ErrValidation{Field: field, Msg: msg}) }

	in.Name, in.Address, in.Phone = strings.TrimSpace(in.Name), strings.TrimSpace(in.Address), strings.TrimSpace(in.Phone)
	in.FirstName, in.LastName = strings.TrimSpace(in.FirstName), strings.TrimSpace(in.LastName)
	in.Title, in.Bio, in.Slug = strings.TrimSpace(in.Title), strings.TrimSpace(in.Bio), strings.TrimSpace(in.Slug)

	if in.Name == "" {
		add("name", "Le nom du cabinet est requis.")
	} else if tooLong(in.Name, 120) {
		add("name", "Le nom du cabinet est trop long (120 caractères maximum).")
	}
	if tooLong(in.Address, 300) {
		add("address", "L'adresse est trop longue (300 caractères maximum).")
	}
	if tooLong(in.Phone, 30) || strings.Trim(in.Phone, "0123456789 +.-()") != "" {
		add("phone", "Le téléphone ne doit contenir que des chiffres, espaces, +, -, . et parenthèses (30 caractères maximum).")
	}
	booking, ok := parseIntRange(in.BookingNoticeHours, 0, 24*30)
	if !ok {
		add("booking_notice_hours", "Le délai minimal de réservation doit être un nombre d'heures entre 0 et 720.")
	}
	cancel, ok2 := parseIntRange(in.CancelNoticeHours, 0, 24*30)
	if !ok2 {
		add("cancel_notice_hours", "Le délai minimal d'annulation doit être un nombre d'heures entre 0 et 720.")
	}
	horizon, ok3 := parseIntRange(in.HorizonDays, 1, 730)
	if !ok3 {
		add("horizon_days", "L'horizon de réservation doit être un nombre de jours entre 1 et 730.")
	}
	if in.FirstName == "" {
		add("first_name", "Le prénom du praticien est requis.")
	} else if tooLong(in.FirstName, 80) {
		add("first_name", "Le prénom est trop long (80 caractères maximum).")
	}
	if in.LastName == "" {
		add("last_name", "Le nom du praticien est requis.")
	} else if tooLong(in.LastName, 80) {
		add("last_name", "Le nom est trop long (80 caractères maximum).")
	}
	if tooLong(in.Title, 80) {
		add("title", "Le titre est trop long (80 caractères maximum).")
	}
	if tooLong(in.Bio, 2000) {
		add("bio", "La présentation est trop longue (2000 caractères maximum).")
	}
	if len(in.Slug) > 60 || !slugRe.MatchString(in.Slug) {
		add("slug", "L'identifiant d'URL ne peut contenir que des lettres minuscules, chiffres et tirets (60 caractères maximum).")
	}
	if len(errs) > 0 {
		return errs
	}

	err := store.WithTx(ctx, s.Pool, func(q *sqlcgen.Queries, _ pgx.Tx) error {
		pr, err := ensurePractitioner(ctx, q, a)
		if err != nil {
			return err
		}
		pc, err := q.GetPractice(ctx, a.PracticeID)
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("lecture cabinet: %w", err)
		}
		if _, err := q.UpdatePractice(ctx, sqlcgen.UpdatePracticeParams{
			ID: pc.ID, Name: in.Name, Address: in.Address, Phone: in.Phone, Timezone: pc.Timezone,
			MinBookingNotice: hoursInterval(booking), MinCancelNotice: hoursInterval(cancel),
			BookingHorizon: pgtype.Interval{Days: int32(horizon), Valid: true}, //nolint:gosec // borné à 730
		}); err != nil {
			return fmt.Errorf("mise à jour cabinet: %w", err)
		}
		_, err = q.UpdatePractitioner(ctx, sqlcgen.UpdatePractitionerParams{
			ID: pr.ID, PracticeID: a.PracticeID, FirstName: in.FirstName, LastName: in.LastName,
			Slug: in.Slug, Title: in.Title, Bio: in.Bio, Color: pr.Color, Active: pr.Active,
		})
		if store.IsUniqueViolation(err) {
			return domain.ValidationErrors{{Field: "slug", Msg: "Cet identifiant d'URL est déjà utilisé dans le cabinet."}}
		}
		if err != nil {
			return fmt.Errorf("mise à jour praticien: %w", err)
		}
		return nil
	})
	return err
}

// location charge le fuseau du cabinet.
func (s *Practice) location(ctx context.Context, practiceID uuid.UUID) (*time.Location, error) {
	pc, err := sqlcgen.New(s.Pool).GetPractice(ctx, practiceID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("lecture cabinet: %w", err)
	}
	loc, err := time.LoadLocation(pc.Timezone)
	if err != nil {
		return nil, fmt.Errorf("fuseau %q: %w", pc.Timezone, err)
	}
	return loc, nil
}

func tooLong(s string, limit int) bool { return utf8.RuneCountInString(s) > limit }

func truncate(s string, limit int) string {
	if utf8.RuneCountInString(s) <= limit {
		return s
	}
	return string([]rune(s)[:limit])
}

// parseIntRange lit un entier décimal dans [lo, hi].
func parseIntRange(s string, lo, hi int) (int, bool) {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || n < lo || n > hi {
		return 0, false
	}
	return n, true
}

func hoursInterval(h int) pgtype.Interval {
	return pgtype.Interval{Microseconds: int64(h) * time.Hour.Microseconds(), Valid: true}
}

func minutesInterval(m int) pgtype.Interval {
	return pgtype.Interval{Microseconds: int64(m) * time.Minute.Microseconds(), Valid: true}
}

// intervalMicros convertit un interval en microsecondes (mois = 30 jours, jour = 24 h).
func intervalMicros(i pgtype.Interval) int64 {
	const day = 24 * int64(time.Hour/time.Microsecond)
	return int64(i.Months)*30*day + int64(i.Days)*day + i.Microseconds
}

func intervalHours(i pgtype.Interval) int {
	return int(intervalMicros(i) / time.Hour.Microseconds())
}

func intervalDays(i pgtype.Interval) int {
	return int(intervalMicros(i) / (24 * time.Hour.Microseconds()))
}

func intervalMinutes(i pgtype.Interval) int {
	return int(intervalMicros(i) / time.Minute.Microseconds())
}
