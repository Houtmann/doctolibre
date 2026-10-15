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

	"doctolibre/internal/outbox"
	"doctolibre/internal/store"
	"doctolibre/internal/store/sqlcgen"
)

// ResetTTL est la durée de validité d'un lien de réinitialisation.
const ResetTTL = time.Hour

// Chemins des pages de réinitialisation.
const (
	ForgotPath = "/admin/password/forgot"
	ResetPath  = "/admin/password/reset"
)

// Actions d'audit.
const (
	ActionPasswordResetRequested = "password_reset_requested"
	ActionPasswordReset          = "password_reset"
)

// TemplatePasswordReset est le template email (internal/notify/templates/email).
const TemplatePasswordReset = "password_reset"

// ErrInvalidResetToken : lien inconnu, expiré ou déjà utilisé.
var ErrInvalidResetToken = errors.New("lien de réinitialisation invalide ou expiré")

// RequestPasswordReset (baseURL : URL publique, pour le lien) prépare l'envoi d'un lien de réinitialisation si un
// compte existe pour email. Le résultat est le même (nil) que le compte existe
// ou non : l'appelant doit répondre de façon identique. Le jeton n'est stocké
// que haché (sha256) ; l'email part via l'outbox, dans la même transaction.
// Les demandes précédentes de l'utilisateur sont invalidées.
func (s *Service) RequestPasswordReset(ctx context.Context, baseURL, email, ip string) error {
	email = strings.ToLower(strings.TrimSpace(email))
	if a, err := mail.ParseAddress(email); err != nil || a.Address != email || len(email) > 254 {
		return nil
	}
	if baseURL == "" {
		return errors.New("réinitialisation du mot de passe: base URL non configurée")
	}
	row, err := sqlcgen.New(s.Pool).GetUserByEmail(ctx, email)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("lecture utilisateur: %w", err)
	}
	token, err := newToken()
	if err != nil {
		return err
	}
	now := s.Clock.Now()
	link := strings.TrimRight(baseURL, "/") + ResetPath + "?token=" + url.QueryEscape(token)
	payload, err := json.Marshal(map[string]string{"reset_url": link, "valid_for": "1 heure"})
	if err != nil {
		return fmt.Errorf("payload email: %w", err)
	}
	return store.WithTx(ctx, s.Pool, func(q *sqlcgen.Queries, tx pgx.Tx) error {
		if err := q.DeletePasswordResetsByUser(ctx, row.ID); err != nil {
			return fmt.Errorf("purge des jetons: %w", err)
		}
		if err := q.CreatePasswordReset(ctx, sqlcgen.CreatePasswordResetParams{
			TokenHash: SessionID(token), UserID: row.ID, ExpiresAt: now.Add(ResetTTL), CreatedAt: now,
		}); err != nil {
			return fmt.Errorf("création du jeton: %w", err)
		}
		if err := outbox.Enqueue(ctx, tx, outbox.Msg{
			PracticeID: row.PracticeID, Kind: outbox.KindEmail, Template: TemplatePasswordReset,
			Payload: payload, Recipient: row.Email,
		}); err != nil {
			return err
		}
		return s.audit(ctx, q, uuid.NullUUID{UUID: row.ID, Valid: true}, ActionPasswordResetRequested, ip)
	})
}

// ResetTokenValid indique si token est utilisable (existe, non utilisé, non expiré).
func (s *Service) ResetTokenValid(ctx context.Context, token string) (bool, error) {
	if token == "" || len(token) > 128 {
		return false, nil
	}
	ok, err := sqlcgen.New(s.Pool).PasswordResetIsValid(ctx, sqlcgen.PasswordResetIsValidParams{
		TokenHash: SessionID(token), Now: s.Clock.Now(),
	})
	if err != nil {
		return false, fmt.Errorf("lecture du jeton: %w", err)
	}
	return ok, nil
}

// ResetPassword change le mot de passe avec un jeton à usage unique, révoque
// toutes les sessions de l'utilisateur et purge ses autres jetons.
// domain.ErrValidation si le mot de passe est trop faible (le jeton reste
// utilisable), ErrInvalidResetToken si le jeton est invalide.
func (s *Service) ResetPassword(ctx context.Context, token, newPassword, ip string) error {
	if err := ValidatePassword(newPassword); err != nil {
		return err
	}
	if token == "" || len(token) > 128 {
		return ErrInvalidResetToken
	}
	hash, err := Hash(newPassword)
	if err != nil {
		return err
	}
	return store.WithTx(ctx, s.Pool, func(q *sqlcgen.Queries, _ pgx.Tx) error {
		userID, err := q.ConsumePasswordReset(ctx, sqlcgen.ConsumePasswordResetParams{
			TokenHash: SessionID(token), Now: s.Clock.Now(),
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrInvalidResetToken
		}
		if err != nil {
			return fmt.Errorf("consommation du jeton: %w", err)
		}
		if err := q.UpdateUserPassword(ctx, sqlcgen.UpdateUserPasswordParams{ID: userID, PasswordHash: hash}); err != nil {
			return fmt.Errorf("mise à jour du mot de passe: %w", err)
		}
		if err := q.DeleteSessionsByUser(ctx, userID); err != nil {
			return fmt.Errorf("révocation des sessions: %w", err)
		}
		if err := q.DeletePasswordResetsByUser(ctx, userID); err != nil {
			return fmt.Errorf("purge des jetons: %w", err)
		}
		return s.audit(ctx, q, uuid.NullUUID{UUID: userID, Valid: true}, ActionPasswordReset, ip)
	})
}
