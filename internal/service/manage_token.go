package service

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"doctolibre/internal/domain"
	"doctolibre/internal/store"
	"doctolibre/internal/store/sqlcgen"
)

// manageTokenLabel sépare la clé des jetons de gestion des autres usages de SecretKey.
const manageTokenLabel = "manage-token/v1"

// ErrNoManageKey : un service qui crée ou lit des liens de gestion a été construit
// sans clé (DOCTOLIBRE_SECRET_KEY). Erreur de câblage, jamais d'entrée utilisateur.
var ErrNoManageKey = errors.New("clé des liens de gestion absente (DOCTOLIBRE_SECRET_KEY)")

// ManageKey est la clé dérivée de Config.SecretKey pour les jetons de gestion.
// Valeur à injecter dans chaque service qui crée ou relit des rendez-vous.
type ManageKey []byte

// NewManageKey dérive la clé : HMAC-SHA256(secret, "manage-token/v1").
func NewManageKey(secret string) ManageKey {
	m := hmac.New(sha256.New, []byte(secret))
	m.Write([]byte(manageTokenLabel))
	return m.Sum(nil)
}

// DeriveManageToken calcule le jeton du lien /r/{jeton} d'un rendez-vous :
// base64url(HMAC-SHA256(key, id ":" version)). Fonction pure.
func DeriveManageToken(key []byte, id uuid.UUID, version int) string {
	m := hmac.New(sha256.New, key)
	m.Write([]byte(id.String() + ":" + strconv.Itoa(version))) // hash.Hash.Write ne renvoie jamais d'erreur
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}

// Token retourne le jeton dérivé du rendez-vous (id, version).
func (k ManageKey) Token(id uuid.UUID, version int) string { return DeriveManageToken(k, id, version) }

// mint tire l'id d'un futur rendez-vous et calcule son jeton (version 1) et le
// haché à stocker. Le jeton n'est jamais stocké ni journalisé.
func (k ManageKey) mint() (id uuid.UUID, token, hash string, err error) {
	if len(k) == 0 {
		return uuid.Nil, "", "", ErrNoManageKey
	}
	id, err = uuid.NewRandom()
	if err != nil {
		return uuid.Nil, "", "", fmt.Errorf("id du rendez-vous: %w", err)
	}
	token = k.Token(id, 1)
	return id, token, ManageTokenHash(token), nil
}

// forAppointment retourne le jeton à mettre dans un message pour un rendez-vous
// existant, ou "" s'il ne correspond pas au haché stocké (ancien RDV à jeton
// aléatoire, lien révoqué ou effacé). Comparaison en temps constant.
func (k ManageKey) forAppointment(a sqlcgen.Appointment) string {
	if len(k) == 0 || !a.ManageTokenHash.Valid {
		return ""
	}
	t := k.Token(a.ID, int(a.ManageTokenVersion))
	if subtle.ConstantTimeCompare([]byte(ManageTokenHash(t)), []byte(a.ManageTokenHash.String)) != 1 {
		return ""
	}
	return t
}

// RevokeManageToken invalide le lien de gestion actuel du rendez-vous (version + 1,
// haché réécrit) et retourne le nouveau jeton. Pas encore appelé par l'application :
// mécanisme prévu pour une future action « renvoyer / invalider le lien ».
// domain.ErrNotFound si le rendez-vous n'est pas dans le cabinet.
func (b *Booking) RevokeManageToken(ctx context.Context, practiceID, appointmentID uuid.UUID) (string, error) {
	if len(b.ManageKey) == 0 {
		return "", ErrNoManageKey
	}
	var token string
	err := store.WithTx(ctx, b.Pool, func(q *sqlcgen.Queries, _ pgx.Tx) error {
		a, err := q.LockAppointment(ctx, sqlcgen.LockAppointmentParams{ID: appointmentID, PracticeID: practiceID})
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("verrou du rendez-vous: %w", err)
		}
		version := a.ManageTokenVersion + 1
		token = b.ManageKey.Token(a.ID, int(version))
		if err := q.SetManageToken(ctx, sqlcgen.SetManageTokenParams{
			Version: version, ManageTokenHash: pgtype.Text{String: ManageTokenHash(token), Valid: true},
			ID: a.ID, PracticeID: practiceID,
		}); err != nil {
			return fmt.Errorf("révocation du lien: %w", err)
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	return token, nil
}
