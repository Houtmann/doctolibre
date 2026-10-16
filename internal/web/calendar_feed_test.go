package web_test

import (
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"doctolibre/internal/domain"
)

var feedURLRe = regexp.MustCompile(`value="[^"]*(/ics/[A-Za-z0-9_-]+\.ics)"`)

func TestCalendarFeedFlow(t *testing.T) {
	t.Parallel()
	e := newCalendarEnv(t, domain.RoleAdmin)
	anon := newClient(t, e.h, "192.0.2.30")

	for _, p := range []string{"/ics/inconnu.ics", "/ics/inconnu", "/ics/.ics"} {
		if rec := anon.do("GET", p, nil); rec.Code != 404 {
			t.Errorf("GET %s: %d", p, rec.Code)
		}
	}
	// Réglages : abonnement proposé mais inactif.
	mustContain(t, e.c.do("GET", "/admin/settings", nil), 200, `Activer l'abonnement`)

	rec := e.c.do("POST", "/admin/settings/feed", url.Values{})
	m := feedURLRe.FindStringSubmatch(rec.Body.String())
	if rec.Code != 200 || m == nil || rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("génération: %d %q", rec.Code, rec.Header().Get("Cache-Control"))
	}
	feedPath := m[1]
	// Le jeton n'est plus affiché ensuite.
	if rec := e.c.do("GET", "/admin/settings", nil); strings.Contains(rec.Body.String(), feedPath) {
		t.Error("jeton réaffiché")
	}

	if rec := e.c.do("POST", "/admin/appointments", e.form("2026-10-20", "10:00")); rec.Code != 303 {
		t.Fatalf("création RDV: %d", rec.Code)
	}

	rec = anon.do("GET", feedPath, nil)
	if rec.Code != 200 || rec.Header().Get("Content-Type") != "text/calendar; charset=utf-8" ||
		rec.Header().Get("Cache-Control") != "private, max-age=300" || rec.Header().Get("ETag") == "" {
		t.Fatalf("flux: %d %v", rec.Code, rec.Header())
	}
	body := rec.Body.String()
	for _, want := range []string{"BEGIN:VCALENDAR\r\n", "SUMMARY:RDV L.D. - ", "DTSTART:20261020T080000Z\r\n"} {
		if !strings.Contains(body, want) {
			t.Errorf("fragment manquant %q dans\n%s", want, body)
		}
	}
	if strings.Contains(body, "Durand") || strings.Contains(body, "0612") {
		t.Error("donnée patient dans le flux")
	}
	if strings.Contains(body, "VALARM") {
		t.Error("le flux praticien ne doit contenir aucune alarme")
	}
	etag := rec.Header().Get("ETag")

	// Requête conditionnelle.
	cond := httptest.NewRequest("GET", feedPath, nil)
	cond.Header.Set("If-None-Match", etag)
	cond.RemoteAddr = "192.0.2.30:4321"
	rr := httptest.NewRecorder()
	e.h.ServeHTTP(rr, cond)
	if rr.Code != 304 || rr.Body.Len() != 0 {
		t.Errorf("If-None-Match: %d", rr.Code)
	}

	// Option nom complet.
	if rec := e.c.do("POST", "/admin/settings/feed/options", url.Values{"full_names": {"1"}}); rec.Code != 303 {
		t.Fatalf("option: %d", rec.Code)
	}
	if rec := anon.do("GET", feedPath, nil); !strings.Contains(rec.Body.String(), "SUMMARY:RDV Léa Durand") {
		t.Errorf("nom complet absent:\n%s", rec.Body.String())
	}

	// Régénération : l'ancien lien meurt.
	rec = e.c.do("POST", "/admin/settings/feed", url.Values{})
	m = feedURLRe.FindStringSubmatch(rec.Body.String())
	if m == nil || m[1] == feedPath {
		t.Fatal("pas de nouveau lien")
	}
	if rec := anon.do("GET", feedPath, nil); rec.Code != 404 {
		t.Errorf("ancien lien: %d", rec.Code)
	}
	if rec := anon.do("GET", m[1], nil); rec.Code != 200 {
		t.Errorf("nouveau lien: %d", rec.Code)
	}

	// Révocation.
	if rec := e.c.do("POST", "/admin/settings/feed/revoke", url.Values{}); rec.Code != 303 {
		t.Fatalf("révocation: %d", rec.Code)
	}
	if rec := anon.do("GET", m[1], nil); rec.Code != 404 {
		t.Errorf("lien révoqué: %d", rec.Code)
	}
}

func TestCalendarFeedAccessControl(t *testing.T) {
	t.Parallel()
	e := newCalendarEnv(t, domain.RoleAdmin)
	anon := newClient(t, e.h, "192.0.2.31")
	for _, p := range []string{"/admin/settings/feed", "/admin/settings/feed/options", "/admin/settings/feed/revoke"} {
		if rec := anon.do("POST", p, url.Values{}); rec.Code != 303 {
			t.Errorf("POST %s sans session: %d", p, rec.Code)
		}
		if rec := e.c.doNoCSRF("POST", p); rec.Code != 403 {
			t.Errorf("POST %s sans CSRF: %d", p, rec.Code)
		}
	}
	sec := newCalendarEnv(t, domain.RoleSecretary)
	if rec := sec.c.do("POST", "/admin/settings/feed", url.Values{}); rec.Code != 403 {
		t.Errorf("secrétaire: %d", rec.Code)
	}
}
