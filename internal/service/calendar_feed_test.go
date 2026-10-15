package service_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"doctolibre/internal/auth"
	"doctolibre/internal/domain"
	"doctolibre/internal/service"
	"doctolibre/internal/testutil"
)

func TestCalendarFeedLifecycle(t *testing.T) {
	t.Parallel()
	f := newCalFixture(t)
	ctx := context.Background()
	clock := testutil.NewFixedClock(f.cal.Clock.Now())
	feed := &service.CalendarFeed{Pool: f.db, Clock: clock}

	if _, _, err := feed.Render(ctx, "inconnu"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("jeton inconnu: %v", err)
	}
	if st, err := feed.Status(ctx, f.act); err != nil || st.Enabled {
		t.Fatalf("statut initial: %+v %v", st, err)
	}
	if err := feed.SetFullNames(ctx, f.act, true); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("option sans abonnement: %v", err)
	}

	// Trois RDV : dans la fenêtre, annulé, hors fenêtre (> J+90).
	keep, err := f.cal.Create(ctx, f.act, f.quick("2026-10-19", "09:00", false))
	if err != nil {
		t.Fatal(err)
	}
	gone, err := f.cal.Create(ctx, f.act, f.quick("2026-10-20", "09:00", false))
	if err != nil {
		t.Fatal(err)
	}
	if err := f.cal.Cancel(ctx, f.act, gone.ID, "test"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.cal.Create(ctx, f.act, f.quick("2027-03-01", "09:00", true)); err != nil {
		t.Fatal(err)
	}

	token, err := feed.Regenerate(ctx, f.act, false)
	if err != nil || len(token) < 40 {
		t.Fatalf("génération: %q %v", token, err)
	}
	body, etag, err := feed.Render(ctx, token)
	if err != nil {
		t.Fatal(err)
	}
	s := string(body)
	for _, want := range []string{
		"UID:" + keep.ID.String() + "@doctolibre\r\n", "UID:" + gone.ID.String() + "@doctolibre\r\n",
		"STATUS:CONFIRMED\r\n", "STATUS:CANCELLED\r\n", "SUMMARY:RDV L.D. - Séance tampon\r\n",
		"DTSTART:20261019T070000Z\r\n", // 09:00 Paris (CEST) = 07:00 UTC
	} {
		if !strings.Contains(s, want) {
			t.Errorf("fragment manquant %q dans\n%s", want, s)
		}
	}
	if n := strings.Count(s, "BEGIN:VEVENT"); n != 2 {
		t.Errorf("%d événements, attendu 2 (le RDV hors fenêtre est exclu)", n)
	}
	// Minimisation : ni nom, ni téléphone, ni email.
	for _, leak := range []string{"Durand", "Léa", "0612", "612345678", "@example"} {
		if strings.Contains(s, leak) {
			t.Errorf("donnée patient %q dans le flux", leak)
		}
	}
	if _, etag2, _ := feed.Render(ctx, token); etag2 != etag {
		t.Errorf("ETag instable : %s / %s", etag, etag2)
	}

	// Option nom complet : même jeton, contenu et ETag différents.
	if err := feed.SetFullNames(ctx, f.act, true); err != nil {
		t.Fatal(err)
	}
	body, etag3, err := feed.Render(ctx, token)
	if err != nil || !strings.Contains(string(body), "SUMMARY:RDV Léa Durand - Séance tampon") || etag3 == etag {
		t.Errorf("nom complet: %v etag=%s\n%s", err, etag3, body)
	}

	// Régénération : l'ancien jeton meurt.
	token2, err := feed.Regenerate(ctx, f.act, false)
	if err != nil || token2 == token {
		t.Fatalf("régénération: %v", err)
	}
	if _, _, err := feed.Render(ctx, token); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("ancien jeton: %v", err)
	}
	if _, _, err := feed.Render(ctx, token2); err != nil {
		t.Errorf("nouveau jeton: %v", err)
	}

	// Révocation.
	if err := feed.Revoke(ctx, f.act); err != nil {
		t.Fatal(err)
	}
	if _, _, err := feed.Render(ctx, token2); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("jeton révoqué: %v", err)
	}
}

func TestCalendarFeedIsolationAndRoles(t *testing.T) {
	t.Parallel()
	f := newCalFixture(t)
	ctx := context.Background()
	feed := &service.CalendarFeed{Pool: f.db, Clock: testutil.NewFixedClock(f.cal.Clock.Now())}
	if _, err := f.cal.Create(ctx, f.act, f.quick("2026-10-19", "09:00", false)); err != nil {
		t.Fatal(err)
	}

	// Un autre cabinet ne voit rien du premier.
	other, err := auth.CreateUser(ctx, f.db, auth.CreateUserParams{
		Email: "autre@example.test", Role: domain.RolePractitioner, PracticeName: "Autre cabinet", Password: testPassword,
	})
	if err != nil {
		t.Fatal(err)
	}
	tok, err := feed.Regenerate(ctx, service.Actor{UserID: other.ID, PracticeID: other.PracticeID}, false)
	if err != nil {
		t.Fatal(err)
	}
	body, _, err := feed.Render(ctx, tok)
	if err != nil || strings.Contains(string(body), "BEGIN:VEVENT") {
		t.Errorf("flux d'un autre cabinet: %v\n%s", err, body)
	}

	// Un secrétaire n'a pas d'agenda.
	sec, err := auth.CreateUser(ctx, f.db, auth.CreateUserParams{
		Email: "secr@example.test", Role: domain.RoleSecretary, PracticeName: "Cabinet secr", Password: testPassword,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := feed.Regenerate(ctx, service.Actor{UserID: sec.ID, PracticeID: sec.PracticeID}, false); !errors.Is(err, domain.ErrForbidden) {
		t.Errorf("secrétaire: %v", err)
	}
}
