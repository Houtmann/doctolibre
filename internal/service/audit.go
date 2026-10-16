package service

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"doctolibre/internal/domain"
	"doctolibre/internal/store/sqlcgen"
)

// AuditPageSize : lignes par page de /admin/audit.
const AuditPageSize = 50

// Audit lit le journal d'audit du cabinet (écran /admin/audit).
type Audit struct {
	Pool  *pgxpool.Pool
	Clock domain.Clock
}

// AuditEntry est une ligne du journal : identifiants seulement, l'email affiché
// est celui du membre du personnel qui a agi.
type AuditEntry struct {
	ID       int64
	At       time.Time
	User     string
	Actor    string
	Action   string
	Entity   string
	EntityID uuid.UUID // uuid.Nil si absent
	Meta     string
}

// AuditPage est une page du journal, du plus récent au plus ancien. Next est le
// curseur de la page suivante (0 sur la dernière).
type AuditPage struct {
	Entries []AuditEntry
	Next    int64
}

// List retourne une page du journal, filtrée par action si non vide. before est le curseur Next.
func (s *Audit) List(ctx context.Context, a Actor, action string, before int64) (AuditPage, error) {
	rows, err := sqlcgen.New(s.Pool).ListAuditLog(ctx, sqlcgen.ListAuditLogParams{
		PracticeID: pgtype.UUID{Bytes: a.PracticeID, Valid: true}, BeforeID: before, Action: action, PageSize: AuditPageSize + 1,
	})
	if err != nil {
		return AuditPage{}, fmt.Errorf("lecture du journal: %w", err)
	}
	var out AuditPage
	if len(rows) > AuditPageSize {
		rows = rows[:AuditPageSize]
		out.Next = rows[len(rows)-1].ID
	}
	for _, r := range rows {
		e := AuditEntry{ID: r.ID, At: r.At, User: r.UserEmail.String, Actor: r.Actor, Action: r.Action, Entity: r.Entity, Meta: string(r.Meta)}
		if r.EntityID.Valid {
			e.EntityID = uuid.UUID(r.EntityID.Bytes)
		}
		out.Entries = append(out.Entries, e)
	}
	return out, nil
}
