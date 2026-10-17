package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"doctolibre/internal/domain"
	"doctolibre/internal/store"
	"doctolibre/internal/store/sqlcgen"
)

// Actions d'audit des délégations d'agenda (T059).
const (
	ActionDelegationCreated = "delegation_created"
	ActionDelegationRevoked = "delegation_revoked"
)

// maxDelegationDays borne la durée d'une délégation (un remplacement ne dure pas des années).
const maxDelegationDays = 366

const dateLayout = "2006-01-02"

// Delegation est une délégation d'agenda en cours ou à venir.
type Delegation struct {
	ID               uuid.UUID
	UserID           uuid.UUID
	Email            string // ne jamais journaliser
	PractitionerID   uuid.UUID
	PractitionerName string
	From, To         time.Time // jours inclusifs (minuit UTC, seule la date compte)
}

// DelegationInput est la saisie du formulaire : dates « AAAA-MM-JJ », bornes incluses.
type DelegationInput struct {
	UserID         uuid.UUID
	PractitionerID uuid.UUID
	From, To       string
}

func auditDelegation(ctx context.Context, q *sqlcgen.Queries, actor User, action string, id uuid.UUID, meta map[string]string) error {
	b, err := json.Marshal(meta)
	if err != nil {
		return fmt.Errorf("meta audit: %w", err)
	}
	return q.CreateAuditLog(ctx, sqlcgen.CreateAuditLogParams{
		UserID: pgtype.UUID{Bytes: actor.ID, Valid: true}, Actor: "user:" + actor.ID.String(), Action: action,
		Entity: "practitioner_delegation", EntityID: pgtype.UUID{Bytes: id, Valid: true}, Meta: b,
	})
}

// localToday retourne le jour courant (date murale) dans le fuseau du cabinet.
func localToday(now time.Time, tz string) (time.Time, error) {
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return time.Time{}, fmt.Errorf("fuseau %q: %w", tz, err)
	}
	y, m, d := now.In(loc).Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC), nil
}

// Delegations liste les délégations en cours ou à venir du cabinet (admin seulement).
func (s *Service) Delegations(ctx context.Context, actor User) ([]Delegation, error) {
	if err := requireTeamAdmin(actor); err != nil {
		return nil, err
	}
	rows, err := sqlcgen.New(s.Pool).ListDelegations(ctx, sqlcgen.ListDelegationsParams{
		PracticeID: actor.PracticeID, Now: s.Clock.Now(), PageSize: teamPageSize,
	})
	if err != nil {
		return nil, fmt.Errorf("liste des délégations: %w", err)
	}
	out := make([]Delegation, 0, len(rows))
	for _, r := range rows {
		out = append(out, Delegation{
			ID: r.ID, UserID: r.UserID, Email: r.Email, PractitionerID: r.PractitionerID,
			PractitionerName: strings.TrimSpace(r.FirstName + " " + r.LastName),
			From:             r.ValidFrom.Time, To: r.ValidTo.Time,
		})
	}
	return out, nil
}

func delegationError(field, msg string) error { return domain.ErrValidation{Field: field, Msg: msg} }

// CreateDelegation donne à un compte praticien existant du cabinet l'accès à
// l'agenda d'un autre praticien, du jour From au jour To inclus (fuseau du
// cabinet). Admin seulement. Les comptes administrateur et secrétariat voient
// déjà tous les agendas : refusé. Une invitation en attente n'a pas de compte :
// l'invité doit d'abord activer le sien.
func (s *Service) CreateDelegation(ctx context.Context, actor User, in DelegationInput) error {
	if err := requireTeamAdmin(actor); err != nil {
		return err
	}
	from, err := time.Parse(dateLayout, strings.TrimSpace(in.From))
	if err != nil {
		return delegationError("from", "Date de début invalide.")
	}
	to, err := time.Parse(dateLayout, strings.TrimSpace(in.To))
	if err != nil {
		return delegationError("to", "Date de fin invalide.")
	}
	if to.Before(from) {
		return delegationError("to", "La date de fin doit suivre la date de début.")
	}
	if to.Sub(from) > maxDelegationDays*24*time.Hour {
		return delegationError("to", "Une délégation dure un an au plus.")
	}
	now := s.Clock.Now()
	return store.WithTx(ctx, s.Pool, func(q *sqlcgen.Queries, _ pgx.Tx) error {
		pc, err := q.GetPractice(ctx, actor.PracticeID)
		if err != nil {
			return fmt.Errorf("lecture cabinet: %w", err)
		}
		today, err := localToday(now, pc.Timezone)
		if err != nil {
			return err
		}
		if to.Before(today) {
			return delegationError("to", "La date de fin est déjà passée.")
		}
		u, err := lockMember(ctx, q, actor, in.UserID)
		if err != nil {
			return err
		}
		if !u.Active {
			return delegationError("user", "Ce compte est désactivé.")
		}
		if u.Role != domain.RolePractitioner {
			return delegationError("user", "Seul un compte praticien a besoin d'une délégation : l'administrateur et le secrétariat voient déjà tous les agendas.")
		}
		if _, err := q.GetPractitioner(ctx, sqlcgen.GetPractitionerParams{ID: in.PractitionerID, PracticeID: actor.PracticeID}); errors.Is(err, pgx.ErrNoRows) {
			return domain.ErrNotFound
		} else if err != nil {
			return fmt.Errorf("lecture praticien: %w", err)
		}
		if u.PractitionerID.Valid && u.PractitionerID.Bytes == in.PractitionerID {
			return delegationError("practitioner", "Ce compte est déjà lié à cet agenda.")
		}
		id, err := q.CreateDelegation(ctx, sqlcgen.CreateDelegationParams{
			PracticeID: actor.PracticeID, UserID: in.UserID, PractitionerID: in.PractitionerID,
			ValidFrom: pgtype.Date{Time: from, Valid: true}, ValidTo: pgtype.Date{Time: to, Valid: true},
			CreatedBy: pgtype.UUID{Bytes: actor.ID, Valid: true}, CreatedAt: now,
		})
		if store.IsExclusionViolation(err) {
			return delegationError("from", "Une délégation existe déjà sur cette période pour ce compte et cet agenda.")
		}
		if err != nil {
			return fmt.Errorf("création de la délégation: %w", err)
		}
		return auditDelegation(ctx, q, actor, ActionDelegationCreated, id, map[string]string{
			"user_id": in.UserID.String(), "practitioner_id": in.PractitionerID.String(),
			"from": from.Format(dateLayout), "to": to.Format(dateLayout),
		})
	})
}

// RevokeDelegation supprime une délégation du cabinet (admin seulement) ; l'accès
// tombe dès la requête suivante du remplaçant, ses droits étant relus à chaque requête.
func (s *Service) RevokeDelegation(ctx context.Context, actor User, id uuid.UUID) error {
	if err := requireTeamAdmin(actor); err != nil {
		return err
	}
	return store.WithTx(ctx, s.Pool, func(q *sqlcgen.Queries, _ pgx.Tx) error {
		r, err := q.DeleteDelegation(ctx, sqlcgen.DeleteDelegationParams{ID: id, PracticeID: actor.PracticeID})
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("révocation de la délégation: %w", err)
		}
		return auditDelegation(ctx, q, actor, ActionDelegationRevoked, id, map[string]string{
			"user_id": r.UserID.String(), "practitioner_id": r.PractitionerID.String(),
		})
	})
}
