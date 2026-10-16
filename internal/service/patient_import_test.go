package service_test

import (
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

type impEnv struct {
	db    *pgxpool.Pool
	svc   *service.PatientImport
	actor service.Actor
	other uuid.UUID // autre cabinet
}

func newImpEnv(t *testing.T) impEnv {
	t.Helper()
	db := testutil.DB(t)
	pc := testutil.NewPractice(t, db)
	var uid uuid.UUID
	if err := db.QueryRow(context.Background(),
		`INSERT INTO app_user (practice_id, email, password_hash, role) VALUES ($1, $2, 'x', 'secretary') RETURNING id`,
		pc.ID, "imp-"+uuid.NewString()+"@example.test").Scan(&uid); err != nil {
		t.Fatal(err)
	}
	return impEnv{
		db:    db,
		svc:   &service.PatientImport{Pool: db, Clock: testutil.NewFixedClock(time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC))},
		actor: service.Actor{UserID: uid, PracticeID: pc.ID},
		other: testutil.NewPractice(t, db).ID,
	}
}

func (e impEnv) exec(t *testing.T, sql string, args ...any) {
	t.Helper()
	if _, err := e.db.Exec(context.Background(), sql, args...); err != nil {
		t.Fatalf("%s: %v", sql, err)
	}
}

func (e impEnv) count(t *testing.T, practice uuid.UUID) int {
	t.Helper()
	var n int
	if err := e.db.QueryRow(context.Background(), `SELECT count(*) FROM patient WHERE practice_id = $1 AND deleted_at IS NULL`, practice).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

const impCSV = "Nom;Prénom;Téléphone;Email;Date de naissance;Adresse\n" +
	"Martin;Hélène;06 12 34 56 78;;03/04/1985;1 rue A\n" + //                  2 : ok
	"Durand;Paul;;PAUL@Example.test;;\n" + //                                  3 : ok
	"Leroy;Zoé;+33 6 12 34 56 78;;;\n" + //                                    4 : même téléphone que la ligne 2
	"Petit;Marc;;paul@example.test;;\n" + //                                   5 : même email (casse) que la ligne 3
	"Bernard;Luc;07 11 22 33 44;;;\n" + //                                     6 : doublon de la base
	"Roux;;06 99 88 77 66;;;\n" + //                                           7 : prénom vide
	"Blanc;Ana;;;;\n" + //                                                     8 : ni téléphone ni email
	"Noir;Eve;12345;;;\n" + //                                                 9 : téléphone illisible
	"Gris;Tom;;tom@example.test;31/02/2000;\n" + //                           10 : date illisible
	"Vert;Lou;;lou@example.test;;\n" //                                       11 : ok

func TestPatientImportPreview(t *testing.T) {
	e := newImpEnv(t)
	e.exec(t, `INSERT INTO patient (practice_id, first_name, last_name, phone) VALUES ($1, 'Lucas', 'Bernard', '+33711223344')`, e.actor.PracticeID)
	// Même téléphone dans un autre cabinet, ou fiche supprimée : pas un doublon.
	e.exec(t, `INSERT INTO patient (practice_id, first_name, last_name, phone) VALUES ($1, 'X', 'Y', '+33612345678')`, e.other)
	e.exec(t, `INSERT INTO patient (practice_id, first_name, last_name, email, deleted_at) VALUES ($1, 'X', 'Y', 'lou@example.test', now())`, e.actor.PracticeID)

	p, err := e.svc.Preview(context.Background(), e.actor, []byte(impCSV))
	if err != nil {
		t.Fatal(err)
	}
	if p.Total != 10 || p.Valid != 3 || p.Duplicates != 3 || p.Invalid != 4 {
		t.Fatalf("compteurs : %+v", p)
	}
	reasons := map[int]string{}
	for _, l := range p.Problems {
		reasons[l.Line] = l.Status + ": " + l.Reason
	}
	want := map[int]string{
		4: "duplicate: Même téléphone que la ligne 2", 5: "duplicate: Même email que la ligne 3", 6: "duplicate: Ce téléphone existe déjà",
		7: "invalid: Le prénom est requis", 8: "invalid: Ni téléphone ni email", 9: "invalid: Le numéro de téléphone", 10: "invalid: Date de naissance illisible",
	}
	for line, w := range want {
		if !strings.HasPrefix(reasons[line], strings.SplitN(w, ":", 2)[0]) || !strings.Contains(reasons[line], strings.SplitN(w, ": ", 2)[1]) {
			t.Errorf("ligne %d : %q, attendu %q", line, reasons[line], w)
		}
	}
	if len(p.Sample) != 3 || p.Sample[0].Phone != "+33612345678" || p.Sample[1].Email != "paul@example.test" {
		t.Errorf("échantillon : %+v", p.Sample)
	}
	if n := e.count(t, e.actor.PracticeID); n != 1 {
		t.Errorf("l'aperçu ne doit rien écrire : %d patients", n)
	}
}

func TestPatientImportInsertsWithoutConsentByDefault(t *testing.T) {
	e := newImpEnv(t)
	res, err := e.svc.Import(context.Background(), e.actor, []byte(impCSV), false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Imported != 4 || res.Skipped != 6 {
		t.Fatalf("bilan : %+v", res)
	}
	var sms, mail, total int
	var addr, birth *string
	if err := e.db.QueryRow(context.Background(), `SELECT count(*) FILTER (WHERE sms_opt_in), count(*) FILTER (WHERE email_opt_in), count(*) FROM patient WHERE practice_id = $1`, e.actor.PracticeID).Scan(&sms, &mail, &total); err != nil {
		t.Fatal(err)
	}
	if total != 4 || sms != 0 || mail != 0 {
		t.Errorf("total=%d sms=%d email=%d", total, sms, mail)
	}
	if err := e.db.QueryRow(context.Background(), `SELECT address, birth_date::text FROM patient WHERE practice_id = $1 AND last_name = 'Martin'`, e.actor.PracticeID).Scan(&addr, &birth); err != nil {
		t.Fatal(err)
	}
	if addr == nil || *addr != "1 rue A" || birth == nil || *birth != "1985-04-03" {
		t.Errorf("adresse/naissance : %v %v", addr, birth)
	}
	if n := e.count(t, e.other); n != 0 {
		t.Errorf("autre cabinet : %d", n)
	}
}

func TestPatientImportWithConsent(t *testing.T) {
	e := newImpEnv(t)
	if _, err := e.svc.Import(context.Background(), e.actor, []byte(impCSV), true); err != nil {
		t.Fatal(err)
	}
	// Le consentement ne vaut que pour une coordonnée existante.
	var badSMS, badMail, okSMS, okMail int
	if err := e.db.QueryRow(context.Background(), `SELECT
		count(*) FILTER (WHERE sms_opt_in AND phone IS NULL), count(*) FILTER (WHERE email_opt_in AND email IS NULL),
		count(*) FILTER (WHERE sms_opt_in), count(*) FILTER (WHERE email_opt_in)
		FROM patient WHERE practice_id = $1`, e.actor.PracticeID).Scan(&badSMS, &badMail, &okSMS, &okMail); err != nil {
		t.Fatal(err)
	}
	if badSMS != 0 || badMail != 0 || okSMS != 2 || okMail != 2 {
		t.Errorf("opt-in : badSMS=%d badMail=%d sms=%d mail=%d", badSMS, badMail, okSMS, okMail)
	}
}

func TestPatientImportIsAtomic(t *testing.T) {
	e := newImpEnv(t)
	// L'écriture de l'audit (après l'insertion des patients) échoue : les patients doivent disparaître.
	e.exec(t, `CREATE FUNCTION fail_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'boom'; END $$`)
	e.exec(t, `CREATE TRIGGER fail_audit BEFORE INSERT ON audit_log FOR EACH ROW EXECUTE FUNCTION fail_audit()`)
	if _, err := e.svc.Import(context.Background(), e.actor, []byte(impCSV), false); err == nil {
		t.Fatal("erreur attendue")
	}
	if n := e.count(t, e.actor.PracticeID); n != 0 {
		t.Errorf("import non atomique : %d patients", n)
	}
}

func TestPatientImportRechecksDuplicatesAtConfirmation(t *testing.T) {
	e := newImpEnv(t)
	csv := []byte("nom;prenom;email\nA;B;ab@example.test\nC;D;cd@example.test\n")
	p, err := e.svc.Preview(context.Background(), e.actor, csv)
	if err != nil || p.Valid != 2 {
		t.Fatalf("aperçu : %+v %v", p, err)
	}
	// Entre l'aperçu et la confirmation, une fiche est créée.
	e.exec(t, `INSERT INTO patient (practice_id, first_name, last_name, email) VALUES ($1, 'A', 'B', 'AB@example.test')`, e.actor.PracticeID)
	res, err := e.svc.Import(context.Background(), e.actor, csv, false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Imported != 1 || res.Skipped != 1 || e.count(t, e.actor.PracticeID) != 2 {
		t.Errorf("bilan : %+v, %d fiches", res, e.count(t, e.actor.PracticeID))
	}
}

func TestPatientImportAuditHasNoPatientData(t *testing.T) {
	e := newImpEnv(t)
	if _, err := e.svc.Import(context.Background(), e.actor, []byte(impCSV), false); err != nil {
		t.Fatal(err)
	}
	var user uuid.UUID
	var meta, entity string
	if err := e.db.QueryRow(context.Background(), `SELECT user_id, meta::text, entity FROM audit_log WHERE action = 'patient.imported'`).Scan(&user, &meta, &entity); err != nil {
		t.Fatal(err)
	}
	if user != e.actor.UserID || entity != "patient_import" {
		t.Errorf("audit : %v %s", user, entity)
	}
	for _, leak := range []string{"Martin", "Hélène", "example.test", "6 12", "+33"} {
		if strings.Contains(meta, leak) {
			t.Errorf("donnée patient dans l'audit : %s", meta)
		}
	}
	if !strings.Contains(meta, `"imported": "4"`) || !strings.Contains(meta, `"skipped": "6"`) {
		t.Errorf("compteurs : %s", meta)
	}
}

func TestPatientImportNothingToImport(t *testing.T) {
	e := newImpEnv(t)
	_, err := e.svc.Import(context.Background(), e.actor, []byte("nom;prenom\nA;B\n"), false)
	var ve domain.ErrValidation
	if !errors.As(err, &ve) {
		t.Fatalf("erreur de validation attendue : %v", err)
	}
}
