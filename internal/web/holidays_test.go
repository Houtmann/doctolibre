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
		"first_name": {"Camille"}, "last_name": {"Martin"}, "slug": {"camille-martin"},
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
		"first_name": {"Camille"}, "last_name": {"Martin"}, "slug": {"camille-martin"},
	})
	if strings.Contains(c.do("GET", "/admin/settings", nil).Body.String(), `name="close_on_holidays" value="1" checked`) {
		t.Errorf("case encore cochée")
	}
}

func TestCalendarShowsHoliday(t *testing.T) {
	t.Parallel()
	e := newCalendarEnv(t, domain.RoleAdmin)
	// Semaine du 26 octobre au 1er novembre 2026 (Toussaint, dimanche).
	rec := e.c.do("GET", "/admin/calendar?view=week&date=2026-10-28", nil)
	if strings.Contains(rec.Body.String(), "Férié") {
		t.Fatal("option désactivée : pas de « Férié »")
	}
	if err := sqlcgen.New(e.db).SetPracticeHolidays(context.Background(), sqlcgen.SetPracticeHolidaysParams{ID: e.actor.PracticeID, CloseOnHolidays: true}); err != nil {
		t.Fatal(err)
	}
	rec = e.c.do("GET", "/admin/calendar?view=week&date=2026-10-28", nil)
	mustContain(t, rec, 200, `<small class="cal-holiday">Férié</small>`)
	if n := strings.Count(rec.Body.String(), "cal-holiday"); n != 1 {
		t.Errorf("« Férié » affiché %d fois, attendu 1", n)
	}
	// Ouverture exceptionnelle partielle le jour férié : il reste marqué « Férié ».
	if _, err := e.db.Exec(context.Background(), `INSERT INTO availability_exception (practitioner_id, during, kind, reason)
		SELECT id, tstzrange('2026-11-01 14:00+01', '2026-11-01 16:00+01', '[)'), 'open', 'test' FROM practitioner WHERE practice_id = $1`, e.actor.PracticeID); err != nil {
		t.Fatal(err)
	}
	rec = e.c.do("GET", "/admin/calendar?view=week&date=2026-10-28", nil)
	mustContain(t, rec, 200, `<small class="cal-holiday">Férié</small>`)
}
