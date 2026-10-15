package notify_test

import (
	"strings"
	"testing"

	"doctolibre/internal/notify"
)

func TestTemplatesOTP(t *testing.T) {
	t.Parallel()
	tpl, err := notify.NewTemplates()
	if err != nil {
		t.Fatal(err)
	}
	e, err := tpl.Render("otp", map[string]any{"code": "042913", "valid_for": "10 minutes", "practice_name": "Cabinet <Dupont> & fils"})
	if err != nil {
		t.Fatal(err)
	}
	if e.Subject != "Votre code de vérification : 042913" {
		t.Errorf("sujet = %q", e.Subject)
	}
	if !strings.Contains(e.Text, "042913") || !strings.Contains(e.Text, "Cabinet <Dupont> & fils") {
		t.Errorf("texte: %q", e.Text)
	}
	if !strings.Contains(e.HTML, "042913") || strings.Contains(e.HTML, "<Dupont>") || !strings.Contains(e.HTML, "&lt;Dupont&gt;") {
		t.Errorf("html non échappé ou code absent: %q", e.HTML)
	}
	if _, err := tpl.Render("otp", map[string]any{}); err == nil {
		t.Error("payload incomplet accepté")
	}
}
