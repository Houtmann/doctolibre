package service

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"doctolibre/internal/store/sqlcgen"
)

// EnsurePlanTokenForTest expose la génération paresseuse du lien de série
// (séries antérieures à T055) aux tests du package service_test.
func EnsurePlanTokenForTest(ctx context.Context, db *pgxpool.Pool, k ManageKey, planID uuid.UUID) (string, error) {
	q := sqlcgen.New(db)
	var pid uuid.UUID
	if err := db.QueryRow(ctx, "SELECT practice_id FROM care_plan WHERE id = $1", planID).Scan(&pid); err != nil {
		return "", err
	}
	cp, err := q.GetCarePlan(ctx, sqlcgen.GetCarePlanParams{ID: planID, PracticeID: pid})
	if err != nil {
		return "", err
	}
	return k.ensurePlanToken(ctx, q, cp)
}
