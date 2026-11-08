package service_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"doctolibre/internal/domain"
	"doctolibre/internal/service"
	"doctolibre/internal/testutil"
)

type statsEnv struct {
	db       *pgxpool.Pool
	svc      *service.Stats
	practice uuid.UUID
	a, b, c  uuid.UUID // praticiens A (titulaire), B (remplaçant), C (autre)
	userA    uuid.UUID // compte lié à A
	userB    uuid.UUID // compte lié à B
	userSec  uuid.UUID // secrétariat, sans fiche praticien
	userAdm  uuid.UUID // admin sans fiche praticien
	cab      uuid.UUID // motif au cabinet
	home     uuid.UUID // motif à domicile
	patient  uuid.UUID
	n        int // compteur de créneaux
	clock    *testutil.FixedClock
}

func (e *statsEnv) exec(t *testing.T, sql string, args ...any) {
	t.Helper()
	if _, err := e.db.Exec(context.Background(), sql, args...); err != nil {
		t.Fatalf("%s: %v", sql, err)
	}
}

func (e *statsEnv) user(t *testing.T, practice uuid.UUID, practitioner *uuid.UUID, role string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	e.exec(t, `INSERT INTO app_user (id, practice_id, practitioner_id, email, password_hash, role) VALUES ($1, $2, $3, $4, 'x', $5)`,
		id, practice, practitioner, id.String()+"@example.test", role)
	return id
}

// appt insère un RDV de 30 min qui commence à start.
func (e *statsEnv) appt(t *testing.T, practice, practitioner, typ uuid.UUID, start time.Time, status string, by *uuid.UUID) {
	t.Helper()
	e.exec(t, `INSERT INTO appointment (practice_id, practitioner_id, patient_id, appointment_type_id, during, during_blocked, status, source, status_changed_by)
		VALUES ($1, $2, $3, $4, tstzrange($5::timestamptz, $5::timestamptz + interval '30 minutes', '[)'), tstzrange($5::timestamptz, $5::timestamptz + interval '30 minutes', '[)'), $6, 'staff', $7)`,
		practice, practitioner, e.patient, typ, start, status, by)
}

// slot retourne un créneau libre d'octobre 2026 (heure de Paris, mercredi 14 et suivants).
func (e *statsEnv) slot() time.Time {
	e.n++
	loc, _ := time.LoadLocation("Europe/Paris")
	return time.Date(2026, 10, 14, 6, 0, 0, 0, loc).Add(time.Duration(e.n) * time.Hour)
}

func newStatsEnv(t *testing.T) *statsEnv {
	t.Helper()
	db := testutil.DB(t)
	p := testutil.NewPractice(t, db)
	e := &statsEnv{db: db, practice: p.ID, clock: testutil.NewFixedClock(time.Date(2026, 11, 8, 10, 0, 0, 0, time.UTC))}
	e.svc = &service.Stats{Pool: db, Clock: e.clock}
	pr := func(first string) uuid.UUID {
		x := testutil.NewPractitioner(t, db, p.ID)
		e.exec(t, `UPDATE practitioner SET first_name = $2 WHERE id = $1`, x.ID, first)
		return x.ID
	}
	e.a, e.b, e.c = pr("Alice"), pr("Bruno"), pr("Chloé")
	e.userA = e.user(t, p.ID, &e.a, domain.RolePractitioner)
	e.userB = e.user(t, p.ID, &e.b, domain.RolePractitioner)
	e.userSec = e.user(t, p.ID, nil, domain.RoleSecretary)
	e.userAdm = e.user(t, p.ID, nil, domain.RoleAdmin)
	e.cab = testutil.NewAppointmentType(t, db, p.ID).ID
	e.home = testutil.NewAppointmentType(t, db, p.ID).ID
	e.exec(t, `UPDATE appointment_type SET at_home = true, name = 'Domicile' WHERE id = $1`, e.home)
	e.patient = testutil.NewPatient(t, db, p.ID).ID
	e.exec(t, `UPDATE patient SET first_name = 'Zorglub', last_name = 'Patientnom' WHERE id = $1`, e.patient)
	return e
}

func (e *statsEnv) admin() service.Actor {
	return service.Actor{UserID: e.userAdm, PracticeID: e.practice, Role: domain.RoleAdmin}
}

func (e *statsEnv) practitioner(user, prac uuid.UUID) service.Actor {
	return service.Actor{UserID: user, PracticeID: e.practice, Role: domain.RolePractitioner, PractitionerID: prac}
}

// find retourne la ligne (titulaire, réalisateur, motif) du rapport.
func find(t *testing.T, r service.StatsReport, titulaire, performer, typ uuid.UUID) service.StatsLine {
	t.Helper()
	for _, g := range r.Groups {
		if g.ID != titulaire {
			continue
		}
		for _, p := range g.Performers {
			if p.ID != performer {
				continue
			}
			for _, l := range p.Lines {
				if l.TypeID == typ {
					return l
				}
			}
		}
	}
	return service.StatsLine{}
}

func TestStatsCountsAndAttribution(t *testing.T) {
	t.Parallel()
	e := newStatsEnv(t)
	for range 2 {
		e.appt(t, e.practice, e.a, e.cab, e.slot(), "done", nil) // avant T072 : status_changed_by NULL
	}
	e.appt(t, e.practice, e.a, e.cab, e.slot(), "done", &e.userA)   // pointé par le titulaire
	e.appt(t, e.practice, e.a, e.cab, e.slot(), "done", &e.userSec) // compte sans fiche praticien : titulaire
	e.appt(t, e.practice, e.a, e.cab, e.slot(), "done", &e.userAdm) // admin non praticien : titulaire
	e.appt(t, e.practice, e.a, e.home, e.slot(), "done", &e.userA)
	e.appt(t, e.practice, e.a, e.cab, e.slot(), "done", &e.userB) // remplaçant
	e.appt(t, e.practice, e.a, e.home, e.slot(), "done", &e.userB)
	e.appt(t, e.practice, e.a, e.cab, e.slot(), "no_show", &e.userB) // une absence reste au titulaire
	e.appt(t, e.practice, e.a, e.cab, e.slot(), "cancelled", nil)
	e.appt(t, e.practice, e.a, e.home, e.slot(), "cancelled", nil)
	e.appt(t, e.practice, e.a, e.cab, e.slot(), "booked", &e.userA) // repassé à « à venir » : pas compté
	e.appt(t, e.practice, e.c, e.cab, e.slot(), "done", &e.userA)   // A pointe un RDV de C : A est remplaçant de C
	// Hors période.
	loc, _ := time.LoadLocation("Europe/Paris")
	e.appt(t, e.practice, e.a, e.cab, time.Date(2026, 9, 20, 9, 0, 0, 0, loc), "done", nil)
	e.appt(t, e.practice, e.a, e.cab, time.Date(2026, 11, 20, 9, 0, 0, 0, loc), "done", nil)
	// Autre cabinet.
	other := testutil.NewPractice(t, e.db)
	op := testutil.NewPractitioner(t, e.db, other.ID)
	ot := testutil.NewAppointmentType(t, e.db, other.ID)
	oPatient := testutil.NewPatient(t, e.db, other.ID)
	e.exec(t, `INSERT INTO appointment (practice_id, practitioner_id, patient_id, appointment_type_id, during, during_blocked, status, source)
		VALUES ($1, $2, $3, $4, tstzrange('2026-10-15 09:00+02', '2026-10-15 09:30+02'), tstzrange('2026-10-15 09:00+02', '2026-10-15 09:30+02'), 'done', 'staff')`,
		other.ID, op.ID, oPatient.ID, ot.ID)

	ctx := context.Background()
	// Période par défaut : mois civil précédent (horloge au 8 novembre).
	rep, err := e.svc.Report(ctx, e.admin(), service.StatsParams{})
	if err != nil {
		t.Fatal(err)
	}
	if rep.From != "2026-10-01" || rep.To != "2026-10-31" {
		t.Fatalf("période par défaut: %s..%s", rep.From, rep.To)
	}
	check := func(name string, got service.StatsLine, done, noShow, cancelled, homeDone int64) {
		t.Helper()
		if got.Done != done || got.NoShow != noShow || got.Cancelled != cancelled || got.HomeDone != homeDone {
			t.Errorf("%s = %+v, attendu faites=%d absents=%d annulées=%d domicile=%d", name, got.StatsCounts, done, noShow, cancelled, homeDone)
		}
	}
	check("A par A, cabinet", find(t, rep, e.a, e.a, e.cab), 5, 1, 1, 0)
	check("A par A, domicile", find(t, rep, e.a, e.a, e.home), 1, 0, 1, 1)
	check("A par B, cabinet", find(t, rep, e.a, e.b, e.cab), 1, 0, 0, 0)
	check("A par B, domicile", find(t, rep, e.a, e.b, e.home), 1, 0, 0, 1)
	check("C par A (remplaçante)", find(t, rep, e.c, e.a, e.cab), 1, 0, 0, 0)
	if rep.Total.Done != 9 || rep.Total.NoShow != 1 || rep.Total.Cancelled != 2 || rep.Total.HomeDone != 2 {
		t.Errorf("total = %+v", rep.Total)
	}
	for _, g := range rep.Groups {
		for _, p := range g.Performers {
			if p.Substitute != (p.ID != g.ID) {
				t.Errorf("Substitute incohérent pour %s sous %s", p.Name, g.Name)
			}
		}
	}

	// Filtre par praticien (admin) : B ne voit que ce qu'il a réalisé.
	repB, err := e.svc.Report(ctx, e.admin(), service.StatsParams{Practitioner: e.b})
	if err != nil {
		t.Fatal(err)
	}
	if repB.Total.Done != 2 || repB.Total.NoShow != 0 || repB.Total.Cancelled != 0 {
		t.Errorf("filtre B = %+v", repB.Total)
	}
}

// Un praticien ne voit que ses chiffres ; un remplaçant seulement ses séances, pas celles du titulaire.
func TestStatsAccess(t *testing.T) {
	t.Parallel()
	e := newStatsEnv(t)
	e.appt(t, e.practice, e.a, e.cab, e.slot(), "done", nil)
	e.appt(t, e.practice, e.a, e.cab, e.slot(), "done", &e.userB)
	e.appt(t, e.practice, e.a, e.cab, e.slot(), "no_show", nil)
	e.appt(t, e.practice, e.c, e.cab, e.slot(), "done", nil)
	ctx := context.Background()
	p := service.StatsParams{From: "2026-10-01", To: "2026-10-31"}

	// Secrétariat : refusé.
	sec := service.Actor{UserID: e.userSec, PracticeID: e.practice, Role: domain.RoleSecretary}
	if _, err := e.svc.Report(ctx, sec, p); !errors.Is(err, domain.ErrForbidden) {
		t.Errorf("secrétariat: %v", err)
	}
	var buf bytes.Buffer
	if _, err := e.svc.ExportCSV(ctx, sec, p, &buf); !errors.Is(err, domain.ErrForbidden) || buf.Len() != 0 {
		t.Errorf("export secrétariat: %v (%d octets)", err, buf.Len())
	}
	// Praticien sans fiche liée : refusé.
	if _, err := e.svc.Report(ctx, service.Actor{UserID: e.userA, PracticeID: e.practice, Role: domain.RolePractitioner}, p); !errors.Is(err, domain.ErrForbidden) {
		t.Errorf("praticien sans fiche: %v", err)
	}
	// Titulaire A : ses 3 RDV (dont la séance faite par le remplaçant), pas ceux de C ; autre praticien refusé.
	repA, err := e.svc.Report(ctx, e.practitioner(e.userA, e.a), p)
	if err != nil {
		t.Fatal(err)
	}
	if repA.Total.Done != 2 || repA.Total.NoShow != 1 || len(repA.Groups) != 1 || len(repA.Choices) != 0 {
		t.Errorf("A = %+v groupes=%d", repA.Total, len(repA.Groups))
	}
	if _, err := e.svc.Report(ctx, e.practitioner(e.userA, e.a), service.StatsParams{From: p.From, To: p.To, Practitioner: e.c}); !errors.Is(err, domain.ErrForbidden) {
		t.Errorf("A vise C: %v", err)
	}
	// Remplaçant B, même avec une délégation sur A : seulement la séance qu'il a faite.
	b := e.practitioner(e.userB, e.b)
	b.Delegated = []uuid.UUID{e.a}
	repB, err := e.svc.Report(ctx, b, p)
	if err != nil {
		t.Fatal(err)
	}
	if repB.Total.Done != 1 || repB.Total.NoShow != 0 {
		t.Errorf("B = %+v", repB.Total)
	}
	if _, err := e.svc.Report(ctx, b, service.StatsParams{From: p.From, To: p.To, Practitioner: e.a}); !errors.Is(err, domain.ErrForbidden) {
		t.Errorf("B vise A (délégué): %v", err)
	}
	// Admin d'un autre cabinet : le praticien n'existe pas chez lui.
	other := testutil.NewPractice(t, e.db)
	adm := service.Actor{UserID: e.userAdm, PracticeID: other.ID, Role: domain.RoleAdmin}
	if _, err := e.svc.Report(ctx, adm, service.StatsParams{From: p.From, To: p.To, Practitioner: e.a}); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("autre cabinet: %v", err)
	}
	empty, err := e.svc.Report(ctx, adm, p)
	if err != nil || empty.Total.Done != 0 || len(empty.Groups) != 0 {
		t.Errorf("autre cabinet, tous: %+v %v", empty.Total, err)
	}
}

// Bornes en Europe/Paris : dernier jour inclus jusqu'à 23:59, lendemain 00:30 exclu,
// changement d'heure d'automne (25/10/2026) et de printemps (29/03/2026).
func TestStatsPeriodBoundariesDST(t *testing.T) {
	t.Parallel()
	e := newStatsEnv(t)
	loc, _ := time.LoadLocation("Europe/Paris")
	at := func(y int, m time.Month, d, h, mi int) time.Time { return time.Date(y, m, d, h, mi, 0, 0, loc) }
	ctx := context.Background()
	count := func(from, to string) int64 {
		t.Helper()
		r, err := e.svc.Report(ctx, e.admin(), service.StatsParams{From: from, To: to})
		if err != nil {
			t.Fatal(err)
		}
		return r.Total.Done
	}
	for _, when := range []time.Time{
		at(2026, 9, 30, 23, 30), // avant octobre : exclu
		at(2026, 10, 1, 0, 0),   // premier instant d'octobre
		at(2026, 10, 25, 2, 30), // heure répétée (CET, 2e passage)
		at(2026, 10, 31, 23, 30),
		at(2026, 11, 1, 0, 30), // exclu
	} {
		e.appt(t, e.practice, e.a, e.cab, when, "done", nil)
	}
	if got := count("2026-10-01", "2026-10-31"); got != 3 {
		t.Errorf("octobre 2026 = %d, attendu 3", got)
	}
	if got := count("2026-10-31", "2026-10-31"); got != 1 {
		t.Errorf("31/10 seul = %d, attendu 1 (23:30)", got)
	}
	if got := count("2026-11-01", "2026-11-01"); got != 1 {
		t.Errorf("01/11 seul = %d, attendu 1 (00:30)", got)
	}
	// Journée de 25 heures : tout le 25/10, sans le 24 ni le 26.
	e.appt(t, e.practice, e.a, e.cab, at(2026, 10, 24, 23, 59), "done", nil)
	e.appt(t, e.practice, e.a, e.cab, at(2026, 10, 26, 0, 0), "done", nil)
	if got := count("2026-10-25", "2026-10-25"); got != 1 {
		t.Errorf("25/10 = %d, attendu 1", got)
	}
	// Journée de 23 heures (29/03/2026) : 23:30 inclus, 00:30 du lendemain exclu.
	e.appt(t, e.practice, e.a, e.cab, at(2026, 3, 29, 23, 30), "done", nil)
	e.appt(t, e.practice, e.a, e.cab, at(2026, 3, 30, 0, 30), "done", nil)
	if got := count("2026-03-29", "2026-03-29"); got != 1 {
		t.Errorf("29/03 = %d, attendu 1", got)
	}
}

func TestStatsPeriodValidation(t *testing.T) {
	t.Parallel()
	e := newStatsEnv(t)
	for _, tc := range []struct {
		name, from, to string
		ok             bool
	}{
		{"365 jours", "2026-01-01", "2026-12-31", true},
		{"inversée", "2026-10-31", "2026-10-01", false},
		{"une seule borne", "2026-10-01", "", false},
		{"invalide", "2026-13-01", "2026-13-02", false},
		{"367 jours", "2026-01-01", "2027-01-02", false},
		{"366 jours exacts", "2024-01-01", "2024-12-31", true}, // 2024 est bissextile
	} {
		_, err := e.svc.Report(context.Background(), e.admin(), service.StatsParams{From: tc.from, To: tc.to})
		var v domain.ErrValidation
		switch {
		case tc.ok && err != nil:
			t.Errorf("%s: %v", tc.name, err)
		case !tc.ok && !errors.As(err, &v):
			t.Errorf("%s: erreur de validation attendue, obtenu %v", tc.name, err)
		}
	}
}

func TestStatsExportCSV(t *testing.T) {
	t.Parallel()
	e := newStatsEnv(t)
	e.exec(t, `UPDATE appointment_type SET name = '=SUM(A1:A9)' WHERE id = $1`, e.cab)
	e.exec(t, `UPDATE practitioner SET first_name = '@cmd', last_name = 'Dupont; "Jr"' WHERE id = $1`, e.c)
	e.appt(t, e.practice, e.a, e.cab, e.slot(), "done", nil)
	e.appt(t, e.practice, e.a, e.home, e.slot(), "no_show", nil)
	e.appt(t, e.practice, e.c, e.cab, e.slot(), "done", &e.userA)
	e.appt(t, e.practice, e.c, e.cab, e.slot(), "cancelled", nil)

	var buf bytes.Buffer
	rep, err := e.svc.ExportCSV(context.Background(), e.admin(), service.StatsParams{From: "2026-10-01", To: "2026-10-31"}, &buf)
	if err != nil {
		t.Fatal(err)
	}
	if rep.From != "2026-10-01" {
		t.Errorf("période: %s", rep.From)
	}
	out := buf.String()
	if !strings.HasPrefix(out, "\xEF\xBB\xBF") {
		t.Errorf("BOM UTF-8 absent: %q", out[:min(8, len(out))])
	}
	lines := strings.Split(strings.TrimRight(strings.TrimPrefix(out, "\xEF\xBB\xBF"), "\r\n"), "\r\n")
	if want := strings.Join(service.StatsCSVHeader, ";"); lines[0] != want {
		t.Errorf("en-têtes = %q, attendu %q", lines[0], want)
	}
	if len(lines) != 5 { // en-têtes + A/cabinet, A/domicile, C/cabinet (par C : annulée), C/cabinet (par A : faite)
		t.Errorf("%d lignes, attendu 5:\n%s", len(lines), out)
	}
	for _, want := range []string{";'=SUM(A1:A9);", "'@cmd Dupont; \"\"Jr\"\"", ";remplaçant;", ";titulaire;", ";domicile;"} {
		if !strings.Contains(out, want) {
			t.Errorf("CSV sans %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, ";=SUM") || strings.Contains(out, "Zorglub") || strings.Contains(out, "Patientnom") || strings.Contains(out, e.patient.String()) {
		t.Errorf("formule non neutralisée ou donnée patient dans le CSV:\n%s", out)
	}

	// Audit : action, période et praticien, sans nom.
	var n int
	var meta string
	err = e.db.QueryRow(context.Background(), `SELECT count(*), coalesce(max(meta::text), '') FROM audit_log WHERE practice_id = $1 AND action = $2`,
		e.practice, service.ActionStatsExported).Scan(&n, &meta)
	if err != nil || n != 1 {
		t.Fatalf("audit: %d lignes, err %v", n, err)
	}
	if !strings.Contains(meta, "2026-10-31") || strings.Contains(meta, "Alice") {
		t.Errorf("meta d'audit = %s", meta)
	}
}

func TestStatsCSVSafe(t *testing.T) {
	t.Parallel()
	// Via l'export : chaque préfixe dangereux est neutralisé.
	e := newStatsEnv(t)
	prefixes := []string{"=1+1", "+1", "-1", "@a", "\tx"}
	for _, p := range prefixes {
		typ := testutil.NewAppointmentType(t, e.db, e.practice).ID
		e.exec(t, `UPDATE appointment_type SET name = $2 WHERE id = $1`, typ, p)
		e.appt(t, e.practice, e.a, typ, e.slot(), "done", nil)
	}
	var buf bytes.Buffer
	if _, err := e.svc.ExportCSV(context.Background(), e.admin(), service.StatsParams{From: "2026-10-01", To: "2026-10-31"}, &buf); err != nil {
		t.Fatal(err)
	}
	for _, p := range prefixes {
		if !strings.Contains(buf.String(), ";'"+p+";") {
			t.Errorf("%q non neutralisé:\n%s", p, buf.String())
		}
	}
}
