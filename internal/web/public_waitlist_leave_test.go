package web_test

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"

	"doctolibre/internal/service"
	"doctolibre/internal/testutil"
)

// TestPublicWaitlistLeave : GET n'écrit rien, POST désinscrit, un lien rejoué ou
// inconnu donne la même page neutre.
func TestPublicWaitlistLeave(t *testing.T) {
	t.Parallel()
	e := newPublicEnv(t)
	wl := withWaitlist(t, e)
	ctx := context.Background()
	c := newClient(t, e.h, "192.0.2.70")

	if _, err := wl.AddByStaff(ctx, staffActor(t, e), service.StaffAddInput{
		Patient: service.PatientDetails{FirstName: "Bob", LastName: "Attente", Email: "bob@example.test", SMSOptIn: true, EmailOptIn: true},
		Window:  service.WindowInput{TypeID: e.typ.ID, Earliest: "2026-10-15", Latest: "2026-10-31"},
	}); err != nil {
		t.Fatal(err)
	}
	// Le lien est celui des messages d'offre : on le reconstruit comme eux, depuis le haché stocké.
	var id string
	if err := e.db.QueryRow(ctx, "SELECT id FROM waitlist_entry").Scan(&id); err != nil {
		t.Fatal(err)
	}
	key := service.NewManageKey(testutil.ManageSecret)
	token := key.WaitlistToken(uuid.MustParse(id))

	rec := c.do("GET", "/wl/"+token, nil)
	mustContain(t, rec, 200, "Quitter la liste d'attente", "Me désinscrire", `name="csrf_token"`, `action="/wl/`+token+`"`)
	if rec.Header().Get("Cache-Control") != "no-store" || rec.Header().Get("Referrer-Policy") != "no-referrer" {
		t.Error("la page de désinscription doit être no-store et no-referrer")
	}
	var status string
	if err := e.db.QueryRow(ctx, "SELECT status FROM waitlist_entry").Scan(&status); err != nil || status != "active" {
		t.Fatalf("statut après GET = %q, %v", status, err)
	}

	mustContain(t, c.do("POST", "/wl/"+token, url.Values{}), 200, "Vous avez quitté la liste d")
	if err := e.db.QueryRow(ctx, "SELECT status FROM waitlist_entry").Scan(&status); err != nil || status != "cancelled" {
		t.Fatalf("statut après POST = %q, %v", status, err)
	}

	// Rejoué, GET ou POST, et inconnu : même réponse.
	replay := c.do("GET", "/wl/"+token, nil)
	unknown := c.do("GET", "/wl/inconnu", nil)
	if replay.Code != http.StatusNotFound || unknown.Code != http.StatusNotFound || replay.Body.String() != unknown.Body.String() {
		t.Errorf("lien rejoué (%d) et inconnu (%d) doivent donner la même page neutre", replay.Code, unknown.Code)
	}
	mustContain(t, replay, 404, "Ce lien n'est plus valable")
	mustContain(t, c.do("POST", "/wl/"+token, url.Values{}), 404, "Ce lien n'est plus valable")
	mustContain(t, c.do("POST", "/wl/inconnu", url.Values{}), 404, "Ce lien n'est plus valable")
	if strings.Contains(replay.Body.String(), "Dupont") || strings.Contains(replay.Body.String(), "Bob") {
		t.Error("la page neutre ne doit rien révéler")
	}
}

// TestPublicWaitlistLeaveRateLimited : le lien de désinscription partage le seau « offer » par IP.
func TestPublicWaitlistLeaveRateLimited(t *testing.T) {
	t.Parallel()
	e := newPublicEnv(t)
	withWaitlist(t, e)
	c := newClient(t, e.h, "192.0.2.71")
	limited := false
	for range 30 {
		rec := c.do("GET", "/wl/inconnu", nil)
		if rec.Code == http.StatusTooManyRequests {
			limited = true
			break
		}
		if rec.Code != http.StatusNotFound {
			t.Fatalf("statut inattendu %d", rec.Code)
		}
	}
	if !limited {
		t.Error("aucune limite de débit sur /wl/{jeton}")
	}
}
