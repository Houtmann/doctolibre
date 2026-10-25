package notify_test

import (
	"strings"
	"testing"

	"doctolibre/internal/notify"
)

func TestTemplatesOTPWaitlist(t *testing.T) {
	t.Parallel()
	tpl, err := notify.NewTemplates()
	if err != nil {
		t.Fatal(err)
	}
	e, err := tpl.Render("otp_waitlist", map[string]any{"code": "042913", "valid_for": "10 minutes", "practice_name": "Cabinet Dupont"})
	if err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{"texte": e.Text, "html": e.HTML} {
		if !strings.Contains(body, "042913") || !strings.Contains(body, "liste d") {
			t.Errorf("%s : code ou mention de la liste d'attente absents: %q", name, body)
		}
		if strings.Contains(body, "rendez-vous") {
			t.Errorf("%s : l'OTP de liste d'attente parle de rendez-vous: %q", name, body)
		}
	}
	if strings.Contains(e.Subject, "rendez-vous") {
		t.Errorf("sujet: %q", e.Subject)
	}
}
