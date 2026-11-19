package web_test

import (
	"net/url"
	"strings"
	"testing"

	"doctolibre/internal/domain"
)

// Fiche praticien sans prénom (créée depuis l'email) : le tableau de bord demande le nom ;
// l'invite disparaît une fois le profil enregistré. Le secrétariat ne la voit jamais.
func TestDashboardNeedProfile(t *testing.T) {
	t.Parallel()
	const want = "Indiquez votre nom tel que les patients le verront"
	_, c := newConfigAdmin(t, domain.RoleAdmin)
	mustContain(t, c.do("GET", "/admin/profile", nil), 200, `name="slug"`) // crée la fiche
	mustContain(t, c.do("GET", "/admin", nil), 200, want, `href="/admin/profile"`)
	rec := c.do("POST", "/admin/profile", url.Values{
		"first_name": {"Camille"}, "last_name": {"Martin"}, "slug": {"camille-martin"}, "color": {"#aa3366"},
	})
	if rec.Code != 303 {
		t.Fatalf("POST profil: %d", rec.Code)
	}
	if body := c.do("GET", "/admin", nil).Body.String(); strings.Contains(body, want) {
		t.Error("l'invite à indiquer son nom reste affichée après l'enregistrement du profil")
	}

	_, sec := newConfigAdmin(t, domain.RoleSecretary)
	if body := sec.do("GET", "/admin", nil).Body.String(); strings.Contains(body, want) {
		t.Error("le secrétariat voit l'invite à indiquer son nom")
	}
}
