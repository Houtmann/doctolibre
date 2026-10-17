package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"doctolibre/internal/domain"
	"doctolibre/internal/store"
	"doctolibre/internal/store/sqlcgen"
)

const defaultTypeColor = domain.DefaultColor

// AppointmentTypeInput est la saisie du formulaire d'un motif (durées en minutes, en texte).
type AppointmentTypeInput struct {
	Name                            string
	DurationMin                     string
	BufferBeforeMin, BufferAfterMin string
	Color                           string
	OnlineBookable, AtHome          bool
	TravelBufferMin                 string // trajet (visite à domicile), en minutes, ajouté avant et après
	ResourceID                      string // ressource exigée (T060) ; vide = aucune
}

type defaultType struct {
	name    string
	minutes int
	online  bool
	atHome  bool
	color   string
}

var defaultTypes = []defaultType{
	{"Bilan initial", 45, true, false, defaultTypeColor},
	{"Séance", 30, true, false, "#0d9488"},
	{"Séance à domicile", 30, false, true, "#b45309"},
}

// SeedAppointmentTypes crée les motifs par défaut d'un cabinet qui n'en a aucun
// (idempotent : ne fait rien dès qu'un motif existe, même désactivé).
func SeedAppointmentTypes(ctx context.Context, q *sqlcgen.Queries, practiceID uuid.UUID) error {
	n, err := q.CountAppointmentTypes(ctx, practiceID)
	if err != nil {
		return fmt.Errorf("comptage motifs: %w", err)
	}
	if n > 0 {
		return nil
	}
	for i, d := range defaultTypes {
		if _, err := q.CreateAppointmentType(ctx, sqlcgen.CreateAppointmentTypeParams{
			PracticeID: practiceID, Name: d.name, Duration: minutesInterval(d.minutes),
			BufferBefore: minutesInterval(0), BufferAfter: minutesInterval(0),
			OnlineBookable: d.online, AtHome: d.atHome, Color: d.color, Position: int32(i), //nolint:gosec // 3 motifs
		}); err != nil {
			return fmt.Errorf("motif par défaut %q: %w", d.name, err)
		}
	}
	return nil
}

// AppointmentTypes liste tous les motifs du cabinet (actifs ou non), par position.
// Un cabinet créé avant le préremplissage (aucun motif) reçoit les motifs par défaut.
func (s *Practice) AppointmentTypes(ctx context.Context, a Actor) ([]sqlcgen.AppointmentType, error) {
	if err := store.WithTx(ctx, s.Pool, func(q *sqlcgen.Queries, _ pgx.Tx) error {
		return SeedAppointmentTypes(ctx, q, a.PracticeID)
	}); err != nil {
		return nil, err
	}
	ts, err := sqlcgen.New(s.Pool).ListAllAppointmentTypes(ctx, sqlcgen.ListAllAppointmentTypesParams{PracticeID: a.PracticeID, PageSize: pageSize})
	if err != nil {
		return nil, fmt.Errorf("liste des motifs: %w", err)
	}
	return ts, nil
}

// AppointmentType retourne un motif du cabinet.
func (s *Practice) AppointmentType(ctx context.Context, a Actor, id uuid.UUID) (sqlcgen.AppointmentType, error) {
	t, err := sqlcgen.New(s.Pool).GetAppointmentType(ctx, sqlcgen.GetAppointmentTypeParams{ID: id, PracticeID: a.PracticeID})
	if errors.Is(err, pgx.ErrNoRows) {
		return t, domain.ErrNotFound
	}
	if err != nil {
		return t, fmt.Errorf("lecture motif: %w", err)
	}
	return t, nil
}

type typeValues struct {
	name                  string
	duration, before, aft int
	travel                int
	color                 string
}

func validateType(in AppointmentTypeInput) (typeValues, error) {
	var errs domain.ValidationErrors
	add := func(field, msg string) { errs = append(errs, domain.ErrValidation{Field: field, Msg: msg}) }
	v := typeValues{name: strings.TrimSpace(in.Name), color: strings.TrimSpace(in.Color)}
	if v.name == "" {
		add("name", "Le nom du motif est requis.")
	} else if tooLong(v.name, 60) {
		add("name", "Le nom du motif est trop long (60 caractères maximum).")
	}
	var ok bool
	if v.duration, ok = parseIntRange(in.DurationMin, 5, 480); !ok {
		add("duration", "La durée doit être un nombre entier de minutes entre 5 et 480.")
	}
	if v.before, ok = parseIntRange(emptyAsZero(in.BufferBeforeMin), 0, 120); !ok {
		add("buffer_before", "La marge avant doit être un nombre entier de minutes entre 0 et 120.")
	}
	if v.aft, ok = parseIntRange(emptyAsZero(in.BufferAfterMin), 0, 120); !ok {
		add("buffer_after", "La marge après doit être un nombre entier de minutes entre 0 et 120.")
	}
	if v.travel, ok = parseIntRange(emptyAsDefault(in.TravelBufferMin, defaultTravelMin), 0, 120); !ok {
		add("travel_buffer", "Le temps de trajet doit être un nombre entier de minutes entre 0 et 120.")
	}
	if v.color == "" {
		v.color = defaultTypeColor
	} else if !domain.ValidColor(v.color) {
		add("color", "La couleur doit être au format #rrggbb.")
	}
	if len(errs) > 0 {
		return v, errs
	}
	return v, nil
}

// defaultTravelMin est le trajet par défaut (aussi la valeur par défaut de la colonne).
const defaultTravelMin = "15"

func emptyAsDefault(s, def string) string {
	if strings.TrimSpace(s) == "" {
		return def
	}
	return s
}

func emptyAsZero(s string) string {
	if strings.TrimSpace(s) == "" {
		return "0"
	}
	return s
}

// CreateAppointmentType ajoute un motif en dernière position.
func (s *Practice) CreateAppointmentType(ctx context.Context, a Actor, in AppointmentTypeInput) (sqlcgen.AppointmentType, error) {
	v, err := validateType(in)
	if err != nil {
		return sqlcgen.AppointmentType{}, err
	}
	var out sqlcgen.AppointmentType
	err = store.WithTx(ctx, s.Pool, func(q *sqlcgen.Queries, _ pgx.Tx) error {
		all, err := q.ListAllAppointmentTypes(ctx, sqlcgen.ListAllAppointmentTypesParams{PracticeID: a.PracticeID, PageSize: pageSize})
		if err != nil {
			return fmt.Errorf("liste des motifs: %w", err)
		}
		if len(all) >= pageSize {
			return domain.ErrValidation{Field: "name", Msg: "Nombre maximal de motifs atteint."}
		}
		pos := int32(0)
		for _, t := range all {
			pos = max(pos, t.Position+1)
		}
		out, err = q.CreateAppointmentType(ctx, sqlcgen.CreateAppointmentTypeParams{
			PracticeID: a.PracticeID, Name: v.name, Duration: minutesInterval(v.duration),
			BufferBefore: minutesInterval(v.before), BufferAfter: minutesInterval(v.aft),
			OnlineBookable: in.OnlineBookable, AtHome: in.AtHome, Color: v.color, Position: pos,
		})
		if err != nil {
			return fmt.Errorf("création motif: %w", err)
		}
		if err := q.SetAppointmentTypeTravelBuffer(ctx, sqlcgen.SetAppointmentTypeTravelBufferParams{
			ID: out.ID, PracticeID: a.PracticeID, TravelBuffer: minutesInterval(v.travel),
		}); err != nil {
			return fmt.Errorf("trajet du motif: %w", err)
		}
		out.TravelBuffer = minutesInterval(v.travel)
		if in.ResourceID != "" {
			return setTypeResource(ctx, q, a.PracticeID, out.ID, in.ResourceID)
		}
		return nil
	})
	return out, err
}

// UpdateAppointmentType modifie un motif (hors position et activation).
func (s *Practice) UpdateAppointmentType(ctx context.Context, a Actor, id uuid.UUID, in AppointmentTypeInput) error {
	v, err := validateType(in)
	if err != nil {
		return err
	}
	cur, err := s.AppointmentType(ctx, a, id)
	if err != nil {
		return err
	}
	return store.WithTx(ctx, s.Pool, func(q *sqlcgen.Queries, _ pgx.Tx) error {
		_, err := q.UpdateAppointmentType(ctx, sqlcgen.UpdateAppointmentTypeParams{
			ID: id, PracticeID: a.PracticeID, Name: v.name, Duration: minutesInterval(v.duration),
			BufferBefore: minutesInterval(v.before), BufferAfter: minutesInterval(v.aft),
			OnlineBookable: in.OnlineBookable, AtHome: in.AtHome, Color: v.color,
			Position: cur.Position, Active: cur.Active,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("mise à jour motif: %w", err)
		}
		if err := q.SetAppointmentTypeTravelBuffer(ctx, sqlcgen.SetAppointmentTypeTravelBufferParams{
			ID: id, PracticeID: a.PracticeID, TravelBuffer: minutesInterval(v.travel),
		}); err != nil {
			return fmt.Errorf("trajet du motif: %w", err)
		}
		return setTypeResource(ctx, q, a.PracticeID, id, in.ResourceID)
	})
}

// SetAppointmentTypeActive active ou désactive un motif (jamais supprimé : les
// rendez-vous passés y font référence).
func (s *Practice) SetAppointmentTypeActive(ctx context.Context, a Actor, id uuid.UUID, active bool) error {
	cur, err := s.AppointmentType(ctx, a, id)
	if err != nil {
		return err
	}
	_, err = sqlcgen.New(s.Pool).UpdateAppointmentType(ctx, sqlcgen.UpdateAppointmentTypeParams{
		ID: id, PracticeID: a.PracticeID, Name: cur.Name, Duration: cur.Duration,
		BufferBefore: cur.BufferBefore, BufferAfter: cur.BufferAfter,
		OnlineBookable: cur.OnlineBookable, AtHome: cur.AtHome, Color: cur.Color,
		Position: cur.Position, Active: active,
	})
	if err != nil {
		return fmt.Errorf("activation motif: %w", err)
	}
	return nil
}

// MoveAppointmentType échange un motif avec son voisin (delta -1 = monter, +1 = descendre)
// et renumérote les positions de façon contiguë.
func (s *Practice) MoveAppointmentType(ctx context.Context, a Actor, id uuid.UUID, delta int) error {
	return store.WithTx(ctx, s.Pool, func(q *sqlcgen.Queries, _ pgx.Tx) error {
		all, err := q.ListAllAppointmentTypes(ctx, sqlcgen.ListAllAppointmentTypesParams{PracticeID: a.PracticeID, PageSize: pageSize})
		if err != nil {
			return fmt.Errorf("liste des motifs: %w", err)
		}
		idx := -1
		for i, t := range all {
			if t.ID == id {
				idx = i
			}
		}
		if idx < 0 {
			return domain.ErrNotFound
		}
		if j := idx + delta; j >= 0 && j < len(all) {
			all[idx], all[j] = all[j], all[idx]
		}
		for i, t := range all {
			if t.Position == int32(i) { //nolint:gosec // < 200
				continue
			}
			if err := q.SetAppointmentTypePosition(ctx, sqlcgen.SetAppointmentTypePositionParams{
				ID: t.ID, PracticeID: a.PracticeID, Position: int32(i), //nolint:gosec // < 200
			}); err != nil {
				return fmt.Errorf("position motif: %w", err)
			}
		}
		return nil
	})
}

// TravelMinutes expose le temps de trajet d'un motif en minutes.
func TravelMinutes(t sqlcgen.AppointmentType) int { return intervalMinutes(t.TravelBuffer) }

// TypeMinutes expose les durées d'un motif en minutes (pour l'affichage et les formulaires).
func TypeMinutes(t sqlcgen.AppointmentType) (duration, before, after int) {
	return intervalMinutes(t.Duration), intervalMinutes(t.BufferBefore), intervalMinutes(t.BufferAfter)
}
