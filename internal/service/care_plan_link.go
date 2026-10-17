package service

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"doctolibre/internal/domain"
	"doctolibre/internal/store/sqlcgen"
)

// carePlanTokenLabel sépare la clé des jetons de série (/s/{jeton}) de celle des
// jetons de séance (/r/{jeton}), dérivée de la même ManageKey.
const carePlanTokenLabel = "care-plan-token/v1"

func (k ManageKey) planKey() []byte {
	m := hmac.New(sha256.New, k)
	m.Write([]byte(carePlanTokenLabel)) // hash.Hash.Write ne renvoie jamais d'erreur
	return m.Sum(nil)
}

// PlanToken retourne le jeton dérivé du lien de série (même schéma que T050).
func (k ManageKey) PlanToken(id uuid.UUID, version int) string {
	return DeriveManageToken(k.planKey(), id, version)
}

// ensurePlanToken retourne le jeton du lien de série, en renseignant le haché
// s'il manque (séries antérieures à T055, lien révoqué puis régénéré). Dans la
// transaction de q.
func (k ManageKey) ensurePlanToken(ctx context.Context, q *sqlcgen.Queries, cp sqlcgen.CarePlan) (string, error) {
	if len(k) == 0 {
		return "", ErrNoManageKey
	}
	t := k.PlanToken(cp.ID, int(cp.TokenVersion))
	h := ManageTokenHash(t)
	if cp.TokenHash.Valid && subtle.ConstantTimeCompare([]byte(h), []byte(cp.TokenHash.String)) == 1 {
		return t, nil
	}
	if err := q.SetCarePlanToken(ctx, sqlcgen.SetCarePlanTokenParams{
		Version: cp.TokenVersion, TokenHash: pgtype.Text{String: h, Valid: true}, ID: cp.ID, PracticeID: cp.PracticeID,
	}); err != nil {
		return "", fmt.Errorf("jeton de série: %w", err)
	}
	return t, nil
}

// planURL est l'adresse absolue du lien de série ("" sans BaseURL ou sans jeton).
func planURL(baseURL, token string) string {
	if baseURL == "" || token == "" {
		return ""
	}
	return strings.TrimRight(baseURL, "/") + "/s/" + url.PathEscape(token)
}

// PlanLinkSession est une séance affichée sur la page publique de la série.
type PlanLinkSession struct {
	ID          uuid.UUID
	Start, End  time.Time
	Status      string
	StatusLabel string
	Address     string
	// ManageToken : jeton /r/{jeton}, rempli seulement pour une séance à venir non annulée.
	ManageToken string
}

// PlanLink est la page publique d'une série retrouvée par son lien /s/{jeton}.
// Aucun libellé libre de série ni donnée médicale : le motif du rendez-vous seul.
type PlanLink struct {
	Practice     sqlcgen.Practice
	Practitioner sqlcgen.Practitioner
	Type         sqlcgen.AppointmentType
	FirstName    string
	Prescribed   int
	Loc          *time.Location
	Upcoming     []PlanLinkSession // séances réservées à venir, chronologique
	Past         []PlanLinkSession // le reste (honorées, absences, annulées, passées), chronologique
}

// CarePlanByToken retrouve une série par son lien. Jeton inconnu, falsifié ou
// révoqué : domain.ErrNotFound (aucune information sur l'existence).
func (b *Booking) CarePlanByToken(ctx context.Context, token string) (PlanLink, error) {
	if token == "" || len(token) > 128 {
		return PlanLink{}, domain.ErrNotFound
	}
	q := sqlcgen.New(b.Pool)
	cp, err := q.GetCarePlanByTokenHash(ctx, pgtype.Text{String: ManageTokenHash(token), Valid: true})
	if errors.Is(err, pgx.ErrNoRows) {
		return PlanLink{}, domain.ErrNotFound
	}
	if err != nil {
		return PlanLink{}, fmt.Errorf("lecture série: %w", err)
	}
	pc, err := q.GetPractice(ctx, cp.PracticeID)
	if err != nil {
		return PlanLink{}, fmt.Errorf("lecture cabinet: %w", err)
	}
	pr, err := q.GetPractitioner(ctx, sqlcgen.GetPractitionerParams{ID: cp.PractitionerID, PracticeID: cp.PracticeID})
	if err != nil {
		return PlanLink{}, fmt.Errorf("lecture praticien: %w", err)
	}
	typ, err := q.GetAppointmentType(ctx, sqlcgen.GetAppointmentTypeParams{ID: cp.AppointmentTypeID, PracticeID: cp.PracticeID})
	if err != nil {
		return PlanLink{}, fmt.Errorf("lecture motif: %w", err)
	}
	pat, err := q.GetPatient(ctx, sqlcgen.GetPatientParams{ID: cp.PatientID, PracticeID: cp.PracticeID})
	if err != nil {
		return PlanLink{}, fmt.Errorf("lecture patient: %w", err)
	}
	loc, err := time.LoadLocation(pc.Timezone)
	if err != nil {
		return PlanLink{}, fmt.Errorf("fuseau %q: %w", pc.Timezone, err)
	}
	rows, err := q.ListCarePlanSessions(ctx, sqlcgen.ListCarePlanSessionsParams{CarePlanID: pgtype.UUID{Bytes: cp.ID, Valid: true}, PracticeID: cp.PracticeID})
	if err != nil {
		return PlanLink{}, fmt.Errorf("séances de la série: %w", err)
	}
	out := PlanLink{Practice: pc, Practitioner: pr, Type: typ, FirstName: pat.FirstName, Prescribed: int(cp.SessionsPrescribed), Loc: loc}
	now := b.Clock.Now()
	for _, a := range rows {
		s := PlanLinkSession{
			ID: a.ID, Start: a.During.Lower.Time.In(loc), End: a.During.Upper.Time.In(loc),
			Status: a.Status, StatusLabel: statusLabels[a.Status], Address: a.Address.String,
		}
		if a.Status == "booked" && !a.During.Lower.Time.Before(now) {
			// Jeton dérivé de la séance (T050) ; "" pour un ancien jeton aléatoire ou révoqué : pas de lien.
			s.ManageToken = b.ManageKey.forAppointment(a)
			out.Upcoming = append(out.Upcoming, s)
			continue
		}
		out.Past = append(out.Past, s)
	}
	return out, nil
}
