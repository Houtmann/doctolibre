package notify_test

import (
	"strings"
	"testing"

	"doctolibre/internal/notify"
)

// Le motif d'annulation est facultatif : sans motif, aucune mention « Motif ».
func TestCancellationReasonIsOptional(t *testing.T) {
	t.Parallel()
	tpl, err := notify.NewTemplates()
	if err != nil {
		t.Fatal(err)
	}
	d := apptData(true)
	d["reason"] = ""
	e, err := tpl.Render("cancellation", d)
	if err != nil {
		t.Fatal(err)
	}
	body, err := notify.RenderSMS("cancellation", d)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{e.Text, e.HTML, body} {
		if strings.Contains(s, "Motif") || strings.Contains(s, "()") {
			t.Errorf("mention de motif sans motif : %s", s)
		}
	}
	withReason, err := tpl.Render("cancellation", apptData(true))
	if err != nil || !strings.Contains(withReason.Text, "Motif : Praticien souffrant") || !strings.Contains(withReason.HTML, "Motif : Praticien souffrant") {
		t.Errorf("motif absent de l'email : %v\n%s", err, withReason.Text)
	}
}
