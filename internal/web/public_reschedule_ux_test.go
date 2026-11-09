package web_test

import (
	"context"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestPatientRescheduleShowsCurrentAppointmentAndNotice(t *testing.T) {
	t.Parallel()
	e := newPublicEnv(t)
	c := newClient(t, e.h, "192.0.2.64")
	manage := e.bookDirect(t, tuesday9h, "Alice", "06 11 11 11 11", "+33611111111")

	// Le délai est annoncé tant que le rendez-vous est modifiable.
	mustContain(t, c.do("GET", manage, nil), 200, "Déplacement et annulation possibles en ligne jusqu'à 24 h avant le rendez-vous.")

	rec := c.do("GET", manage+"/reschedule", nil)
	mustContain(t, rec, 200, "Rendez-vous actuel", "mardi 20 octobre 2026 à 09h00")

	// Hors délai : plus d'annonce du délai (le texte « ne peut plus » prend le relais).
	e.clock.Advance(tuesday9h.Sub(e.clock.Now()) - 23*time.Hour)
	if strings.Contains(c.do("GET", manage, nil).Body.String(), "possibles en ligne jusqu'à") {
		t.Error("délai annoncé alors que le rendez-vous n'est plus modifiable")
	}
}

func TestPatientRescheduleOpensOnAppointmentWeek(t *testing.T) {
	t.Parallel()
	e := newPublicEnv(t)
	c := newClient(t, e.h, "192.0.2.65")
	// Mardi 10 novembre 2026, 9 h à Paris (CET, après le changement d'heure du 25 octobre).
	far := time.Date(2026, 11, 10, 8, 0, 0, 0, time.UTC)
	manage := e.bookDirect(t, far, "Alice", "06 11 11 11 11", "+33611111111")

	// Sans ?week= : la semaine du rendez-vous, pas la semaine courante.
	rec := c.do("GET", manage+"/reschedule", nil)
	mustContain(t, rec, 200, "Rendez-vous actuel", "mardi 10 novembre 2026 à 09h00", "Semaine précédente", "start=2026-11-10T08%3A30%3A00Z")
	if strings.Contains(rec.Body.String(), "mardi 20 octobre 2026") {
		t.Error("la grille s'ouvre sur la semaine courante")
	}
	if strings.Contains(rec.Body.String(), "start=2026-11-10T08%3A00%3A00Z") {
		t.Error("le créneau actuel est proposé")
	}

	// ?week= explicite : respecté ; semaine passée : ramenée à la semaine courante (minMon).
	mustContain(t, c.do("GET", manage+"/reschedule?week=2026-43", nil), 200, "mardi 20 octobre 2026")
	rec = c.do("GET", manage+"/reschedule?week=2026-30", nil)
	mustContain(t, rec, 200, "12 octobre")
	if strings.Contains(rec.Body.String(), "Semaine précédente") {
		t.Error("une semaine passée est proposée")
	}

	// Horizon réduit après coup : la semaine du rendez-vous est bornée à la dernière ouverte (maxMon).
	if _, err := e.db.Exec(context.Background(), "UPDATE practice SET booking_horizon = interval '14 days' WHERE id = $1", e.pc.ID); err != nil {
		t.Fatal(err)
	}
	rec = c.do("GET", manage+"/reschedule", nil)
	if rec.Code != 200 || strings.Contains(rec.Body.String(), "mardi 10 novembre 2026 à 09h30") {
		t.Errorf("semaine hors horizon: code %d, grille %q", rec.Code, rec.Body.String())
	}
}

func TestPatientCancelledRebookURL(t *testing.T) {
	t.Parallel()
	e := newPublicEnv(t)
	c := newClient(t, e.h, "192.0.2.66")
	manage := e.bookDirect(t, tuesday9h, "Alice", "06 11 11 11 11", "+33611111111")
	if rec := c.do("POST", manage+"/cancel", url.Values{}); rec.Code != 303 {
		t.Fatalf("annulation: %d", rec.Code)
	}
	// Annulation récente, motif actif et réservable en ligne : reprise directe (T098).
	mustContain(t, c.do("GET", manage, nil), 200, "Reprendre rendez-vous", `href="`+manage+`/rebook"`)

	// Motif retiré de la réservation en ligne, puis désactivé : page du praticien.
	for _, set := range []string{"online_bookable = false", "active = false"} {
		if _, err := e.db.Exec(context.Background(), "UPDATE appointment_type SET online_bookable = true, active = true WHERE id = $1", e.typ.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := e.db.Exec(context.Background(), "UPDATE appointment_type SET "+set+" WHERE id = $1", e.typ.ID); err != nil {
			t.Fatal(err)
		}
		rec := c.do("GET", manage, nil)
		mustContain(t, rec, 200, `href="`+e.base()+`"`)
		if strings.Contains(rec.Body.String(), "/book?type=") {
			t.Errorf("%s : lien vers la grille alors que le motif n'est plus réservable", set)
		}
	}
}
