package notify_test

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"doctolibre/internal/notify"
	"doctolibre/internal/notify/sms"
)

var update = flag.Bool("update", false, "régénère les golden files")

// apptData : données réalistes (nom de cabinet de 30 caractères au plus, comme le
// service le tronque pour les SMS, et lien de gestion de 43 caractères de jeton).
func apptData(manage bool) map[string]any {
	d := map[string]any{
		"practice_name": "Cabinet de kinésithérapie Dup", "practice_address": "12 rue des Lilas, 69003 Lyon",
		"practice_phone": "04 72 00 00 00", "first_name": "Léa", "practitioner": "Camille Martin",
		"type_name": "Séance de kinésithérapie", "date": "jeudi 15 octobre", "date_short": "15/10", "time": "14h30",
		"manage_url": "", "signature": "Cabinet de kinésithérapie Dupont",
		"old_date": "mardi 13 octobre", "old_date_short": "13/10", "old_time": "9h00",
		"valid_until": "16h30", "offer_url": "https://rdv.cabinet-dupont.fr/w/AbCdEfGhIjKlMnOpQrStUvWxYz0123456789_-AbCde",
		"count": 10, "sessions": []string{"jeudi 15 octobre à 14h30", "lundi 19 octobre à 14h30", "jeudi 22 octobre à 14h30"},
	}
	if manage {
		d["manage_url"] = "https://rdv.cabinet-dupont.fr/r/AbCdEfGhIjKlMnOpQrStUvWxYz0123456789_-AbCde"
	}
	return d
}

func golden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if *update {
		if err := os.MkdirAll("testdata", 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o600); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path) //nolint:gosec // chemin construit par le test
	if err != nil {
		t.Fatalf("golden %s: %v (lancer avec -update)", name, err)
	}
	if string(want) != got {
		t.Errorf("%s différent du golden file (-update pour régénérer)\n--- obtenu ---\n%s", name, got)
	}
}

func TestAppointmentTemplatesGolden(t *testing.T) {
	t.Parallel()
	tpl, err := notify.NewTemplates()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"confirmation", "reminder", "cancellation", "reschedule", "series", "waitlist"} {
		for _, manage := range []bool{true, false} {
			variant := "avec_lien"
			if !manage {
				variant = "sans_lien"
			}
			t.Run(name+"_"+variant, func(t *testing.T) {
				t.Parallel()
				e, err := tpl.Render(name, apptData(manage))
				if err != nil {
					t.Fatal(err)
				}
				golden(t, name+"_"+variant+".email.txt", "Sujet : "+e.Subject+"\n\n"+e.Text)
				golden(t, name+"_"+variant+".email.html", e.HTML)
				body, err := notify.RenderSMS(name, apptData(manage))
				if err != nil {
					t.Fatal(err)
				}
				golden(t, name+"_"+variant+".sms.txt", body+"\n")
				if n, ucs2 := sms.Segments(body); n != 1 || ucs2 {
					t.Errorf("SMS %s (%d caractères) : %d segment(s), UCS-2=%v ; attendu 1 segment GSM-7\n%s", name, len([]rune(body)), n, ucs2, body)
				}
				if !strings.Contains(e.Text, "Léa") || !strings.Contains(e.HTML, "15 octobre") {
					t.Errorf("email %s : prénom ou date absents", name)
				}
				wantLink := manage && name != "series" && name != "waitlist"
				if wantLink != strings.Contains(e.Text, "/r/") {
					t.Errorf("email %s : lien de gestion présent=%v, attendu %v", name, !wantLink, wantLink)
				}
			})
		}
	}
}

func TestRenderSMSErrors(t *testing.T) {
	t.Parallel()
	if _, err := notify.RenderSMS("inconnu", apptData(false)); err == nil {
		t.Error("template inconnu accepté")
	}
	if _, err := notify.RenderSMS("confirmation", map[string]any{}); err == nil {
		t.Error("données incomplètes acceptées")
	}
}
