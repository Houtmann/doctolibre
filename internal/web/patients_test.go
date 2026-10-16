package web_test

import (
	"context"
	"io"
	"log/slog"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"doctolibre/internal/auth"
	"doctolibre/internal/config"
	"doctolibre/internal/domain"
	"doctolibre/internal/service"
	"doctolibre/internal/store"
	"doctolibre/internal/store/sqlcgen"
	"doctolibre/internal/testutil"
	"doctolibre/internal/web"
)

type patEnv struct {
	c     *client
	db    *pgxpool.Pool
	actor service.Actor
	svc   *service.Patients
}

func newPatientEnv(t *testing.T, role string) patEnv {
	t.Helper()
	db := testutil.DB(t)
	ctx := context.Background()
	u, err := auth.CreateUser(ctx, db, auth.CreateUserParams{Email: userEmail, Role: role, PracticeName: "Cabinet test", Password: pw})
	if err != nil {
		t.Fatal(err)
	}
	clock := testutil.NewFixedClock(time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC))
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	svc := &service.Patients{Pool: db, Clock: clock}
	h := web.Routes(web.Deps{
		Config: config.Config{Env: config.EnvDev}, Logger: logger, DB: db,
		Auth:     &auth.Service{Pool: db, Clock: clock, Logger: logger},
		Patients: svc,
	})
	c := newClient(t, h, "192.0.2.30")
	if rec := c.login(userEmail, pw, "/admin"); rec.Code != 303 {
		t.Fatalf("login: %d", rec.Code)
	}
	return patEnv{c: c, db: db, actor: service.Actor{UserID: u.ID, PracticeID: u.PracticeID}, svc: svc}
}

func (e patEnv) newPatient(t *testing.T, first, last, phone string) sqlcgen.Patient {
	t.Helper()
	p, err := sqlcgen.New(e.db).CreatePatient(context.Background(), sqlcgen.CreatePatientParams{
		PracticeID: e.actor.PracticeID, FirstName: first, LastName: last,
		Phone: pgtype.Text{String: phone, Valid: phone != ""}, Email: pgtype.Text{String: strings.ToLower(last) + "@example.test", Valid: true},
		SmsOptIn: true, EmailOptIn: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestPatientSearchIgnoresCaseAndAccents(t *testing.T) {
	e := newPatientEnv(t, domain.RoleSecretary)
	e.newPatient(t, "Hélène", "Martin", "+33612345678")
	e.newPatient(t, "Paul", "Durand", "+33698765432")

	for _, q := range []string{"helene", "HÉLÈNE", "martin helene", "0612345678", "06 12 34 56 78"} {
		body := e.c.do("GET", "/admin/patients?q="+url.QueryEscape(q), nil).Body.String()
		if !strings.Contains(body, "Martin Hélène") || strings.Contains(body, "Durand") {
			t.Errorf("q=%q: résultat inattendu", q)
		}
	}
	if body := e.c.do("GET", "/admin/patients?q=zzz", nil).Body.String(); !strings.Contains(body, "Aucun patient") {
		t.Error("recherche vide: message attendu")
	}
	// Les jokers LIKE sont littéraux.
	if body := e.c.do("GET", "/admin/patients?q=%25", nil).Body.String(); strings.Contains(body, "Martin") {
		t.Error("« % » ne doit pas être un joker")
	}
}

func TestPatientListKeysetPagination(t *testing.T) {
	e := newPatientEnv(t, domain.RoleAdmin)
	for i := 0; i < service.PatientPageSize+3; i++ {
		e.newPatient(t, "Prénom", "Nom"+string(rune('A'+i/26))+string(rune('a'+i%26)), "")
	}
	first, err := e.svc.List(context.Background(), e.actor, "", "")
	if err != nil || len(first.Patients) != service.PatientPageSize || first.Next == "" {
		t.Fatalf("page 1: %d, next=%q, err=%v", len(first.Patients), first.Next, err)
	}
	second, err := e.svc.List(context.Background(), e.actor, "", first.Next)
	if err != nil || len(second.Patients) != 3 || second.Next != "" {
		t.Fatalf("page 2: %d, next=%q, err=%v", len(second.Patients), second.Next, err)
	}
	if second.Patients[0].LastName <= first.Patients[len(first.Patients)-1].LastName {
		t.Error("ordre keyset non respecté")
	}
}

func TestPatientEditAndNoMedicalWarning(t *testing.T) {
	e := newPatientEnv(t, domain.RolePractitioner)
	p := e.newPatient(t, "Alex", "Dupont", "+33611111111")
	page := e.c.do("GET", "/admin/patients/"+p.ID.String(), nil).Body.String()
	if !strings.Contains(page, "Pas de données médicales") {
		t.Error("avertissement « pas de données médicales » absent")
	}
	rec := e.c.do("POST", "/admin/patients/"+p.ID.String(), url.Values{
		"first_name": {"Alexis"}, "last_name": {"Dupont"}, "phone": {"06 22 22 22 22"}, "email": {"alexis@example.test"},
		"notes_admin": {"préfère le matin"}, "email_opt_in": {"1"},
	})
	if rec.Code != 303 {
		t.Fatalf("édition: %d", rec.Code)
	}
	got, err := sqlcgen.New(e.db).GetPatient(context.Background(), sqlcgen.GetPatientParams{ID: p.ID, PracticeID: e.actor.PracticeID})
	if err != nil {
		t.Fatal(err)
	}
	if got.FirstName != "Alexis" || got.Phone.String != "+33622222222" || got.NotesAdmin != "préfère le matin" || got.SmsOptIn || !got.EmailOptIn {
		t.Errorf("fiche mal enregistrée: %+v", got)
	}
	// Saisie invalide : 422 et rien d'écrit.
	rec = e.c.do("POST", "/admin/patients/"+p.ID.String(), url.Values{"first_name": {"X"}, "last_name": {"Y"}, "phone": {"abc"}})
	if rec.Code != 422 {
		t.Errorf("saisie invalide: %d", rec.Code)
	}
	// Le champ fautif (téléphone) est marqué, pas le nom.
	body := rec.Body.String()
	if !strings.Contains(body, `class="dl-field is-invalid"`) || !strings.Contains(body, `id="pf-phone"`) ||
		!strings.Contains(body, `aria-invalid="true"`) || !strings.Contains(body, `aria-describedby="pf-contact-help form-errors"`) {
		t.Error("champ téléphone non marqué is-invalid / aria-invalid")
	}
	if strings.Contains(body, `id="pf-last-name" name="last_name" value="Y" maxlength="80" required autocomplete="off" aria-invalid`) {
		t.Error("le nom ne devrait pas être marqué en erreur")
	}
}

func TestPatientNewInvalidMarksFields(t *testing.T) {
	e := newPatientEnv(t, domain.RoleSecretary)
	rec := e.c.do("POST", "/admin/patients", url.Values{"first_name": {""}, "last_name": {"Martin"}, "phone": {"0612345678"}})
	body := rec.Body.String()
	if rec.Code != 422 || !strings.Contains(body, `class="dl-field is-invalid"`) || !strings.Contains(body, `aria-invalid="true"`) {
		t.Fatalf("prénom manquant: %d, champ non marqué", rec.Code)
	}
}

func TestPatientCreateDuplicateProposal(t *testing.T) {
	e := newPatientEnv(t, domain.RoleSecretary)
	e.newPatient(t, "Hélène", "Martin", "+33612345678")
	form := func() url.Values {
		return url.Values{"first_name": {"Helene"}, "last_name": {"Martin"}, "phone": {"0612345678"}, "sms_opt_in": {"1"}}
	}
	rec := e.c.do("POST", "/admin/patients", form())
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "existe déjà") || !strings.Contains(rec.Body.String(), "Créer quand même") {
		t.Fatalf("doublon non proposé: %d", rec.Code)
	}
	res, _ := e.svc.List(context.Background(), e.actor, "", "")
	if len(res.Patients) != 1 {
		t.Fatalf("aucune fiche ne devait être créée: %d", len(res.Patients))
	}
	f := form()
	f.Set("force", "1")
	if rec := e.c.do("POST", "/admin/patients", f); rec.Code != 303 {
		t.Fatalf("création forcée: %d", rec.Code)
	}
	res, _ = e.svc.List(context.Background(), e.actor, "", "")
	if len(res.Patients) != 2 {
		t.Fatalf("2 fiches attendues: %d", len(res.Patients))
	}
}

func TestPatientMerge(t *testing.T) {
	e := newPatientEnv(t, domain.RoleAdmin)
	ctx := context.Background()
	keep := e.newPatient(t, "Hélène", "Martin", "+33612345678")
	dup := e.newPatient(t, "Helene", "Martin", "")
	if _, err := sqlcgen.New(e.db).UpdatePatient(ctx, sqlcgen.UpdatePatientParams{
		ID: dup.ID, PracticeID: e.actor.PracticeID, FirstName: "Helene", LastName: "Martin",
		Email: dup.Email, SmsOptIn: true, EmailOptIn: false,
	}); err != nil {
		t.Fatal(err)
	}
	pr, err := (&service.Practice{Pool: e.db, Clock: testutil.NewFixedClock(time.Now())}).Practitioner(ctx, e.actor)
	if err != nil {
		t.Fatal(err)
	}
	ty := testutil.NewAppointmentType(t, e.db, e.actor.PracticeID)
	start := time.Date(2026, 10, 20, 8, 0, 0, 0, time.UTC)
	for i, p := range []uuid.UUID{dup.ID, keep.ID} {
		s := start.Add(time.Duration(i) * time.Hour)
		if _, err := store.CreateAppointment(ctx, sqlcgen.New(e.db), store.NewAppointment{
			PracticeID: e.actor.PracticeID, PractitionerID: pr.ID, PatientID: p, AppointmentTypeID: ty.ID,
			Start: s, End: s.Add(30 * time.Minute), Source: "staff",
		}); err != nil {
			t.Fatal(err)
		}
	}

	if rec := e.c.do("POST", "/admin/patients/"+dup.ID.String()+"/merge?into="+dup.ID.String(), url.Values{}); rec.Code != 422 {
		t.Errorf("fusion avec soi-même: %d", rec.Code)
	}
	rec := e.c.do("POST", "/admin/patients/"+dup.ID.String()+"/merge?into="+keep.ID.String(), url.Values{})
	if rec.Code != 303 || rec.Header().Get("Location") != "/admin/patients/"+keep.ID.String() {
		t.Fatalf("fusion: %d %s", rec.Code, rec.Header().Get("Location"))
	}

	var n int
	if err := e.db.QueryRow(ctx, `SELECT count(*) FROM appointment WHERE patient_id = $1`, keep.ID).Scan(&n); err != nil || n != 2 {
		t.Errorf("RDV rattachés à la fiche conservée: %d (err=%v)", n, err)
	}
	if err := e.db.QueryRow(ctx, `SELECT count(*) FROM appointment WHERE patient_id = $1`, dup.ID).Scan(&n); err != nil || n != 0 {
		t.Errorf("RDV restants sur le doublon: %d (err=%v)", n, err)
	}
	var deleted bool
	var mergedInto uuid.UUID
	if err := e.db.QueryRow(ctx, `SELECT deleted_at IS NOT NULL, merged_into FROM patient WHERE id = $1`, dup.ID).Scan(&deleted, &mergedInto); err != nil || !deleted || mergedInto != keep.ID {
		t.Errorf("doublon: deleted=%v merged_into=%v err=%v", deleted, mergedInto, err)
	}
	var emailOpt bool
	if err := e.db.QueryRow(ctx, `SELECT email_opt_in FROM patient WHERE id = $1`, keep.ID).Scan(&emailOpt); err != nil || emailOpt {
		t.Errorf("l'opt-out email du doublon doit être conservé (opt_in=%v, err=%v)", emailOpt, err)
	}
	var action, meta string
	if err := e.db.QueryRow(ctx, `SELECT action, meta::text FROM audit_log WHERE entity = 'patient' AND entity_id = $1 AND action = 'patient.merged'`, keep.ID).Scan(&action, &meta); err != nil {
		t.Fatalf("audit absent: %v", err)
	}
	if !strings.Contains(meta, dup.ID.String()) {
		t.Errorf("audit sans l'identifiant fusionné: %s", meta)
	}
	if rec := e.c.do("GET", "/admin/patients/"+dup.ID.String(), nil); rec.Code != 404 {
		t.Errorf("fiche fusionnée: %d, 404 attendu", rec.Code)
	}
}

func TestPatientsAreScopedToPractice(t *testing.T) {
	e := newPatientEnv(t, domain.RoleAdmin)
	other := testutil.NewPractice(t, e.db)
	foreign := testutil.NewPatient(t, e.db, other.ID)
	if rec := e.c.do("GET", "/admin/patients/"+foreign.ID.String(), nil); rec.Code != 404 {
		t.Errorf("fiche d'un autre cabinet: %d", rec.Code)
	}
	mine := e.newPatient(t, "A", "B", "+33600000000")
	if rec := e.c.do("POST", "/admin/patients/"+mine.ID.String()+"/merge?into="+foreign.ID.String(), url.Values{}); rec.Code != 404 {
		t.Errorf("fusion vers un autre cabinet: %d", rec.Code)
	}
}

// TestPatientSearchUsesTrigramIndex vérifie que le prédicat de recherche est
// servi par l'index trigram.
func TestPatientSearchUsesTrigramIndex(t *testing.T) {
	e := newPatientEnv(t, domain.RoleAdmin)
	ctx := context.Background()
	tx, err := e.db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	// Seqscan désactivé : on vérifie que l'index peut servir le prédicat (expression,
	// classe d'opérateurs, prédicat partiel). Le choix réel dépend des statistiques.
	if _, err := tx.Exec(ctx, `SET LOCAL enable_seqscan = off`); err != nil {
		t.Fatal(err)
	}
	rows, err := tx.Query(ctx, `EXPLAIN SELECT * FROM patient
		WHERE deleted_at IS NULL
		  AND patient_search_text(first_name, last_name, phone, email) LIKE '%' || f_unaccent(lower($1::text)) || '%'`,
		"helene")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var plan strings.Builder
	for rows.Next() {
		var l string
		if err := rows.Scan(&l); err != nil {
			t.Fatal(err)
		}
		plan.WriteString(l + "\n")
	}
	if !strings.Contains(plan.String(), "patient_search_trgm_idx") {
		t.Errorf("l'index trigram n'est pas utilisé:\n%s", plan.String())
	}
}
