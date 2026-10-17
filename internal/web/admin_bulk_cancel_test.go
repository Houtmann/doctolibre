package web_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"doctolibre/internal/auth"
	"doctolibre/internal/config"
	"doctolibre/internal/domain"
	"doctolibre/internal/service"
	"doctolibre/internal/store/sqlcgen"
	"doctolibre/internal/testutil"
	"doctolibre/internal/web"
)

// newBulkAdmin monte le handler complet avec le service d'agenda ; deux rendez-vous
// sont pris le mardi 2026-10-20 (9 h et 10 h) pour le praticien de l'utilisateur.
func newBulkAdmin(t *testing.T, role string) (*client, service.Calendar) {
	t.Helper()
	db := testutil.DB(t)
	ctx := context.Background()
	u, err := auth.CreateUser(ctx, db, auth.CreateUserParams{Email: userEmail, Role: role, PracticeName: "Cabinet test", Password: pw})
	if err != nil {
		t.Fatal(err)
	}
	clock := testutil.NewFixedClock(time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC))
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	key := service.NewManageKey(testutil.ManageSecret)
	cal := &service.Calendar{Pool: db, Clock: clock, ManageKey: key}
	h := web.Routes(web.Deps{
		Config:   config.Config{Env: config.EnvDev},
		Logger:   logger,
		DB:       db,
		Auth:     &auth.Service{Pool: db, Clock: clock, Logger: logger},
		Practice: &service.Practice{Pool: db, Clock: clock},
		Calendar: cal,
	})
	c := newClient(t, h, "192.0.2.10")
	if rec := c.login(userEmail, pw, "/admin"); rec.Code != 303 {
		t.Fatalf("login: %d", rec.Code)
	}
	if role != domain.RoleSecretary {
		types, err := sqlcgen.New(db).ListAppointmentTypes(ctx, sqlcgen.ListAppointmentTypesParams{PracticeID: u.PracticeID, PageSize: 10})
		if err != nil || len(types) == 0 {
			t.Fatalf("motifs: %v", err)
		}
		act := service.Actor{UserID: u.ID, PracticeID: u.PracticeID}
		for i, clock := range []string{"09:00", "10:00"} {
			_, err := cal.Create(ctx, act, service.NewAppointmentInput{
				NewFirstName: []string{"Léa", "Marc"}[i], NewLastName: "Durand", NewPhone: "06 12 34 56 7" + string(rune('8'+i)),
				TypeID: types[0].ID.String(), Date: "2026-10-20", Time: clock, Force: true,
			})
			if err != nil {
				t.Fatalf("création du RDV %d: %v", i, err)
			}
		}
	}
	return c, *cal
}

func TestBulkCancelWebFlow(t *testing.T) {
	t.Parallel()
	c, _ := newBulkAdmin(t, domain.RoleAdmin)

	// Le formulaire est sur la page des absences, et l'agenda y renvoie.
	mustContain(t, c.do("GET", "/admin/absences", nil), 200, "Absence imprévue", `action="/admin/absences/cancel"`)
	mustContain(t, c.do("GET", "/admin/calendar", nil), 200, `#bulk-cancel"`)

	q := url.Values{"start_date": {"2026-10-20"}, "start_time": {"10:00"}, "end_time": {"12:00"}, "reason": {"Praticien souffrant"}}
	// Aperçu : seul le RDV de 10 h est touché, rien n'est modifié.
	prev := c.do("GET", "/admin/absences/cancel?"+q.Encode(), nil)
	mustContain(t, prev, 200, "1 rendez-vous sera annulé", "Marc Durand", "Praticien souffrant", `name="reason" value="Praticien souffrant"`)
	if body := prev.Body.String(); strings.Contains(body, "Léa Durand") {
		t.Errorf("le RDV de 9 h ne devrait pas être listé")
	}
	mustContain(t, c.do("GET", "/admin/absences", nil), 200, "Aucune absence à venir.")

	// Confirmation : POST + redirect, message de succès, absence listée.
	if rec := c.do("POST", "/admin/absences/cancel", q); rec.Code != 303 {
		t.Fatalf("confirmation: %d\n%s", rec.Code, rec.Body.String())
	}
	mustContain(t, c.do("GET", "/admin/absences", nil), 200, "1 rendez-vous annulé", "Praticien souffrant")

	// Validation : retour sur la page des absences, formulaire conservé.
	bad := url.Values{"start_date": {"2026-10-20"}, "start_time": {"09:00"}}
	mustContain(t, c.do("GET", "/admin/absences/cancel?"+bad.Encode(), nil), 422, "deux heures", `value="2026-10-20"`)
	mustContain(t, c.do("POST", "/admin/absences/cancel", bad), 422, "deux heures")
}

func TestBulkCancelWebRefusesSecretaryAndRequiresCSRF(t *testing.T) {
	t.Parallel()
	c, _ := newBulkAdmin(t, domain.RoleSecretary)
	q := url.Values{"start_date": {"2026-10-20"}}
	for _, target := range []string{"/admin/absences/cancel?" + q.Encode()} {
		if rec := c.do("GET", target, nil); rec.Code != http.StatusForbidden && rec.Code != http.StatusNotFound {
			t.Errorf("GET %s secrétariat: %d", target, rec.Code)
		}
	}
	if rec := c.do("POST", "/admin/absences/cancel", q); rec.Code != http.StatusForbidden && rec.Code != http.StatusNotFound {
		t.Errorf("POST secrétariat: %d", rec.Code)
	}
	// L'agenda du secrétariat n'affiche pas le lien.
	if body := c.do("GET", "/admin/calendar", nil).Body.String(); strings.Contains(body, "Absence imprévue") {
		t.Error("lien d'absence imprévue visible du secrétariat")
	}
}

func TestBulkCancelWebPractitionerCannotTargetAnotherAgenda(t *testing.T) {
	t.Parallel()
	c, _ := newBulkAdmin(t, domain.RolePractitioner)
	q := url.Values{"start_date": {"2026-10-20"}, "practitioner": {"6f1c1a52-3a6e-4d6e-9a3e-0d3b2f0a1b11"}}
	if rec := c.do("GET", "/admin/absences/cancel?"+q.Encode(), nil); rec.Code != http.StatusForbidden {
		t.Errorf("GET agenda d'un autre : %d", rec.Code)
	}
	if rec := c.do("POST", "/admin/absences/cancel", q); rec.Code != http.StatusForbidden {
		t.Errorf("POST agenda d'un autre : %d", rec.Code)
	}
	own := url.Values{"start_date": {"2026-10-20"}}
	if rec := c.do("GET", "/admin/absences/cancel?"+own.Encode(), nil); rec.Code != http.StatusOK {
		t.Errorf("GET son agenda : %d", rec.Code)
	}
}
