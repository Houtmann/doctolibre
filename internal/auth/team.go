package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/mail"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"doctolibre/internal/domain"
	"doctolibre/internal/outbox"
	"doctolibre/internal/store"
	"doctolibre/internal/store/sqlcgen"
)

// InvitationTTL est la durée de validité d'un lien d'activation.
const InvitationTTL = 48 * time.Hour

// AcceptPath est la page d'activation d'une invitation.
const AcceptPath = "/admin/invitation"

// TemplateInvitation est le template email (internal/notify/templates/email).
const TemplateInvitation = "user_invitation"

// Actions d'audit de la gestion d'équipe.
const (
	ActionUserInvited     = "user_invited"
	ActionInvitationUsed  = "invitation_accepted"
	ActionInvitationRevok = "invitation_revoked"
	ActionUserRoleChanged = "user_role_changed"
	ActionUserDeactivated = "user_deactivated"
	ActionUserReactivated = "user_reactivated"
)

// ErrInvalidInvitation : lien inconnu, expiré ou déjà utilisé.
var ErrInvalidInvitation = errors.New("invitation invalide ou expirée")

const teamPageSize = 200

// RoleLabel est le libellé français d'un rôle.
func RoleLabel(role string) string {
	switch role {
	case domain.RoleAdmin:
		return "Administrateur"
	case domain.RolePractitioner:
		return "Praticien"
	case domain.RoleSecretary:
		return "Secrétariat"
	}
	return role
}

// TeamMember est un compte du cabinet.
type TeamMember struct {
	ID          uuid.UUID
	Email       string
	Role        string
	Active      bool
	Name        string // nom du praticien lié, s'il existe
	LastLoginAt time.Time
}

// PendingInvitation est une invitation envoyée et non encore utilisée.
type PendingInvitation struct {
	ID        string // identifiant opaque (haché du jeton) pour l'annulation
	Email     string
	Role      string
	ExpiresAt time.Time
}

// TeamView regroupe les comptes et les invitations en attente.
type TeamView struct {
	Members     []TeamMember
	Invitations []PendingInvitation
}

func requireTeamAdmin(actor User) error {
	if !Can(actor, ActionTeamManage, Resource{PracticeID: actor.PracticeID}) {
		return domain.ErrForbidden
	}
	return nil
}

func auditTeam(ctx context.Context, q *sqlcgen.Queries, actor User, action string, entity uuid.NullUUID, meta map[string]string) error {
	b, err := json.Marshal(meta)
	if err != nil {
		return fmt.Errorf("meta audit: %w", err)
	}
	return q.CreateAuditLog(ctx, sqlcgen.CreateAuditLogParams{
		UserID: pgtype.UUID{Bytes: actor.ID, Valid: true}, Actor: "user:" + actor.ID.String(), Action: action,
		Entity: "user", EntityID: pgtype.UUID{Bytes: entity.UUID, Valid: entity.Valid}, Meta: b,
	})
}

// Team liste les comptes et invitations en attente du cabinet de actor (admin).
func (s *Service) Team(ctx context.Context, actor User) (TeamView, error) {
	if err := requireTeamAdmin(actor); err != nil {
		return TeamView{}, err
	}
	q := sqlcgen.New(s.Pool)
	rows, err := q.ListTeamUsers(ctx, sqlcgen.ListTeamUsersParams{PracticeID: actor.PracticeID, PageSize: teamPageSize})
	if err != nil {
		return TeamView{}, fmt.Errorf("liste de l'équipe: %w", err)
	}
	inv, err := q.ListPendingInvitations(ctx, sqlcgen.ListPendingInvitationsParams{PracticeID: actor.PracticeID, Now: s.Clock.Now(), PageSize: teamPageSize})
	if err != nil {
		return TeamView{}, fmt.Errorf("liste des invitations: %w", err)
	}
	var v TeamView
	for _, r := range rows {
		m := TeamMember{ID: r.ID, Email: r.Email, Role: r.Role, Active: r.Active, Name: strings.TrimSpace(r.FirstName + " " + r.LastName)}
		if r.LastLoginAt.Valid {
			m.LastLoginAt = r.LastLoginAt.Time
		}
		v.Members = append(v.Members, m)
	}
	for _, i := range inv {
		v.Invitations = append(v.Invitations, PendingInvitation{ID: i.TokenHash, Email: i.Email, Role: i.Role, ExpiresAt: i.ExpiresAt})
	}
	return v, nil
}

// Invite enregistre une invitation (jeton haché sha256, usage unique, 48 h) et
// met l'email d'activation dans l'outbox, dans la même transaction. baseURL est
// l'URL publique. Le jeton n'est jamais journalisé.
func (s *Service) Invite(ctx context.Context, actor User, baseURL, email, role, ip string) error {
	if err := requireTeamAdmin(actor); err != nil {
		return err
	}
	email = strings.ToLower(strings.TrimSpace(email))
	if a, err := mail.ParseAddress(email); err != nil || a.Address != email || len(email) > 254 {
		return domain.ErrValidation{Field: "email", Msg: "Adresse email invalide."}
	}
	if !domain.ValidRole(role) {
		return domain.ErrValidation{Field: "role", Msg: "Rôle inconnu."}
	}
	if baseURL == "" {
		return errors.New("invitation: base URL non configurée")
	}
	q := sqlcgen.New(s.Pool)
	if _, err := q.GetUserByEmail(ctx, email); err == nil {
		return domain.ErrValidation{Field: "email", Msg: "Un compte existe déjà avec cette adresse."}
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("lecture utilisateur: %w", err)
	}
	pc, err := q.GetPractice(ctx, actor.PracticeID)
	if err != nil {
		return fmt.Errorf("lecture cabinet: %w", err)
	}
	token, err := newToken()
	if err != nil {
		return err
	}
	now := s.Clock.Now()
	link := strings.TrimRight(baseURL, "/") + AcceptPath + "?token=" + url.QueryEscape(token)
	payload, err := json.Marshal(map[string]string{
		"accept_url": link, "valid_for": "48 heures", "practice_name": pc.Name, "role_label": RoleLabel(role),
	})
	if err != nil {
		return fmt.Errorf("payload email: %w", err)
	}
	return store.WithTx(ctx, s.Pool, func(q *sqlcgen.Queries, tx pgx.Tx) error {
		// Une seule invitation en cours par adresse : la précédente est invalidée.
		if err := q.DeletePendingInvitationsByEmail(ctx, sqlcgen.DeletePendingInvitationsByEmailParams{PracticeID: actor.PracticeID, Email: email}); err != nil {
			return fmt.Errorf("purge des invitations: %w", err)
		}
		if err := q.CreateInvitation(ctx, sqlcgen.CreateInvitationParams{
			TokenHash: SessionID(token), PracticeID: actor.PracticeID, InvitedBy: pgtype.UUID{Bytes: actor.ID, Valid: true},
			Email: email, Role: role, ExpiresAt: now.Add(InvitationTTL), CreatedAt: now,
		}); err != nil {
			return fmt.Errorf("création de l'invitation: %w", err)
		}
		if err := outbox.Enqueue(ctx, tx, outbox.Msg{
			PracticeID: actor.PracticeID, Kind: outbox.KindEmail, Template: TemplateInvitation,
			Payload: payload, Recipient: email,
		}); err != nil {
			return err
		}
		return auditTeam(ctx, q, actor, ActionUserInvited, uuid.NullUUID{}, map[string]string{"role": role, "ip": ip})
	})
}

// RevokeInvitation annule une invitation en attente du cabinet.
func (s *Service) RevokeInvitation(ctx context.Context, actor User, id, ip string) error {
	if err := requireTeamAdmin(actor); err != nil {
		return err
	}
	return store.WithTx(ctx, s.Pool, func(q *sqlcgen.Queries, _ pgx.Tx) error {
		n, err := q.RevokeInvitation(ctx, sqlcgen.RevokeInvitationParams{TokenHash: id, PracticeID: actor.PracticeID})
		if err != nil {
			return fmt.Errorf("annulation de l'invitation: %w", err)
		}
		if n == 0 {
			return domain.ErrNotFound
		}
		return auditTeam(ctx, q, actor, ActionInvitationRevok, uuid.NullUUID{}, map[string]string{"ip": ip})
	})
}

// InvitationInfo décrit une invitation valide, pour la page d'activation.
type InvitationInfo struct {
	Email        string
	RoleLabel    string
	PracticeName string
}

// Invitation retourne l'invitation correspondant à token (ErrInvalidInvitation si inconnue, expirée ou utilisée).
func (s *Service) Invitation(ctx context.Context, token string) (InvitationInfo, error) {
	if token == "" || len(token) > 128 {
		return InvitationInfo{}, ErrInvalidInvitation
	}
	q := sqlcgen.New(s.Pool)
	inv, err := q.GetValidInvitation(ctx, sqlcgen.GetValidInvitationParams{TokenHash: SessionID(token), Now: s.Clock.Now()})
	if errors.Is(err, pgx.ErrNoRows) {
		return InvitationInfo{}, ErrInvalidInvitation
	}
	if err != nil {
		return InvitationInfo{}, fmt.Errorf("lecture de l'invitation: %w", err)
	}
	pc, err := q.GetPractice(ctx, inv.PracticeID)
	if err != nil {
		return InvitationInfo{}, fmt.Errorf("lecture cabinet: %w", err)
	}
	return InvitationInfo{Email: inv.Email, RoleLabel: RoleLabel(inv.Role), PracticeName: pc.Name}, nil
}

// AcceptInvitation consomme le jeton (usage unique) et crée le compte avec le
// mot de passe choisi. domain.ErrValidation si le mot de passe est trop faible
// (le jeton reste utilisable) ; ErrInvalidInvitation si le jeton est invalide.
func (s *Service) AcceptInvitation(ctx context.Context, token, password, ip string) error {
	if err := ValidatePassword(password); err != nil {
		return err
	}
	if token == "" || len(token) > 128 {
		return ErrInvalidInvitation
	}
	hash, err := Hash(password)
	if err != nil {
		return err
	}
	return store.WithTx(ctx, s.Pool, func(q *sqlcgen.Queries, _ pgx.Tx) error {
		inv, err := q.ConsumeInvitation(ctx, sqlcgen.ConsumeInvitationParams{TokenHash: SessionID(token), Now: s.Clock.Now()})
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrInvalidInvitation
		}
		if err != nil {
			return fmt.Errorf("consommation de l'invitation: %w", err)
		}
		u, err := q.CreateUser(ctx, sqlcgen.CreateUserParams{PracticeID: inv.PracticeID, Email: inv.Email, PasswordHash: hash, Role: inv.Role})
		if store.IsUniqueViolation(err) {
			return domain.ErrValidation{Field: "email", Msg: "Un compte existe déjà avec cette adresse."}
		}
		if err != nil {
			return fmt.Errorf("création du compte: %w", err)
		}
		return s.audit(ctx, q, uuid.NullUUID{UUID: u.ID, Valid: true}, ActionInvitationUsed, ip)
	})
}

// lockMember verrouille un compte du cabinet de actor (ErrNotFound sinon).
func lockMember(ctx context.Context, q *sqlcgen.Queries, actor User, id uuid.UUID) (sqlcgen.AppUser, error) {
	u, err := q.GetPracticeUserForUpdate(ctx, sqlcgen.GetPracticeUserForUpdateParams{ID: id, PracticeID: actor.PracticeID})
	if errors.Is(err, pgx.ErrNoRows) {
		return sqlcgen.AppUser{}, domain.ErrNotFound
	}
	if err != nil {
		return sqlcgen.AppUser{}, fmt.Errorf("lecture utilisateur: %w", err)
	}
	return u, nil
}

var errLastAdmin = domain.ErrValidation{Field: "role", Msg: "Le cabinet doit garder au moins un administrateur actif."}

// ensureAnotherAdmin refuse de retirer le dernier administrateur actif.
func ensureAnotherAdmin(ctx context.Context, q *sqlcgen.Queries, practiceID uuid.UUID) error {
	n, err := q.CountActiveAdmins(ctx, practiceID)
	if err != nil {
		return fmt.Errorf("comptage des administrateurs: %w", err)
	}
	if n <= 1 {
		return errLastAdmin
	}
	return nil
}

// SetRole change le rôle d'un compte du cabinet (admin seulement). Le dernier
// administrateur actif ne peut pas être rétrogradé.
func (s *Service) SetRole(ctx context.Context, actor User, id uuid.UUID, role string) error {
	if err := requireTeamAdmin(actor); err != nil {
		return err
	}
	if !domain.ValidRole(role) {
		return domain.ErrValidation{Field: "role", Msg: "Rôle inconnu."}
	}
	return store.WithTx(ctx, s.Pool, func(q *sqlcgen.Queries, _ pgx.Tx) error {
		u, err := lockMember(ctx, q, actor, id)
		if err != nil {
			return err
		}
		if u.Role == role {
			return nil
		}
		if u.Role == domain.RoleAdmin && u.Active {
			if err := ensureAnotherAdmin(ctx, q, actor.PracticeID); err != nil {
				return err
			}
		}
		if err := q.SetUserRole(ctx, sqlcgen.SetUserRoleParams{ID: id, PracticeID: actor.PracticeID, Role: role}); err != nil {
			return fmt.Errorf("changement de rôle: %w", err)
		}
		return auditTeam(ctx, q, actor, ActionUserRoleChanged, uuid.NullUUID{UUID: id, Valid: true}, map[string]string{"from": u.Role, "to": role})
	})
}

// SetActive désactive ou réactive un compte (admin seulement). Désactiver
// révoque ses sessions et retire son profil praticien de la réservation en
// ligne ; ses rendez-vous restent. On ne se désactive pas soi-même, ni le dernier administrateur.
func (s *Service) SetActive(ctx context.Context, actor User, id uuid.UUID, active bool) error {
	if err := requireTeamAdmin(actor); err != nil {
		return err
	}
	if !active && id == actor.ID {
		return domain.ErrValidation{Field: "active", Msg: "Vous ne pouvez pas désactiver votre propre compte."}
	}
	return store.WithTx(ctx, s.Pool, func(q *sqlcgen.Queries, _ pgx.Tx) error {
		u, err := lockMember(ctx, q, actor, id)
		if err != nil {
			return err
		}
		if u.Active == active {
			return nil
		}
		if !active && u.Role == domain.RoleAdmin {
			if err := ensureAnotherAdmin(ctx, q, actor.PracticeID); err != nil {
				return err
			}
		}
		if err := q.SetUserActive(ctx, sqlcgen.SetUserActiveParams{ID: id, PracticeID: actor.PracticeID, Active: active}); err != nil {
			return fmt.Errorf("changement d'état: %w", err)
		}
		if u.PractitionerID.Valid {
			if err := q.SetPractitionerActive(ctx, sqlcgen.SetPractitionerActiveParams{ID: u.PractitionerID.Bytes, PracticeID: actor.PracticeID, Active: active}); err != nil {
				return fmt.Errorf("état du praticien: %w", err)
			}
		}
		action := ActionUserReactivated
		if !active {
			if err := q.DeleteSessionsByUser(ctx, id); err != nil {
				return fmt.Errorf("révocation des sessions: %w", err)
			}
			action = ActionUserDeactivated
		}
		return auditTeam(ctx, q, actor, action, uuid.NullUUID{UUID: id, Valid: true}, nil)
	})
}
