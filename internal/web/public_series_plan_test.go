package web_test

import (
	"context"
	"fmt"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"doctolibre/internal/domain"
)

var planLineRe = regexp.MustCompile(`name="line" value="([^"]+)"`)

// newSelfPlanEnv : la série de newSeriesEnv (10 prescrites, 3 posées, 7 restantes) ouverte à la
// planification par le patient.
func newSelfPlanEnv(t *testing.T) seriesEnv {
	t.Helper()
	s := newSeriesEnv(t)
	ctx := context.Background()
	if _, err := s.db.Exec(ctx, "UPDATE practice SET care_plan_self_scheduling = true, booking_horizon = '120 days' WHERE id = $1", s.pc.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(ctx, "UPDATE care_plan SET patient_self_schedule = true"); err != nil {
		t.Fatal(err)
	}
	return s
}

func (s seriesEnv) countSessions(t *testing.T) int {
	t.Helper()
	var n int
	if err := s.db.QueryRow(context.Background(), "SELECT count(*) FROM appointment WHERE care_plan_id IS NOT NULL AND status <> 'cancelled'").Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func planLines(t *testing.T, body string) []string {
	t.Helper()
	var out []string
	for _, m := range planLineRe.FindAllStringSubmatch(body, -1) {
		out = append(out, m[1])
	}
	return out
}

func TestSeriesPlanJourneyWithoutJS(t *testing.T) {
	t.Parallel()
	s := newSelfPlanEnv(t)
	c := newClient(t, s.h, "192.0.2.80")
	base := "/s/" + s.token

	rec := c.do("GET", base, nil)
	mustContain(t, rec, 200, "Planifier mes séances", base+"/plan", "Il vous reste 7 séances à planifier")

	rec = c.do("GET", base+"/plan", nil)
	mustContain(t, rec, 200, "Proposer des séances", `name="count"`, `value="7"`, "csrf_token")
	if rec.Header().Get("Cache-Control") != "no-store" || rec.Header().Get("Referrer-Policy") != "no-referrer" {
		t.Errorf("en-têtes : %v", rec.Header())
	}

	// 1. Proposition (POST classique, page complète).
	rec = c.do("POST", base+"/plan/propose", url.Values{"count": {"3"}})
	mustContain(t, rec, 200, "<!doctype html>", "Séances proposées", "Confirmer ces séances", "Créneau suivant", "Retirer")
	lines := planLines(t, rec.Body.String())
	if len(lines) != 3 {
		t.Fatalf("%d séances proposées, attendu 3 : %s", len(lines), rec.Body.String())
	}

	// 2. Ajustements : retirer une ligne, demander le créneau suivant (page entière sans JS)…
	rec = c.do("POST", base+"/plan/adjust", url.Values{"count": {"3"}, "line": lines, "remove": {"1"}})
	mustContain(t, rec, 200, "Séances proposées")
	if got := planLines(t, rec.Body.String()); len(got) != 2 || got[0] != lines[0] || got[1] != lines[2] {
		t.Errorf("après retrait : %v (avant %v)", got, lines)
	}
	rec = c.do("POST", base+"/plan/adjust", url.Values{"count": {"3"}, "line": lines, "next": {"0"}})
	mustContain(t, rec, 200, "Séances proposées")
	if got := planLines(t, rec.Body.String()); len(got) != 3 || got[0] == lines[0] && got[1] == lines[1] && got[2] == lines[2] {
		t.Errorf("« créneau suivant » sans effet : %v", got)
	}
	// … et la version htmx : le fragment seul.
	hx := c.doHX("POST", base+"/plan/adjust", url.Values{"count": {"3"}, "line": lines, "remove": {"0"}})
	mustContain(t, hx, 200, `id="plan-proposal"`, "Confirmer ces séances")
	if strings.Contains(hx.Body.String(), "<html") || !strings.Contains(hx.Header().Get("Vary"), "HX-Request") {
		t.Errorf("htmx : fragment attendu (Vary=%q)", hx.Header().Get("Vary"))
	}

	// 3. Confirmation : POST classique puis redirection vers la page de série.
	rec = c.do("POST", base+"/plan/confirm", url.Values{"count": {"3"}, "line": lines})
	if rec.Code != 303 || rec.Header().Get("Location") != base {
		t.Fatalf("confirmation : %d %q\n%s", rec.Code, rec.Header().Get("Location"), rec.Body.String())
	}
	if got := s.countSessions(t); got != 6 {
		t.Fatalf("%d séances, attendu 6", got)
	}
	rec = c.do("GET", base, nil)
	mustContain(t, rec, 200, "3 séance(s) planifiée(s)", "Il vous reste 4 séances à planifier")

	// 4. Rejeu (double clic, retour arrière) : aucune séance de plus au-delà du quota, et 404 neutre une fois la série complète.
	c2 := newClient(t, s.h, "192.0.2.81")
	rec = c2.do("POST", base+"/plan/propose", url.Values{"count": {"4"}})
	rest := planLines(t, rec.Body.String())
	if len(rest) != 4 {
		t.Fatalf("%d séances proposées pour 4 restantes\n%s", len(rest), rec.Body.String())
	}
	if rec = c2.do("POST", base+"/plan/confirm", url.Values{"line": rest}); rec.Code != 303 {
		t.Fatalf("confirmation du reste : %d", rec.Code)
	}
	if rec = c2.do("POST", base+"/plan/confirm", url.Values{"line": rest}); rec.Code != 404 {
		t.Errorf("rejeu sur une série complète : %d, attendu 404", rec.Code)
	}
	if got := s.countSessions(t); got != 10 {
		t.Errorf("%d séances, attendu 10", got)
	}
	mustContain(t, c2.do("GET", base, nil), 200, "Vos séances")
	if strings.Contains(c2.do("GET", base, nil).Body.String(), "Planifier mes séances") {
		t.Error("le bouton reste affiché une fois la série complète")
	}
}

func TestSeriesPlanClosedIsNeutral404(t *testing.T) {
	t.Parallel()
	for name, sql := range map[string]string{
		"réglage du cabinet": "UPDATE practice SET care_plan_self_scheduling = false",
		"case de la série":   "UPDATE care_plan SET patient_self_schedule = false",
		"série terminée":     "UPDATE care_plan SET status = 'completed'",
		"jeton révoqué":      "UPDATE care_plan SET token_version = token_version + 1, token_hash = NULL",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			s := newSelfPlanEnv(t)
			c := newClient(t, s.h, "192.0.2.82")
			base := "/s/" + s.token
			if _, err := s.db.Exec(context.Background(), sql); err != nil {
				t.Fatal(err)
			}
			for _, tc := range []struct{ method, path string }{
				{"GET", "/plan"}, {"POST", "/plan/propose"}, {"POST", "/plan/adjust"}, {"POST", "/plan/confirm"},
			} {
				var form url.Values
				if tc.method == "POST" {
					form = url.Values{"count": {"2"}, "line": {"2026-11-03T08:00:00Z"}}
				}
				if rec := c.do(tc.method, base+tc.path, form); rec.Code != 404 {
					t.Errorf("%s %s : %d, attendu 404", tc.method, tc.path, rec.Code)
				}
			}
			if got := s.countSessions(t); got != 3 {
				t.Errorf("%d séances, attendu 3 : rien ne doit être créé", got)
			}
			if name != "jeton révoqué" && strings.Contains(c.do("GET", base, nil).Body.String(), "Planifier mes séances") {
				t.Error("bouton affiché alors que la planification est fermée")
			}
		})
	}
}

func TestSeriesPlanRequiresCSRF(t *testing.T) {
	t.Parallel()
	s := newSelfPlanEnv(t)
	c := newClient(t, s.h, "192.0.2.83")
	for _, p := range []string{"/plan/propose", "/plan/adjust", "/plan/confirm"} {
		if rec := c.doNoCSRF("POST", "/s/"+s.token+p); rec.Code != 403 {
			t.Errorf("POST %s sans CSRF : %d, attendu 403", p, rec.Code)
		}
	}
	if got := s.countSessions(t); got != 3 {
		t.Errorf("%d séances", got)
	}
}

func TestSeriesPlanProposalsAreCappedPerSeries(t *testing.T) {
	t.Parallel()
	s := newSelfPlanEnv(t)
	base := "/s/" + s.token
	// Le plafond est par série (pas par IP) : on change d'IP pour ne pas buter sur le limiteur « manage ».
	last := 0
	for i := range 25 {
		c := newClient(t, s.h, fmt.Sprintf("198.51.100.%d", i+1))
		last = c.do("POST", base+"/plan/propose", url.Values{"count": {"1"}}).Code
		if i < 20 && last != 200 {
			t.Fatalf("proposition %d refusée : %d", i+1, last)
		}
		if i >= 20 && last != 429 {
			t.Fatalf("proposition %d : %d, attendu 429 après 20 par heure", i+1, last)
		}
	}
}

func TestSettingsSelfSchedulingCheckbox(t *testing.T) {
	t.Parallel()
	_, c := newConfigAdmin(t, domain.RoleAdmin)
	mustContain(t, c.do("GET", "/admin/settings", nil), 200, `name="care_plan_self_scheduling"`)
	form := url.Values{
		"name": {"Cabinet Étoile"}, "booking_notice_hours": {"6"}, "cancel_notice_hours": {"48"}, "horizon_days": {"45"},
		"first_name": {"Camille"}, "last_name": {"Martin"}, "slug": {"camille-martin"}, "color": {"#2563eb"},
		"care_plan_self_scheduling": {"1"},
	}
	if rec := c.do("POST", "/admin/settings", form); rec.Code != 303 {
		t.Fatalf("POST : %d", rec.Code)
	}
	if !strings.Contains(c.do("GET", "/admin/settings", nil).Body.String(), `name="care_plan_self_scheduling" value="1" checked`) {
		t.Error("case non cochée après enregistrement")
	}
	delete(form, "care_plan_self_scheduling")
	c.do("POST", "/admin/settings", form)
	if strings.Contains(c.do("GET", "/admin/settings", nil).Body.String(), `name="care_plan_self_scheduling" value="1" checked`) {
		t.Error("case encore cochée")
	}
}

func TestCarePlanFormOffersSelfScheduling(t *testing.T) {
	t.Parallel()
	e := newPlanEnv(t, domain.RoleAdmin)
	ctx := context.Background()
	form := "/admin/patients/" + e.patient.ID.String() + "/care-plans"

	// Réglage du cabinet désactivé : pas de case, et la série ne peut pas se passer de séance.
	if body := e.c.do("GET", form+"/new", nil).Body.String(); strings.Contains(body, `name="self_schedule"`) {
		t.Error("case proposée alors que le réglage du cabinet est désactivé")
	}
	in := e.criteria()
	in.Set("self_schedule", "1")
	mustContain(t, e.c.do("POST", form, in), 422, "pas activée")

	if _, err := e.db.Exec(ctx, "UPDATE practice SET care_plan_self_scheduling = true WHERE id = $1", e.actor.PracticeID); err != nil {
		t.Fatal(err)
	}
	mustContain(t, e.c.do("GET", form+"/new", nil), 200, `name="self_schedule"`, "Laisser le patient planifier", "Créer la série sans séance")
	// Sans la case : toujours au moins une séance.
	mustContain(t, e.c.do("POST", form, e.criteria()), 422, "Planifiez au moins une séance")

	// Avec la case, 0 séance posée : la série est créée et le lien est affiché au cabinet.
	rec := e.c.do("POST", form, in)
	if rec.Code != 303 {
		t.Fatalf("création : %d\n%s", rec.Code, rec.Body.String())
	}
	mustContain(t, e.c.do("GET", rec.Header().Get("Location"), nil), 200, "Le patient planifie les séances restantes depuis son lien de série", "/s/")
	var self bool
	var n int
	if err := e.db.QueryRow(ctx, "SELECT patient_self_schedule, (SELECT count(*) FROM appointment) FROM care_plan").Scan(&self, &n); err != nil || !self || n != 0 {
		t.Errorf("patient_self_schedule=%v, %d rendez-vous (%v)", self, n, err)
	}
}

func TestSeriesPlanAdjustKeepsPracticeLocalTime(t *testing.T) {
	t.Parallel()
	s := newSelfPlanEnv(t)
	c := newClient(t, s.h, "192.0.2.84")
	base := "/s/" + s.token
	rec := c.do("POST", base+"/plan/propose", url.Values{"count": {"3"}})
	mustContain(t, rec, 200, "09h00")
	lines := planLines(t, rec.Body.String())
	if len(lines) != 3 || !strings.HasSuffix(lines[0], "T07:00:00Z") && !strings.HasSuffix(lines[0], "T08:00:00Z") {
		t.Fatalf("lignes : %v", lines)
	}
	check := func(name string, rec *httptest.ResponseRecorder) {
		t.Helper()
		body := rec.Body.String()
		if rec.Code != 200 || !strings.Contains(body, "09h00") || strings.Contains(body, "07h00") || strings.Contains(body, "08h00") {
			t.Errorf("%s : heure locale attendue (09h00, pas l'heure UTC)\n%s", name, body)
		}
	}
	form := func(k, v string) url.Values { return url.Values{"count": {"3"}, "line": lines, k: {v}} }
	check("sans JS, retirer", c.do("POST", base+"/plan/adjust", form("remove", "2")))
	check("sans JS, suivant", c.do("POST", base+"/plan/adjust", form("next", "0")))
	check("htmx, retirer", c.doHX("POST", base+"/plan/adjust", form("remove", "2")))
	check("htmx, suivant", c.doHX("POST", base+"/plan/adjust", form("next", "0")))
}

func TestSeriesPlanNoSlotAlertPrecedesForm(t *testing.T) {
	t.Parallel()
	s := newSelfPlanEnv(t)
	if _, err := s.db.Exec(context.Background(), "UPDATE practice SET booking_horizon = '1 day' WHERE id = $1", s.pc.ID); err != nil {
		t.Fatal(err)
	}
	c := newClient(t, s.h, "192.0.2.85")
	body := c.do("POST", "/s/"+s.token+"/plan/propose", url.Values{"count": {"2"}}).Body.String()
	alert, button := strings.Index(body, "Aucun créneau disponible"), strings.Index(body, "Proposer des séances</button>")
	if alert < 0 || button < 0 || alert > button {
		t.Errorf("l'alerte (%d) doit précéder le formulaire (%d)", alert, button)
	}
	if strings.Count(body, "Aucun créneau disponible") != 1 {
		t.Error("alerte dupliquée")
	}
}
