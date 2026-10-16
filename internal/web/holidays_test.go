package web_test

import (
	"context"
	"net/url"
	"strings"
	"testing"

	"doctolibre/internal/domain"
	"doctolibre/internal/store/sqlcgen"
)

func TestSettingsHolidaysCheckboxes(t *testing.T) {
	t.Parallel()
	_, c := newConfigAdmin(t, domain.RoleAdmin)
	mustContain(t, c.do("GET", "/admin/settings", nil), 200, `name="close_on_holidays"`, `name="holidays_alsace_moselle"`)

	rec := c.do("POST", "/admin/settings", url.Values{
		"name": {"Cabinet Étoile"}, "booking_notice_hours": {"6"}, "cancel_notice_hours": {"48"}, "horizon_days": {"45"},
		"first_name": {"Camille"}, "last_name": {"Martin"}, "slug": {"camille-martin"}, "color": {"#2563eb"},
		"close_on_holidays": {"1"}, "holidays_alsace_moselle": {"1"},
	})
	if rec.Code != 303 {
		t.Fatalf("POST: %d", rec.Code)
	}
	body := c.do("GET", "/admin/settings", nil).Body.String()
	if !strings.Contains(body, `name="close_on_holidays" value="1" checked`) || !strings.Contains(body, `name="holidays_alsace_moselle" value="1" checked`) {
		t.Errorf("cases non cochées après enregistrement")
	}
	// Décochées : retour à false.
	c.do("POST", "/admin/settings", url.Values{
		"name": {"Cabinet Étoile"}, "booking_notice_hours": {"6"}, "cancel_notice_hours": {"48"}, "horizon_days": {"45"},
		"first_name": {"Camille"}, "last_name": {"Martin"}, "slug": {"camille-martin"}, "color": {"#2563eb"},
	})
	if strings.Contains(c.do("GET", "/admin/settings", nil).Body.String(), `name="close_on_holidays" value="1" checked`) {
		t.Errorf("case encore cochée")
	}
}

func TestCalendarShowsHoliday(t *testing.T) {
	t.Parallel()
	e := newCalendarEnv(t, domain.RoleAdmin)
	// Mercredi 11 novembre 2026 (Armistice) : plage 08–12 les jours ouvrés.
	const day = "start=2026-11-11T00:00:00&end=2026-11-12T00:00:00"
	if body := e.c.do("GET", "/admin/calendar?view=day&date=2026-11-11", nil).Body.String(); strings.Contains(body, "Férié") {
		t.Fatal("option désactivée : pas de « Férié »")
	}
	if evs := e.events(t, day); len(evs) != 2 {
		t.Fatalf("option désactivée : %d plages fermées, attendu 2 (avant 08h, après 12h)", len(evs))
	}
	if err := sqlcgen.New(e.db).SetPracticeHolidays(context.Background(), sqlcgen.SetPracticeHolidaysParams{ID: e.actor.PracticeID, CloseOnHolidays: true}); err != nil {
		t.Fatal(err)
	}
	mustContain(t, e.c.do("GET", "/admin/calendar?view=day&date=2026-11-11", nil), 200, `Férié`)
	// Jour férié : une seule plage fermée, la journée entière.
	evs := e.events(t, day)
	if len(evs) != 1 || evs[0].Display != "background" || evs[0].Start != "2026-11-11T00:00:00+01:00" || evs[0].End != "2026-11-12T00:00:00+01:00" {
		t.Fatalf("jour férié: %+v", evs)
	}
	// Ouverture exceptionnelle partielle le jour férié : seule cette plage est ouverte.
	if _, err := e.db.Exec(context.Background(), `INSERT INTO availability_exception (practitioner_id, during, kind, reason)
		SELECT id, tstzrange('2026-11-11 14:00+01', '2026-11-11 16:00+01', '[)'), 'open', 'test' FROM practitioner WHERE practice_id = $1`, e.actor.PracticeID); err != nil {
		t.Fatal(err)
	}
	evs = e.events(t, day)
	if len(evs) != 2 || evs[0].End != "2026-11-11T14:00:00+01:00" || evs[1].Start != "2026-11-11T16:00:00+01:00" {
		t.Fatalf("ouverture exceptionnelle: %+v", evs)
	}
	mustContain(t, e.c.do("GET", "/admin/calendar?view=day&date=2026-11-11", nil), 200, `Férié`)
}
