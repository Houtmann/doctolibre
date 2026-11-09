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
		"manage_url": "", "series_url": "", "rebook_url": "", "cancel_notice": "24 h", "instructions": "", "signature": "Cabinet de kinésithérapie Dupont",
		"old_date": "mardi 13 octobre", "old_date_short": "13/10", "old_time": "9h00",
		"reason": "Praticien souffrant", "window": "du 12 novembre au 12 décembre, lundi, mercredi", "leave_url": "", "valid_until": "16h30", "offer_url": "https://rdv.cabinet-dupont.fr/w/AbCdEfGhIjKlMnOpQrStUvWxYz0123456789_-AbCde",
		"count": 10, "sessions": []string{"jeudi 15 octobre à 14h30", "lundi 19 octobre à 14h30", "jeudi 22 octobre à 14h30"},
	}
	if manage {
		d["manage_url"] = "https://rdv.cabinet-dupont.fr/r/AbCdEfGhIjKlMnOpQrStUvWxYz0123456789_-AbCde"
		d["rebook_url"] = "https://rdv.cabinet-dupont.fr/p/cabinet-dupont/camille-martin/book?type=3f2b8c1e-5a7d-4e90-b1c2-8d4f6a9e0b13"
		d["leave_url"] = "https://rdv.cabinet-dupont.fr/wl/AbCdEfGhIjKlMnOpQrStUvWxYz0123456789_-AbCde"
		d["series_url"] = "https://rdv.cabinet-dupont.fr/s/AbCdEfGhIjKlMnOpQrStUvWxYz0123456789_-AbCde"
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
	for _, name := range []string{"confirmation", "reminder", "cancellation", "reschedule", "series", "waitlist", "patient_cancellation", "waitlist_joined"} {
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
				if !strings.Contains(e.Text, "Léa") || (name != "waitlist_joined" && !strings.Contains(e.HTML, "15 octobre")) {
					t.Errorf("email %s : prénom ou date absents", name)
				}
				wantLink := manage && name != "series" && name != "waitlist" && name != "patient_cancellation" && name != "waitlist_joined"
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

// TestInstructionsBlock : le bloc « À prévoir » n'apparaît que si les consignes sont renseignées,
// est échappé en HTML, et n'entre jamais dans les SMS.
func TestInstructionsBlock(t *testing.T) {
	t.Parallel()
	tpl, err := notify.NewTemplates()
	if err != nil {
		t.Fatal(err)
	}
	const instr = "Apporter l'ordonnance\nTenue souple <script>alert(1)</script>"
	for _, name := range []string{"confirmation", "reminder", "reschedule", "series"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			d := apptData(true)
			d["instructions"] = instr
			e, err := tpl.Render(name, d)
			if err != nil {
				t.Fatal(err)
			}
			golden(t, name+"_consignes.email.txt", "Sujet : "+e.Subject+"\n\n"+e.Text)
			golden(t, name+"_consignes.email.html", e.HTML)
			if !strings.Contains(e.Text, "À prévoir :\nApporter l'ordonnance\nTenue souple") || !strings.Contains(e.HTML, "À prévoir") {
				t.Errorf("bloc « À prévoir » absent : %s", e.Text)
			}
			if strings.Contains(e.HTML, "<script>") {
				t.Errorf("consignes non échappées dans le HTML : %s", e.HTML)
			}
			plain, err := tpl.Render(name, apptData(true))
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(plain.Text, "À prévoir") || strings.Contains(plain.HTML, "À prévoir") {
				t.Error("bloc « À prévoir » présent alors que les consignes sont vides")
			}
			withSMS, err := notify.RenderSMS(name, d)
			if err != nil {
				t.Fatal(err)
			}
			withoutSMS, err := notify.RenderSMS(name, apptData(true))
			if err != nil {
				t.Fatal(err)
			}
			if withSMS != withoutSMS {
				t.Errorf("les consignes changent le SMS :\n%s\n%s", withSMS, withoutSMS)
			}
		})
	}
}
