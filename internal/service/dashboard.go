package service

import (
	"context"
	"fmt"

	"doctolibre/internal/store/sqlcgen"
)

// Dashboard est la donnée du tableau de bord : liens de réservation et état de la configuration.
type Dashboard struct {
	PracticeSlug  string
	Practitioners []sqlcgen.Practitioner
	// HasAvailability : au moins un praticien actif a une plage horaire.
	HasAvailability bool
	// HasBookableType : au moins un motif actif est réservable en ligne.
	HasBookableType bool
}

// Dashboard retourne ce qu'il faut pour afficher la page de réservation du cabinet de l'acteur.
func (s *Practice) Dashboard(ctx context.Context, a Actor) (Dashboard, error) {
	q := sqlcgen.New(s.Pool)
	pc, err := q.GetPractice(ctx, a.PracticeID)
	if err != nil {
		return Dashboard{}, fmt.Errorf("cabinet: %w", err)
	}
	prs, err := q.ListPractitioners(ctx, sqlcgen.ListPractitionersParams{PracticeID: pc.ID, PageSize: pageSize})
	if err != nil {
		return Dashboard{}, fmt.Errorf("liste des praticiens: %w", err)
	}
	rules, err := q.CountPracticeAvailabilityRules(ctx, pc.ID)
	if err != nil {
		return Dashboard{}, fmt.Errorf("plages horaires: %w", err)
	}
	types, err := q.ListBookableAppointmentTypes(ctx, sqlcgen.ListBookableAppointmentTypesParams{PracticeID: pc.ID, PageSize: 1})
	if err != nil {
		return Dashboard{}, fmt.Errorf("motifs réservables: %w", err)
	}
	return Dashboard{PracticeSlug: pc.Slug, Practitioners: prs, HasAvailability: rules > 0, HasBookableType: len(types) > 0}, nil
}
