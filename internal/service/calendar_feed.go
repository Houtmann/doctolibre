package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"doctolibre/internal/domain"
	"doctolibre/internal/ics"
	"doctolibre/internal/store"
	"doctolibre/internal/store/sqlcgen"
)

// Fenêtre du flux : de J-30 à J+90.
const (
	feedDaysBefore = 30
	feedDaysAfter  = 90
)

// CalendarFeed gère l'abonnement ICS en lecture seule d'un praticien.
// Le jeton n'est jamais stocké en clair (sha256 hex).
type CalendarFeed struct {
	Pool  *pgxpool.Pool
	Clock domain.Clock
}

// FeedStatus est l'état de l'abonnement d'un utilisateur.
type FeedStatus struct {
	Enabled   bool
	FullNames bool
}

// FeedTokenHash est le haché (sha256 hex) stocké dans calendar_feed_token.
func FeedTokenHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func newFeedToken() (string, error) {
	var buf [32]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", fmt.Errorf("jeton de flux: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf[:]), nil
}

// Status retourne l'état de l'abonnement de l'utilisateur.
func (f *CalendarFeed) Status(ctx context.Context, a Actor) (FeedStatus, error) {
	row, err := sqlcgen.New(f.Pool).GetFeedTokenByUser(ctx, a.UserID)
	if errors.Is(err, pgx.ErrNoRows) {
		return FeedStatus{}, nil
	}
	if err != nil {
		return FeedStatus{}, fmt.Errorf("lecture jeton de flux: %w", err)
	}
	return FeedStatus{Enabled: true, FullNames: row.FullNames}, nil
}

// Regenerate crée (ou remplace) le jeton et le retourne en clair — c'est la seule
// fois où il est visible. L'ancien lien cesse de fonctionner. Un secrétaire (sans
// agenda) reçoit domain.ErrForbidden.
func (f *CalendarFeed) Regenerate(ctx context.Context, a Actor, fullNames bool) (string, error) {
	token, err := newFeedToken()
	if err != nil {
		return "", err
	}
	err = store.WithTx(ctx, f.Pool, func(q *sqlcgen.Queries, _ pgx.Tx) error {
		if _, err := ensurePractitioner(ctx, q, a); err != nil {
			return err
		}
		return q.UpsertFeedToken(ctx, sqlcgen.UpsertFeedTokenParams{
			UserID: a.UserID, TokenHash: FeedTokenHash(token), FullNames: fullNames,
		})
	})
	if err != nil {
		return "", fmt.Errorf("régénération du jeton de flux: %w", err)
	}
	return token, nil
}

// SetFullNames change l'option « nom complet » sans changer le jeton.
// domain.ErrNotFound si l'abonnement n'est pas activé.
func (f *CalendarFeed) SetFullNames(ctx context.Context, a Actor, fullNames bool) error {
	n, err := sqlcgen.New(f.Pool).SetFeedFullNames(ctx, sqlcgen.SetFeedFullNamesParams{UserID: a.UserID, FullNames: fullNames})
	if err != nil {
		return fmt.Errorf("option du flux: %w", err)
	}
	if n == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// Revoke supprime le jeton : le lien répond 404.
func (f *CalendarFeed) Revoke(ctx context.Context, a Actor) error {
	if err := sqlcgen.New(f.Pool).DeleteFeedToken(ctx, a.UserID); err != nil {
		return fmt.Errorf("révocation du jeton de flux: %w", err)
	}
	return nil
}

// Render retourne le fichier .ics du jeton et son ETag (guillemets inclus).
// domain.ErrNotFound si le jeton est inconnu ou révoqué. La recherche se fait par
// égalité sur le haché (index unique) : le jeton lui-même, à 256 bits aléatoires,
// n'est pas devinable ; aucun secret n'est comparé octet à octet côté applicatif.
func (f *CalendarFeed) Render(ctx context.Context, token string) (body []byte, etag string, err error) {
	q := sqlcgen.New(f.Pool)
	owner, err := q.GetFeedOwnerByTokenHash(ctx, FeedTokenHash(token))
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && !owner.PractitionerID.Valid) {
		return nil, "", domain.ErrNotFound
	}
	if err != nil {
		return nil, "", fmt.Errorf("lecture du jeton de flux: %w", err)
	}
	now := f.Clock.Now()
	rows, err := q.ListFeedAppointments(ctx, sqlcgen.ListFeedAppointmentsParams{
		PracticeID:     owner.PracticeID,
		PractitionerID: owner.PractitionerID.Bytes,
		FromAt:         now.AddDate(0, 0, -feedDaysBefore),
		ToAt:           now.AddDate(0, 0, feedDaysAfter),
	})
	if err != nil {
		return nil, "", fmt.Errorf("lecture des rendez-vous du flux: %w", err)
	}
	events := make([]ics.Event, len(rows))
	for i, r := range rows {
		status := "CONFIRMED"
		if r.Status == "cancelled" {
			status = "CANCELLED"
		}
		events[i] = ics.Event{
			UID: r.ID.String() + "@doctolibre", Start: r.StartsAt, End: r.EndsAt,
			Stamp: r.UpdatedAt, Sequence: int(r.UpdatedAt.Unix()), Status: status,
			Summary: feedSummary(r.FirstName, r.LastName, r.TypeName, owner.FullNames),
		}
	}
	body = ics.Feed("Doctolibre", events)
	sum := sha256.Sum256(body)
	return body, `"` + hex.EncodeToString(sum[:16]) + `"`, nil
}

// feedSummary : « RDV L.D. - Motif » ou, option cochée, « RDV Léa Durand - Motif ».
func feedSummary(first, last, typeName string, full bool) string {
	who := initial(first) + initial(last)
	if full {
		who = strings.TrimSpace(first + " " + last)
	}
	s := "RDV"
	if who != "" {
		s += " " + who
	}
	if typeName != "" {
		s += " - " + typeName
	}
	return s
}

func initial(name string) string {
	r, _ := utf8.DecodeRuneInString(strings.TrimSpace(name))
	if r == utf8.RuneError {
		return ""
	}
	return string(unicode.ToUpper(r)) + "."
}
