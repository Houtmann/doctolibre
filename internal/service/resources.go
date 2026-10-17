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

// Ressources partagées (T060) : box, appareils, de capacité 1. Un motif exige au plus
// une ressource ; la réservation (insertion, déplacement, annulation du RDV) est
// synchronisée par le trigger appointment_sync_resource, donc identique pour tous les
// chemins d'écriture. Une ressource désactivée n'est plus exigée pour les nouveaux RDV
// (le motif se comporte comme sans ressource) ; les RDV existants gardent la leur.
// Mêmes règles d'accès que les motifs (aucune règle supplémentaire).

func validateResourceName(name string) (string, error) {
	name = strings.TrimSpace(name)
	switch {
	case name == "":
		return "", domain.ErrValidation{Field: "resource_name", Msg: "Le nom de la ressource est requis."}
	case tooLong(name, 60):
		return "", domain.ErrValidation{Field: "resource_name", Msg: "Le nom de la ressource est trop long (60 caractères maximum)."}
	}
	return name, nil
}

// Resources liste les ressources du cabinet (actives ou non).
func (s *Practice) Resources(ctx context.Context, a Actor) ([]sqlcgen.Resource, error) {
	rs, err := sqlcgen.New(s.Pool).ListResources(ctx, sqlcgen.ListResourcesParams{PracticeID: a.PracticeID, PageSize: pageSize})
	if err != nil {
		return nil, fmt.Errorf("liste des ressources: %w", err)
	}
	return rs, nil
}

// TypeResources donne, pour chaque motif du cabinet, l'identifiant de sa ressource.
func (s *Practice) TypeResources(ctx context.Context, a Actor) (map[uuid.UUID]uuid.UUID, error) {
	rows, err := sqlcgen.New(s.Pool).ListTypeResources(ctx, sqlcgen.ListTypeResourcesParams{PracticeID: a.PracticeID, PageSize: pageSize})
	if err != nil {
		return nil, fmt.Errorf("liaisons motif-ressource: %w", err)
	}
	out := make(map[uuid.UUID]uuid.UUID, len(rows))
	for _, r := range rows {
		out[r.AppointmentTypeID] = r.ResourceID
	}
	return out, nil
}

// CreateResource ajoute une ressource active.
func (s *Practice) CreateResource(ctx context.Context, a Actor, name string) (sqlcgen.Resource, error) {
	name, err := validateResourceName(name)
	if err != nil {
		return sqlcgen.Resource{}, err
	}
	var out sqlcgen.Resource
	err = store.WithTx(ctx, s.Pool, func(q *sqlcgen.Queries, _ pgx.Tx) error {
		all, err := q.ListResources(ctx, sqlcgen.ListResourcesParams{PracticeID: a.PracticeID, PageSize: pageSize})
		if err != nil {
			return fmt.Errorf("liste des ressources: %w", err)
		}
		if len(all) >= pageSize {
			return domain.ErrValidation{Field: "resource_name", Msg: "Nombre maximal de ressources atteint."}
		}
		out, err = q.CreateResource(ctx, sqlcgen.CreateResourceParams{PracticeID: a.PracticeID, Name: name})
		if err != nil {
			return fmt.Errorf("création ressource: %w", err)
		}
		return nil
	})
	return out, err
}

// UpdateResource renomme une ressource et l'active ou la désactive.
func (s *Practice) UpdateResource(ctx context.Context, a Actor, id uuid.UUID, name string, active bool) error {
	name, err := validateResourceName(name)
	if err != nil {
		return err
	}
	_, err = sqlcgen.New(s.Pool).UpdateResource(ctx, sqlcgen.UpdateResourceParams{ID: id, PracticeID: a.PracticeID, Name: name, Active: active})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("mise à jour ressource: %w", err)
	}
	return nil
}

// SetResourceActive active ou désactive une ressource sans toucher aux RDV existants.
func (s *Practice) SetResourceActive(ctx context.Context, a Actor, id uuid.UUID, active bool) error {
	q := sqlcgen.New(s.Pool)
	cur, err := q.GetResource(ctx, sqlcgen.GetResourceParams{ID: id, PracticeID: a.PracticeID})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("lecture ressource: %w", err)
	}
	if _, err := q.UpdateResource(ctx, sqlcgen.UpdateResourceParams{ID: id, PracticeID: a.PracticeID, Name: cur.Name, Active: active}); err != nil {
		return fmt.Errorf("activation ressource: %w", err)
	}
	return nil
}

// setTypeResource lie (ou délie, resourceID vide) la ressource d'un motif du cabinet.
// La ressource doit appartenir au même cabinet que le motif.
func setTypeResource(ctx context.Context, q *sqlcgen.Queries, practiceID, typeID uuid.UUID, resourceID string) error {
	resourceID = strings.TrimSpace(resourceID)
	if resourceID == "" {
		if err := q.ClearTypeResource(ctx, typeID); err != nil {
			return fmt.Errorf("retrait ressource du motif: %w", err)
		}
		return nil
	}
	rid, err := uuid.Parse(resourceID)
	if err != nil {
		return domain.ErrValidation{Field: "resource", Msg: "Ressource inconnue."}
	}
	if _, err := q.GetResource(ctx, sqlcgen.GetResourceParams{ID: rid, PracticeID: practiceID}); errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrValidation{Field: "resource", Msg: "Ressource inconnue."}
	} else if err != nil {
		return fmt.Errorf("lecture ressource: %w", err)
	}
	if err := q.SetTypeResource(ctx, sqlcgen.SetTypeResourceParams{AppointmentTypeID: typeID, ResourceID: rid}); err != nil {
		return fmt.Errorf("ressource du motif: %w", err)
	}
	return nil
}
