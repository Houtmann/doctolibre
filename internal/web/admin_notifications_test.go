package web_test

import (
	"net/url"
	"testing"

	"doctolibre/internal/domain"
)

func TestNotificationSettingsPage(t *testing.T) {
	t.Parallel()
	_, c := newConfigAdmin(t, domain.RoleAdmin)

	// Valeurs par défaut et estimation (aucun rendez-vous : 0 SMS).
	rec := c.do("GET", "/admin/settings/notifications", nil)
	mustContain(t, rec, 200, "Notifications aux patients", `name="reminder_hour"`, "Estimation des SMS", "0 SMS par mois")

	// Enregistrement valide : redirection puis relecture.
	form := url.Values{
		"confirmation_channel": {"both"}, "reminder_channel": {"sms"}, "change_channel": {"email"},
		"reminder_day_before": {"1"}, "reminder_hour": {"17"}, "reminder_two_hours": {"1"},
		"signature": {"Cabinet Dupont"},
	}
	if rec := c.do("POST", "/admin/settings/notifications", form); rec.Code != 303 {
		t.Fatalf("POST valide : %d\n%s", rec.Code, rec.Body)
	}
	mustContain(t, c.do("GET", "/admin/settings/notifications", nil), 200, `value="17"`, "Cabinet Dupont")

	// Saisie invalide : 422 et saisie conservée.
	form.Set("reminder_hour", "30")
	rec = c.do("POST", "/admin/settings/notifications", form)
	mustContain(t, rec, 422, "heure d&#39;envoi du rappel", `value="30"`)

	// Un secrétaire n'accède pas aux réglages.
	_, sec := newConfigAdmin(t, domain.RoleSecretary)
	if rec := sec.do("GET", "/admin/settings/notifications", nil); rec.Code != 403 {
		t.Errorf("secrétaire : %d", rec.Code)
	}
}
