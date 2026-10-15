package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"doctolibre/internal/domain"
	"doctolibre/internal/store"
	"doctolibre/internal/store/sqlcgen"
)

// MinPasswordLen est la longueur minimale (en caractères) d'un mot de passe.
const MinPasswordLen = 12

// CreateUserParams décrit l'utilisateur à créer.
type CreateUserParams struct {
	Email        string
	Role         string
	PracticeName string
	Password     string
	Timezone     string // fuseau du cabinet créé ; "" = Europe/Paris
}

// ValidatePassword vérifie la politique de mot de passe.
func ValidatePassword(pw string) error {
	if utf8.RuneCountInString(pw) < MinPasswordLen {
		return domain.ErrValidation{Field: "password", Msg: fmt.Sprintf("au moins %d caractères", MinPasswordLen)}
	}
	if len(pw) > 256 {
		return domain.ErrValidation{Field: "password", Msg: "trop long"}
	}
	return nil
}

// CreateUser crée le cabinet s'il n'existe pas (par nom exact) puis l'utilisateur,
// dans une même transaction.
func CreateUser(ctx context.Context, pool *pgxpool.Pool, p CreateUserParams) (sqlcgen.AppUser, error) {
	email := strings.ToLower(strings.TrimSpace(p.Email))
	if a, err := mail.ParseAddress(email); err != nil || a.Address != email {
		return sqlcgen.AppUser{}, domain.ErrValidation{Field: "email", Msg: "adresse invalide"}
	}
	if !domain.ValidRole(p.Role) {
		return sqlcgen.AppUser{}, domain.ErrValidation{Field: "role", Msg: "rôle inconnu (admin, practitioner, secretary)"}
	}
	name := strings.TrimSpace(p.PracticeName)
	if name == "" {
		return sqlcgen.AppUser{}, domain.ErrValidation{Field: "practice-name", Msg: "requis"}
	}
	if err := ValidatePassword(p.Password); err != nil {
		return sqlcgen.AppUser{}, err
	}
	hash, err := Hash(p.Password)
	if err != nil {
		return sqlcgen.AppUser{}, err
	}
	tz := p.Timezone
	if tz == "" {
		tz = "Europe/Paris"
	}

	var user sqlcgen.AppUser
	err = store.WithTx(ctx, pool, func(q *sqlcgen.Queries, _ pgx.Tx) error {
		practice, err := q.GetPracticeByName(ctx, name)
		if errors.Is(err, pgx.ErrNoRows) {
			slug := Slugify(name)
			if _, serr := q.GetPracticeBySlug(ctx, slug); serr == nil {
				slug += "-" + randomSuffix()
			} else if !errors.Is(serr, pgx.ErrNoRows) {
				return fmt.Errorf("lecture cabinet: %w", serr)
			}
			practice, err = q.CreatePractice(ctx, sqlcgen.CreatePracticeParams{Name: name, Slug: slug, Timezone: tz})
		}
		if err != nil {
			return fmt.Errorf("cabinet: %w", err)
		}
		user, err = q.CreateUser(ctx, sqlcgen.CreateUserParams{
			PracticeID: practice.ID, Email: email, PasswordHash: hash, Role: p.Role,
		})
		if store.IsUniqueViolation(err) {
			return domain.ErrValidation{Field: "email", Msg: "déjà utilisé"}
		}
		if err != nil {
			return fmt.Errorf("utilisateur: %w", err)
		}
		return nil
	})
	return user, err
}

func randomSuffix() string {
	b := make([]byte, 3)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

var accentFold = strings.NewReplacer(
	"à", "a", "â", "a", "ä", "a", "é", "e", "è", "e", "ê", "e", "ë", "e",
	"î", "i", "ï", "i", "ô", "o", "ö", "o", "ù", "u", "û", "u", "ü", "u",
	"ç", "c", "œ", "oe", "æ", "ae", "ÿ", "y", "ñ", "n",
)

// Slugify transforme un nom en slug [a-z0-9-] (« Cabinet Kiné Étoile » → « cabinet-kine-etoile »).
func Slugify(name string) string {
	s := accentFold.Replace(strings.ToLower(name))
	var b strings.Builder
	dash := true // évite un tiret en tête
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			dash = false
		case !dash:
			b.WriteByte('-')
			dash = true
		}
	}
	out := strings.TrimRight(b.String(), "-")
	if out == "" {
		return "cabinet"
	}
	return out
}
