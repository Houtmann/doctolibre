package service_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"doctolibre/internal/domain"
	"doctolibre/internal/service"
	"doctolibre/internal/store/sqlcgen"
	"doctolibre/internal/testutil"
)

// Marqueurs reconnaissables : aucun ne doit survivre à l'anonymisation.
const (
	mkFirst   = "Zelphira"
	mkLast    = "Quillmark"
	mkPhone   = "+33699887766"
	mkEmail   = "zelphira.quillmark@example.test"
	mkAddress = "12 rue des Zymotiques"
	mkNote    = "NOTEMARK-zx"
	mkLabel   = "LABELMARK-zx"
	mkReason  = "REASONMARK-zx"
)

var allMarkers = []string{mkFirst, mkLast, mkPhone, mkEmail, mkAddress, mkNote, mkLabel, mkReason}

type privEnv struct {
	db    *pgxpool.Pool
	clock *testutil.FixedClock
	pc    sqlcgen.Practice
	pr    sqlcgen.Practitioner
	typ   sqlcgen.AppointmentType
	user  uuid.UUID
	actor service.Actor
	pat   *service.Patients
	priv  *service.Privacy
}

var privNow = time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC)

func newPrivEnv(t *testing.T) *privEnv {
	t.Helper()
	db := testutil.DB(t)
	clock := testutil.NewFixedClock(privNow)
	pc := testutil.NewPractice(t, db)
	e := &privEnv{db: db, clock: clock, pc: pc, pr: testutil.NewPractitioner(t, db, pc.ID), typ: testutil.NewAppointmentType(t, db, pc.ID)}
	e.user = e.newUser(t, "staff-"+uuid.NewString()+"@example.test")
	e.actor = service.Actor{UserID: e.user, PracticeID: pc.ID}
	e.pat = &service.Patients{Pool: db, Clock: clock}
	e.priv = &service.Privacy{Pool: db, Clock: clock}
	return e
}

func (e *privEnv) exec(t *testing.T, sql string, args ...any) {
	t.Helper()
	if _, err := e.db.Exec(context.Background(), sql, args...); err != nil {
		t.Fatalf("%s: %v", sql, err)
	}
}

func (e *privEnv) count(t *testing.T, sql string, args ...any) int {
	t.Helper()
	var n int
	if err := e.db.QueryRow(context.Background(), sql, args...).Scan(&n); err != nil {
		t.Fatalf("%s: %v", sql, err)
	}
	return n
}

func (e *privEnv) newUser(t *testing.T, email string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := e.db.QueryRow(context.Background(),
		`INSERT INTO app_user (practice_id, email, password_hash, role) VALUES ($1, $2, 'x', 'admin') RETURNING id`, e.pc.ID, email).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func (e *privEnv) patient(t *testing.T, first, last, phone, email string) sqlcgen.Patient {
	t.Helper()
	p, err := sqlcgen.New(e.db).CreatePatient(context.Background(), sqlcgen.CreatePatientParams{
		PracticeID: e.pc.ID, FirstName: first, LastName: last,
		Phone: pgtype.Text{String: phone, Valid: phone != ""}, Email: pgtype.Text{String: email, Valid: email != ""},
		SmsOptIn: true, EmailOptIn: true, NotesAdmin: mkNote, Address: pgtype.Text{String: mkAddress, Valid: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// appt crée un rendez-vous (status) à start, avec adresse et motif d'annulation identifiants.
func (e *privEnv) appt(t *testing.T, patientID uuid.UUID, start time.Time, status string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	err := e.db.QueryRow(context.Background(), `
INSERT INTO appointment (practice_id, practitioner_id, patient_id, appointment_type_id, during, during_blocked, status, source, address, cancel_reason, manage_token_hash)
VALUES ($1, $2, $3, $4, tstzrange($5, $6), tstzrange($5, $6), $7, 'staff', $8, $9, $10) RETURNING id`,
		e.pc.ID, e.pr.ID, patientID, e.typ.ID, start, start.Add(30*time.Minute), status, mkAddress, mkReason, "hash-"+uuid.NewString()).Scan(&id)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// message insère un message de l'outbox avec contenu identifiant.
func (e *privEnv) message(t *testing.T, apptID *uuid.UUID, recipient, status string, sentAt *time.Time) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	err := e.db.QueryRow(context.Background(), `
INSERT INTO outbox_message (practice_id, kind, template, payload, recipient, appointment_id, status, sent_at, last_error, created_at, updated_at)
VALUES ($1, 'email', 'confirmation', $2::jsonb, $3, $4, $5, $6, $7, $8, $8) RETURNING id`,
		e.pc.ID, `{"first_name":"`+mkFirst+`","manage_url":"https://x.test/r/SECRETTOKEN","body":"Bonjour `+mkFirst+`"}`, recipient, apptID, status, sentAt,
		"échec pour "+recipient, privNow.AddDate(-1, 0, 0)).Scan(&id)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// dump retourne le contenu textuel de toutes les tables du schéma public.
func (e *privEnv) dump(t *testing.T) string {
	t.Helper()
	ctx := context.Background()
	rows, err := e.db.Query(ctx, `SELECT tablename FROM pg_tables WHERE schemaname = 'public' AND tablename <> 'goose_db_version'`)
	if err != nil {
		t.Fatal(err)
	}
	var tables []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			t.Fatal(err)
		}
		tables = append(tables, n)
	}
	rows.Close()
	var sb strings.Builder
	for _, tb := range tables {
		var s *string
		if err := e.db.QueryRow(ctx, `SELECT string_agg(x::text, E'\n') FROM "`+tb+`" x`).Scan(&s); err != nil {
			t.Fatalf("dump %s: %v", tb, err)
		}
		if s != nil {
			sb.WriteString(*s)
			sb.WriteByte('\n')
		}
	}
	return sb.String()
}

func assertNoMarkers(t *testing.T, e *privEnv) {
	t.Helper()
	d := strings.ToLower(e.dump(t))
	for _, m := range allMarkers {
		if strings.Contains(d, strings.ToLower(m)) {
			t.Errorf("la base contient encore %q", m)
		}
	}
	if strings.Contains(d, "secrettoken") {
		t.Error("la base contient encore un jeton de gestion")
	}
}

func TestEraseAnonymizesAndLeavesNoIdentifyingData(t *testing.T) {
	t.Parallel()
	e := newPrivEnv(t)
	ctx := context.Background()
	p := e.patient(t, mkFirst, mkLast, mkPhone, mkEmail)
	past := e.appt(t, p.ID, privNow.AddDate(0, 0, -30), "done")
	e.appt(t, p.ID, privNow.AddDate(0, 0, -10), "cancelled")
	sent := privNow.AddDate(0, 0, -29)
	e.message(t, &past, mkEmail, "sent", &sent)
	e.message(t, &past, mkEmail, "pending", nil)
	e.message(t, nil, mkPhone, "sent", &sent) // code de vérification, sans rendez-vous
	e.exec(t, `INSERT INTO care_plan (practice_id, patient_id, practitioner_id, appointment_type_id, label, sessions_prescribed, frequency_per_week, start_date)
		VALUES ($1, $2, $3, $4, $5, 5, 2, '2026-09-01')`, e.pc.ID, p.ID, e.pr.ID, e.typ.ID, mkLabel)
	// Du bruit : un autre patient ne doit pas être touché.
	other := e.patient(t, "Autre", "Personne", "+33611223344", "autre@example.test")

	mode, err := e.pat.Erase(ctx, e.actor, p.ID)
	if err != nil || mode != "anonymized" {
		t.Fatalf("Erase = %q, %v", mode, err)
	}
	// Seule la fiche « autre » garde ses données (marqueurs retirés pour la comparaison).
	e.exec(t, `UPDATE patient SET notes_admin = '', address = NULL WHERE id = $1`, other.ID)
	assertNoMarkers(t, e)
	if n := e.count(t, `SELECT count(*) FROM appointment WHERE patient_id = $1`, p.ID); n != 2 {
		t.Errorf("rendez-vous conservés = %d, attendu 2", n)
	}
	if n := e.count(t, `SELECT count(*) FROM patient WHERE id = $1 AND last_name = 'supprimé' AND anonymized_at IS NOT NULL AND deleted_at IS NOT NULL AND phone IS NULL AND email IS NULL`, p.ID); n != 1 {
		t.Error("fiche non anonymisée")
	}
	if n := e.count(t, `SELECT count(*) FROM outbox_message WHERE appointment_id = $1 AND status = 'pending'`, past); n != 0 {
		t.Error("message en attente non annulé")
	}
	if n := e.count(t, `SELECT count(*) FROM patient WHERE id = $1 AND last_name = 'Personne'`, other.ID); n != 1 {
		t.Error("un autre patient a été touché")
	}
	if n := e.count(t, `SELECT count(*) FROM audit_log WHERE action = 'patient.anonymized' AND entity_id = $1 AND practice_id = $2 AND user_id = $3`, p.ID, e.pc.ID, e.user); n != 1 {
		t.Error("anonymisation non journalisée")
	}
	// Une seconde suppression ne trouve plus rien.
	if _, err := e.pat.Erase(ctx, e.actor, p.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("seconde suppression = %v, attendu ErrNotFound", err)
	}
}

func TestEraseHardDeletesPatientWithoutAppointments(t *testing.T) {
	t.Parallel()
	e := newPrivEnv(t)
	p := e.patient(t, mkFirst, mkLast, mkPhone, mkEmail)
	sent := privNow.AddDate(0, 0, -1)
	e.message(t, nil, mkEmail, "sent", &sent)
	mode, err := e.pat.Erase(context.Background(), e.actor, p.ID)
	if err != nil || mode != "deleted" {
		t.Fatalf("Erase = %q, %v", mode, err)
	}
	if n := e.count(t, `SELECT count(*) FROM patient WHERE id = $1`, p.ID); n != 0 {
		t.Error("fiche toujours présente")
	}
	assertNoMarkers(t, e)
}

func TestEraseRefusesUpcomingAppointments(t *testing.T) {
	t.Parallel()
	e := newPrivEnv(t)
	p := e.patient(t, mkFirst, mkLast, mkPhone, mkEmail)
	e.appt(t, p.ID, privNow.AddDate(0, 0, 3), "booked")
	_, err := e.pat.Erase(context.Background(), e.actor, p.ID)
	if len(domain.FieldErrors(err)) == 0 {
		t.Fatalf("Erase = %v, attendu une erreur de validation", err)
	}
	if n := e.count(t, `SELECT count(*) FROM patient WHERE id = $1 AND anonymized_at IS NULL`, p.ID); n != 1 {
		t.Error("la fiche ne devait pas changer")
	}
}

func TestEraseCascadesToMergedDuplicates(t *testing.T) {
	t.Parallel()
	e := newPrivEnv(t)
	ctx := context.Background()
	keep := e.patient(t, mkFirst, mkLast, mkPhone, mkEmail)
	dup := e.patient(t, mkFirst, mkLast, "+33699887755", "dup."+mkEmail)
	e.appt(t, keep.ID, privNow.AddDate(0, 0, -20), "done")
	if _, err := e.pat.Merge(ctx, e.actor, dup.ID, keep.ID); err != nil {
		t.Fatal(err)
	}
	if mode, err := e.pat.Erase(ctx, e.actor, keep.ID); err != nil || mode != "anonymized" {
		t.Fatalf("Erase = %q, %v", mode, err)
	}
	e.exec(t, `UPDATE patient SET notes_admin = '', address = NULL`)
	assertNoMarkers(t, e)
	if n := e.count(t, `SELECT count(*) FROM patient WHERE practice_id = $1 AND anonymized_at IS NOT NULL`, e.pc.ID); n != 2 {
		t.Errorf("fiches anonymisées = %d, attendu 2 (fiche + doublon fusionné)", n)
	}
}

func TestMarkSentRemovesSecretsFromPayload(t *testing.T) {
	t.Parallel()
	e := newPrivEnv(t)
	ctx := context.Background()
	id := e.message(t, nil, "a@example.test", "sending", nil)
	e.exec(t, `UPDATE outbox_message SET attempts = 1, payload = payload || '{"reset_url":"https://x.test/reset/SECRETTOKEN","code":"123456","otp":true}' WHERE id = $1`, id)
	n, err := sqlcgen.New(e.db).MarkOutboxSent(ctx, sqlcgen.MarkOutboxSentParams{Now: privNow, ID: id, Attempts: 1})
	if err != nil || n != 1 {
		t.Fatalf("MarkOutboxSent = %d, %v", n, err)
	}
	var payload string
	if err := e.db.QueryRow(ctx, `SELECT payload::text FROM outbox_message WHERE id = $1`, id).Scan(&payload); err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"manage_url", "reset_url", "SECRETTOKEN", "123456", "body"} {
		if strings.Contains(payload, secret) {
			t.Errorf("payload après envoi contient encore %q : %s", secret, payload)
		}
	}
	if !strings.Contains(payload, "otp") {
		t.Errorf("les champs non secrets doivent rester : %s", payload)
	}
}

func TestPurgeRunOnceWithFixedClock(t *testing.T) {
	t.Parallel()
	e := newPrivEnv(t)
	ctx := context.Background()
	day := func(n int) time.Time { return privNow.AddDate(0, 0, -n) }

	// Messages : rétention 90 jours par défaut.
	sentOld, sentNew := day(100), day(10)
	oldMsg := e.message(t, nil, mkEmail, "sent", &sentOld)
	newMsg := e.message(t, nil, "recent@example.test", "sent", &sentNew)
	failedOld := e.message(t, nil, mkEmail, "failed", nil)
	e.exec(t, `UPDATE outbox_message SET updated_at = $2 WHERE id = $1`, failedOld, day(120))
	e.exec(t, `UPDATE outbox_message SET updated_at = $2 WHERE id = ANY($1)`, []uuid.UUID{oldMsg, newMsg}, day(1))
	pending := e.message(t, nil, mkEmail, "pending", nil)
	e.exec(t, `UPDATE outbox_message SET updated_at = $2 WHERE id = $1`, pending, day(400))

	// Sessions, jetons, codes expirés ou valides.
	e.exec(t, `INSERT INTO session (id, user_id, expires_at) VALUES ('old', $1, $2), ('live', $1, $3)`, e.user, day(1), privNow.Add(time.Hour))
	e.exec(t, `INSERT INTO password_reset (token_hash, user_id, expires_at) VALUES ('old', $1, $2), ('live', $1, $3)`, e.user, day(1), privNow.Add(time.Hour))
	e.exec(t, `INSERT INTO otp_challenge (practice_id, channel, recipient_hash, binding_hash, code_hash, expires_at) VALUES
		($1, 'sms', 'r', 'b', 'c', $2), ($1, 'sms', 'r', 'b', 'c', $3)`, e.pc.ID, day(1), privNow.Add(time.Hour))

	// Journal : rétention 365 jours.
	e.exec(t, `INSERT INTO audit_log (at, practice_id, actor, action, entity) VALUES ($2, $1, 'user', 'old', 'x'), ($3, $1, 'user', 'recent', 'x')`, e.pc.ID, day(400), day(5))
	e.exec(t, `INSERT INTO audit_log (at, actor, action, entity) VALUES ($1, 'anonymous', 'orphan_old', 'user'), ($2, 'anonymous', 'orphan_recent', 'user')`, day(400), day(5))

	// Patients : inactif depuis 4 ans, récent, et inactif mais avec un rendez-vous à venir.
	longAgo := privNow.AddDate(-4, 0, 0)
	inactive := e.patient(t, mkFirst, mkLast, mkPhone, mkEmail)
	e.appt(t, inactive.ID, longAgo, "done")
	e.exec(t, `UPDATE patient SET created_at = $2, updated_at = $2 WHERE id = $1`, inactive.ID, longAgo)
	recent := e.patient(t, "Récent", "Patient", "+33600000001", "")
	e.appt(t, recent.ID, day(30), "done")
	future := e.patient(t, "Futur", "Patient", "+33600000002", "")
	e.appt(t, future.ID, privNow.AddDate(0, 0, 5), "booked")
	e.exec(t, `UPDATE patient SET created_at = $2, updated_at = $2 WHERE id = ANY($1)`, []uuid.UUID{future.ID}, longAgo)

	// Purge des patients désactivée par défaut : rien ne bouge côté patients.
	purge := &service.Privacy{Pool: e.db, Clock: e.clock}
	res, err := purge.RunOnce(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if res.PatientsAnonymized != 0 {
		t.Errorf("patients anonymisés sans activation = %d", res.PatientsAnonymized)
	}
	scrubbed := func(id uuid.UUID) bool {
		return e.count(t, `SELECT count(*) FROM outbox_message WHERE id = $1 AND scrubbed_at IS NOT NULL AND recipient = '' AND payload = '{}'::jsonb AND last_error IS NULL`, id) == 1
	}
	if !scrubbed(oldMsg) || !scrubbed(failedOld) {
		t.Error("messages anciens non effacés")
	}
	if scrubbed(newMsg) || scrubbed(pending) {
		t.Error("message récent ou en attente effacé")
	}
	if n := e.count(t, `SELECT count(*) FROM outbox_message WHERE id = $1 AND status = 'sent' AND sent_at IS NOT NULL`, oldMsg); n != 1 {
		t.Error("la trace d'envoi doit rester")
	}
	if n := e.count(t, `SELECT count(*) FROM session WHERE user_id = $1`, e.user); n != 1 {
		t.Errorf("sessions restantes = %d, attendu 1", n)
	}
	if n := e.count(t, `SELECT count(*) FROM password_reset WHERE user_id = $1`, e.user); n != 1 {
		t.Errorf("jetons restants = %d, attendu 1", n)
	}
	if n := e.count(t, `SELECT count(*) FROM otp_challenge WHERE practice_id = $1`, e.pc.ID); n != 1 {
		t.Errorf("codes restants = %d, attendu 1", n)
	}
	if n := e.count(t, `SELECT count(*) FROM audit_log WHERE action IN ('old', 'orphan_old')`); n != 0 {
		t.Error("anciennes lignes d'audit conservées")
	}
	if n := e.count(t, `SELECT count(*) FROM audit_log WHERE action IN ('recent', 'orphan_recent')`); n != 2 {
		t.Error("lignes d'audit récentes supprimées")
	}

	// Prévisualisation : seul le patient inactif est listé.
	pv, err := purge.Preview(ctx, e.actor)
	if err != nil {
		t.Fatal(err)
	}
	if pv.Total != 1 || len(pv.Candidates) != 1 || pv.Candidates[0].ID != inactive.ID {
		t.Fatalf("prévisualisation = %+v", pv)
	}

	// Activation : le patient inactif est anonymisé, pas les autres, et c'est idempotent.
	e.exec(t, `UPDATE practice SET patient_purge_enabled = true WHERE id = $1`, e.pc.ID)
	res, err = purge.RunOnce(ctx)
	if err != nil || res.PatientsAnonymized != 1 {
		t.Fatalf("RunOnce = %+v, %v", res, err)
	}
	if n := e.count(t, `SELECT count(*) FROM patient WHERE practice_id = $1 AND anonymized_at IS NOT NULL`, e.pc.ID); n != 1 {
		t.Errorf("fiches anonymisées = %d, attendu 1", n)
	}
	if n := e.count(t, `SELECT count(*) FROM audit_log WHERE action = 'patient.anonymized' AND actor = 'system' AND user_id IS NULL AND entity_id = $1`, inactive.ID); n != 1 {
		t.Error("purge non journalisée")
	}
	res, err = purge.RunOnce(ctx)
	if err != nil || res.PatientsAnonymized != 0 || res.MessagesScrubbed != 0 {
		t.Errorf("seconde exécution non idempotente : %+v, %v", res, err)
	}
	// Un jour plus tard (horloge avancée) : le patient « récent » est toujours conservé, 3 ans plus tard il ne l'est plus.
	e.clock.Advance(4 * 365 * 24 * time.Hour)
	res, err = purge.RunOnce(ctx)
	if err != nil || res.PatientsAnonymized != 2 {
		t.Errorf("après 4 ans : %+v, %v", res, err)
	}
}

func TestSettingsValidationAndAudit(t *testing.T) {
	t.Parallel()
	e := newPrivEnv(t)
	ctx := context.Background()
	bad := service.PrivacySettings{OutboxDays: 0, PatientYears: 99, AuditDays: 5}
	if err := e.priv.UpdateSettings(ctx, e.actor, bad); len(domain.FieldErrors(err)) != 3 {
		t.Fatalf("UpdateSettings = %v, attendu 3 erreurs", err)
	}
	ok := service.PrivacySettings{OutboxDays: 30, PatientYears: 5, AuditDays: 200, PurgeEnabled: true, Contact: " dpo@example.test "}
	if err := e.priv.UpdateSettings(ctx, e.actor, ok); err != nil {
		t.Fatal(err)
	}
	got, err := e.priv.Settings(ctx, e.actor)
	if err != nil || got.OutboxDays != 30 || got.PatientYears != 5 || !got.PurgeEnabled || got.Contact != "dpo@example.test" {
		t.Errorf("Settings = %+v, %v", got, err)
	}
	if n := e.count(t, `SELECT count(*) FROM audit_log WHERE action = 'privacy.settings_updated' AND practice_id = $1 AND meta::text NOT LIKE '%example.test%'`, e.pc.ID); n != 1 {
		t.Error("réglages non journalisés (ou PII dans le journal)")
	}
}

func TestExportContainsAllDataAndIsAudited(t *testing.T) {
	t.Parallel()
	e := newPrivEnv(t)
	ctx := context.Background()
	p := e.patient(t, mkFirst, mkLast, mkPhone, mkEmail)
	a := e.appt(t, p.ID, privNow.AddDate(0, 0, -3), "done")
	sent := privNow.AddDate(0, 0, -3)
	e.message(t, &a, mkEmail, "sent", &sent)
	e.exec(t, `INSERT INTO care_plan (practice_id, patient_id, practitioner_id, appointment_type_id, label, sessions_prescribed, frequency_per_week, start_date)
		VALUES ($1, $2, $3, $4, $5, 5, 2, '2026-09-01')`, e.pc.ID, p.ID, e.pr.ID, e.typ.ID, mkLabel)

	exp, err := e.pat.Export(ctx, e.actor, p.ID, "csv")
	if err != nil {
		t.Fatal(err)
	}
	if exp.Patient.LastName != mkLast || exp.Patient.Phone != mkPhone || exp.Patient.Address != mkAddress || exp.Patient.NotesAdmin != mkNote {
		t.Errorf("fiche exportée incomplète : %+v", exp.Patient)
	}
	if len(exp.Appointments) != 1 || exp.Appointments[0].Address != mkAddress || exp.Appointments[0].CancelReason != mkReason {
		t.Errorf("rendez-vous exportés : %+v", exp.Appointments)
	}
	if len(exp.CarePlans) != 1 || exp.CarePlans[0].Label != mkLabel || len(exp.Messages) != 1 || exp.Messages[0].Recipient != mkEmail {
		t.Errorf("séries/messages exportés : %+v / %+v", exp.CarePlans, exp.Messages)
	}
	if strings.Contains(exp.Messages[0].Template+exp.Messages[0].Status, "SECRETTOKEN") {
		t.Error("l'export ne doit pas contenir de jeton")
	}
	var buf bytes.Buffer
	if err := exp.WriteCSV(&buf); err != nil {
		t.Fatal(err)
	}
	csv := buf.String()
	if !strings.Contains(csv, mkLast) || !strings.Contains(csv, mkPhone) || strings.Count(csv, "\n") != 2 {
		t.Errorf("CSV inattendu : %q", csv)
	}
	if n := e.count(t, `SELECT count(*) FROM audit_log WHERE action = 'patient.exported' AND entity_id = $1 AND meta->>'format' = 'csv' AND user_id = $2`, p.ID, e.user); n != 1 {
		t.Error("export non journalisé")
	}
	// Autre cabinet : introuvable.
	other := testutil.NewPractice(t, e.db)
	if _, err := e.pat.Export(ctx, service.Actor{UserID: e.user, PracticeID: other.ID}, p.ID, "json"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("export inter-cabinet = %v, attendu ErrNotFound", err)
	}
}

func TestExportCSVNeutralizesFormulas(t *testing.T) {
	t.Parallel()
	exp := service.PatientExport{Patient: service.ExportedPatient{ID: "1", LastName: "=HYPERLINK(\"x\")", FirstName: "-2+3", Phone: "+33612345678", NotesAdmin: "@cmd"}}
	var buf bytes.Buffer
	if err := exp.WriteCSV(&buf); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{`"'=HYPERLINK(""x"")"`, "'-2+3", "+33612345678", "'@cmd"} {
		if !strings.Contains(out, want) {
			t.Errorf("CSV sans %s : %q", want, out)
		}
	}
	if strings.Contains(out, "'+33") {
		t.Error("un numéro de téléphone ne doit pas être préfixé")
	}
}

func TestAuditListIsPaginatedAndPerPractice(t *testing.T) {
	t.Parallel()
	e := newPrivEnv(t)
	ctx := context.Background()
	for i := 0; i < service.AuditPageSize+5; i++ {
		e.exec(t, `INSERT INTO audit_log (practice_id, actor, action, entity) VALUES ($1, 'user', 'patient_view', 'patient')`, e.pc.ID)
	}
	other := testutil.NewPractice(t, e.db)
	e.exec(t, `INSERT INTO audit_log (practice_id, actor, action, entity) VALUES ($1, 'user', 'autre_cabinet', 'patient')`, other.ID)
	audit := &service.Audit{Pool: e.db, Clock: e.clock}
	p1, err := audit.List(ctx, e.actor, "", 0)
	if err != nil || len(p1.Entries) != service.AuditPageSize || p1.Next == 0 {
		t.Fatalf("page 1: %d, next=%d, err=%v", len(p1.Entries), p1.Next, err)
	}
	p2, err := audit.List(ctx, e.actor, "", p1.Next)
	if err != nil || len(p2.Entries) != 5 || p2.Next != 0 {
		t.Fatalf("page 2: %d, next=%d, err=%v", len(p2.Entries), p2.Next, err)
	}
	for _, en := range append(p1.Entries, p2.Entries...) {
		if en.Action == "autre_cabinet" {
			t.Fatal("ligne d'un autre cabinet")
		}
	}
	if f, _ := audit.List(ctx, e.actor, "inexistante", 0); len(f.Entries) != 0 {
		t.Error("filtre par action sans effet")
	}
}
