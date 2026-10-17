package web_test

import (
	"context"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// advance avance l'horloge de d en gardant les sessions des clients vivantes
// (expiration glissante de 12 h) : une requête toutes les 6 h au plus.
func (e groupEnv) advance(d time.Duration) {
	for d > 0 {
		step := min(d, 6*time.Hour)
		e.clock.Advance(step)
		d -= step
		e.admin.do("GET", "/admin", nil)
		e.prac[0].do("GET", "/admin", nil)
	}
}

func (e groupEnv) delegate(t *testing.T, c *client, from, to string) int {
	t.Helper()
	return c.do("POST", "/admin/team/delegations", url.Values{
		"user": {e.pracUser[0].ID.String()}, "practitioner": {e.pracIDs[1].String()}, "from": {from}, "to": {to},
	}).Code
}

// probe retourne les statuts de plusieurs entrées qui visent l'agenda du praticien 2
// depuis la session du praticien 1 : page agenda, JSON, détail d'un RDV, absences.
func (e groupEnv) probe(t *testing.T) map[string]int {
	t.Helper()
	other := e.pracIDs[1].String()
	c := e.prac[0]
	return map[string]int{
		"agenda":   c.do("GET", "/admin/calendar?view=day&date=2026-10-15&practitioner="+other, nil).Code,
		"json":     c.do("GET", "/admin/agenda/events?start=2026-10-15&end=2026-10-16&practitioner="+other, nil).Code,
		"rdv":      c.do("GET", "/admin/appointments/"+e.appts[1].String(), nil).Code,
		"absences": c.do("GET", "/admin/absences?practitioner="+other, nil).Code,
		"tiers":    c.do("GET", "/admin/calendar?view=day&date=2026-10-15&practitioner="+e.pracIDs[2].String(), nil).Code,
	}
}

func wantProbe(t *testing.T, when string, got map[string]int, access bool) {
	t.Helper()
	want := 403
	if access {
		want = 200
	}
	for k, code := range got {
		w := want
		if k == "tiers" {
			w = 403 // une délégation ne s'étend jamais aux autres agendas
		}
		if code != w {
			t.Errorf("%s: %s = %d, attendu %d", when, k, code, w)
		}
	}
}

// Avant la période : 403 ; pendant : accès ; après : 403, avec la même session
// ouverte (rien n'est mis en cache) et aux frontières de jour d'Europe/Paris.
func TestDelegationWindowAppliesToEveryEntryPoint(t *testing.T) {
	t.Parallel()
	e := newGroupEnv(t) // 15/10/2026 11:00 Paris
	if code := e.delegate(t, e.admin, "2026-10-16", "2026-10-17"); code != 303 {
		t.Fatalf("création de la délégation: %d", code)
	}
	wantProbe(t, "avant", e.probe(t), false)

	// 23:59:59 Paris le 15 : toujours avant.
	e.advance(12*time.Hour + 59*time.Minute + 59*time.Second) // 09:00 UTC -> 21:59:59 UTC
	wantProbe(t, "veille 23:59:59", e.probe(t), false)
	e.advance(time.Second) // 00:00 Paris le 16
	wantProbe(t, "16/10 00:00", e.probe(t), true)

	// Dernier jour, 23:59:59 locale (21:59:59 UTC le 17) : encore ouvert.
	e.advance(47*time.Hour + 59*time.Minute + 59*time.Second)
	wantProbe(t, "17/10 23:59:59", e.probe(t), true)
	e.advance(time.Second)
	wantProbe(t, "18/10 00:00", e.probe(t), false)
}

// Le remplaçant agit sur l'agenda délégué, et son user_id figure dans l'audit ;
// la révocation coupe l'accès à la requête suivante.
func TestDelegateActsUnderOwnIdentityAndRevocationIsImmediate(t *testing.T) {
	t.Parallel()
	e := newGroupEnv(t)
	if code := e.delegate(t, e.admin, "2026-10-15", "2026-10-20"); code != 303 {
		t.Fatalf("création: %d", code)
	}
	foreign := e.appts[1]
	if rec := e.prac[0].do("POST", "/admin/appointments/"+foreign.String()+"/status", url.Values{"status": {"done"}}); rec.Code != 303 {
		t.Fatalf("statut par le remplaçant: %d", rec.Code)
	}
	var uid uuid.UUID
	if err := e.db.QueryRow(context.Background(),
		`SELECT user_id FROM audit_log WHERE entity_id = $1 AND action LIKE 'appointment%' ORDER BY id DESC LIMIT 1`, foreign).Scan(&uid); err != nil {
		t.Fatal(err)
	}
	if uid != e.pracUser[0].ID {
		t.Errorf("audit: user_id=%v, attendu le remplaçant %v", uid, e.pracUser[0].ID)
	}
	// Il garde ses propres droits et n'obtient pas les réglages du cabinet.
	if rec := e.prac[0].do("GET", "/admin/team", nil); rec.Code != 403 {
		t.Errorf("page équipe: %d", rec.Code)
	}

	var id string
	if err := e.db.QueryRow(context.Background(), `SELECT id::text FROM practitioner_delegation`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	// Un praticien ne révoque pas ; l'administrateur oui.
	if rec := e.prac[0].do("POST", "/admin/team/delegations/"+id+"/revoke", url.Values{}); rec.Code != 403 {
		t.Errorf("révocation par un praticien: %d", rec.Code)
	}
	wantProbe(t, "avant révocation", e.probe(t), true)
	if rec := e.admin.do("POST", "/admin/team/delegations/"+id+"/revoke", url.Values{}); rec.Code != 303 {
		t.Fatalf("révocation: %d", rec.Code)
	}
	wantProbe(t, "après révocation", e.probe(t), false)
}

// Seul l'administrateur gère les délégations ; la page équipe les liste.
func TestDelegationManagementIsAdminOnly(t *testing.T) {
	t.Parallel()
	e := newGroupEnv(t)
	for name, c := range map[string]*client{"praticien": e.prac[0], "secrétariat": e.sec} {
		if code := e.delegate(t, c, "2026-10-16", "2026-10-17"); code != 403 {
			t.Errorf("%s: création = %d", name, code)
		}
	}
	var n int
	if err := e.db.QueryRow(context.Background(), `SELECT count(*) FROM practitioner_delegation`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("délégations créées malgré le refus: %d, %v", n, err)
	}
	// Erreur de saisie : 422 et formulaire réaffiché.
	if code := e.delegate(t, e.admin, "2026-10-20", "2026-10-19"); code != 422 {
		t.Errorf("période inversée: %d", code)
	}
	if code := e.delegate(t, e.admin, "2026-10-16", "2026-10-17"); code != 303 {
		t.Fatalf("création: %d", code)
	}
	rec := e.admin.do("GET", "/admin/team", nil)
	if b := rec.Body.String(); rec.Code != 200 || !strings.Contains(b, "Remplaçants") || !strings.Contains(b, "p1@example.test : agenda de") {
		t.Errorf("page équipe: %d", rec.Code)
	}
}
