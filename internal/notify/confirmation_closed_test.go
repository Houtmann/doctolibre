package notify_test

import (
	"strings"
	"testing"

	"doctolibre/internal/notify"
)

// Réservé à moins de MinCancelNotice du début : l'email ne promet plus « possibles jusqu'à… »
// et dit que le rendez-vous n'est plus modifiable en ligne.
func TestConfirmationDelaiDepasse(t *testing.T) {
	t.Parallel()
	tpl, err := notify.NewTemplates()
	if err != nil {
		t.Fatal(err)
	}
	d := apptData(true)
	d["cancel_notice"], d["cancel_closed"] = "", "24 h"
	e, err := tpl.Render("confirmation", d)
	if err != nil {
		t.Fatal(err)
	}
	golden(t, "confirmation_delai_depasse.email.txt", "Sujet : "+e.Subject+"\n\n"+e.Text)
	golden(t, "confirmation_delai_depasse.email.html", e.HTML)
	for _, body := range []string{e.Text, e.HTML} {
		if strings.Contains(body, "possibles jusqu") || !strings.Contains(body, "ne peut plus être déplacé ni annulé en ligne (délai de 24 h)") {
			t.Errorf("phrase de délai incorrecte :\n%s", body)
		}
	}
}
