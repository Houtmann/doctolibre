package web_test

import (
	"context"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"doctolibre/internal/store/sqlcgen"
)

// Parcours public d'une visite à domicile : adresse demandée, zone appliquée,
// adresse conservée jusqu'à la vérification du code, trajet dans le RDV créé.
func TestPublicHomeVisitJourney(t *testing.T) {
	t.Parallel()
	e := newPublicEnv(t)
	ctx := context.Background()
	q := sqlcgen.New(e.db)
	mins := func(m int) pgtype.Interval {
		return pgtype.Interval{Microseconds: int64(m) * time.Minute.Microseconds(), Valid: true}
	}
	home, err := q.CreateAppointmentType(ctx, sqlcgen.CreateAppointmentTypeParams{
		PracticeID: e.pc.ID, Name: "Séance à domicile", Duration: mins(30), BufferBefore: mins(0), BufferAfter: mins(0),
		OnlineBookable: true, AtHome: true, Color: "#b45309",
	})
	if err != nil {
		t.Fatal(err)
	}
	c := newClient(t, e.h, "192.0.2.60")

	// Option désactivée : le motif n'est pas proposé.
	rec := c.do("GET", e.base(), nil)
	if strings.Contains(rec.Body.String(), "domicile") {
		t.Fatal("motif à domicile proposé alors que l'option est désactivée")
	}

	if err := q.SetPracticeHomeVisit(ctx, sqlcgen.SetPracticeHomeVisitParams{
		ID: e.pc.ID, HomeVisitOnline: true, HomeVisitZones: []string{"75011"},
	}); err != nil {
		t.Fatal(err)
	}
	mustContain(t, c.do("GET", e.base(), nil), 200, "Séance à domicile")

	const start = "2026-10-20T07:00:00Z"
	tid := home.ID.String()
	target := e.base() + "/book/details?type=" + tid + "&start=" + url.QueryEscape(start)
	mustContain(t, c.do("GET", target, nil), 200, `name="address"`, "Adresse de la visite")

	// Hors zone, puis sans adresse : refusés, aucun code envoyé.
	f := detailsForm(tid, start)
	f.Set("address", "5 quai de la Saône, 69001 Lyon")
	mustContain(t, c.do("POST", target, f), 422, "hors de la zone desservie", "5 quai de la Saône")
	f.Del("address")
	mustContain(t, c.do("POST", target, f), 422, "adresse de la visite à domicile est requise")
	if e.otp.count() != 0 {
		t.Fatal("code envoyé pour une adresse refusée")
	}

	// En zone : la page de vérification conserve l'adresse (champ caché), puis le RDV est créé.
	f.Set("address", "12 rue de la Paix, 75011 Paris")
	rec = c.do("POST", target, f)
	mustContain(t, rec, 200, `name="code"`, `name="address" value="12 rue de la Paix, 75011 Paris"`)
	m := challengeRe.FindStringSubmatch(rec.Body.String())
	if m == nil {
		t.Fatal("challenge_id absent")
	}
	f.Set("challenge_id", m[1])
	f.Set("code", e.otp.code("+33611111111"))
	rec = c.do("POST", e.base()+"/book/confirm", f)
	if rec.Code != 303 {
		t.Fatalf("confirmation: %d\n%s", rec.Code, rec.Body.String())
	}
	manage := rec.Header().Get("Location")
	mustContain(t, c.do("GET", manage, nil), 200, "Visite à domicile", "12 rue de la Paix, 75011 Paris")
	mustContain(t, c.do("GET", manage+"/ics", nil), 200, "LOCATION:12 rue de la Paix")

	var before int
	var addr string
	if err := e.db.QueryRow(ctx, `SELECT EXTRACT(EPOCH FROM buffer_before)::int, address FROM appointment`).Scan(&before, &addr); err != nil {
		t.Fatal(err)
	}
	if before != 900 || addr != "12 rue de la Paix, 75011 Paris" {
		t.Errorf("buffer_before = %v, address = %q", before, addr)
	}
}
