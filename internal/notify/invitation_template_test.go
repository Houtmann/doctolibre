package notify_test

import (
	"strings"
	"testing"

	"doctolibre/internal/notify"
)

func TestTemplatesUserInvitation(t *testing.T) {
	t.Parallel()
	tpl, err := notify.NewTemplates()
	if err != nil {
		t.Fatal(err)
	}
	e, err := tpl.Render("user_invitation", map[string]any{
		"accept_url": "https://x.test/admin/invitation?token=a&b=1", "valid_for": "48 heures",
		"practice_name": "Cabinet Étoile", "role_label": "Praticien",
	})
	if err != nil {
		t.Fatal(err)
	}
	if e.Subject != "Invitation à rejoindre Cabinet Étoile sur Doctolibre" {
		t.Errorf("sujet = %q", e.Subject)
	}
	if !strings.Contains(e.Text, "https://x.test/admin/invitation?token=a&b=1") || !strings.Contains(e.Text, "48 heures") {
		t.Errorf("texte: %q", e.Text)
	}
	if !strings.Contains(e.HTML, `href="https://x.test/admin/invitation?token=a&amp;b=1"`) {
		t.Errorf("html: %q", e.HTML)
	}
	if _, err := tpl.Render("user_invitation", nil); err == nil {
		t.Error("payload incomplet accepté")
	}
}
