package service_test

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"doctolibre/internal/notify"
	"doctolibre/internal/notify/sms"
	"doctolibre/internal/service"
)

const baseURL = "https://rdv.example.fr"

type outMsg struct {
	Kind, Template, Recipient, DedupKey, Status string
	RunAt                                       time.Time
	Payload                                     string
}

func (m outMsg) body(t *testing.T) string {
	t.Helper()
	var p struct {
		Body string `json:"body"`
	}
	if err := json.Unmarshal([]byte(m.Payload), &p); err != nil {
		t.Fatal(err)
	}
	return p.Body
}

// outMsgs retourne les messages outbox du cabinet (hors OTP) triés par création.
func outMsgs(t *testing.T, db *pgxpool.Pool, practiceID uuid.UUID) []outMsg {
	t.Helper()
	rows, err := db.Query(context.Background(),
		`SELECT kind, template, recipient, coalesce(dedup_key, ''), status, run_at, payload::text
		 FROM outbox_message WHERE practice_id = $1 AND appointment_id IS NOT NULL ORDER BY created_at, id`, practiceID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []outMsg
	for rows.Next() {
		var m outMsg
		if err := rows.Scan(&m.Kind, &m.Template, &m.Recipient, &m.DedupKey, &m.Status, &m.RunAt, &m.Payload); err != nil {
			t.Fatal(err)
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func only(ms []outMsg, template string) []outMsg {
	var out []outMsg
	for _, m := range ms {
		if m.Template == template {
			out = append(out, m)
		}
	}
	return out
}

func mustExec(t *testing.T, db *pgxpool.Pool, sql string, args ...any) {
	t.Helper()
	if _, err := db.Exec(context.Background(), sql, args...); err != nil {
		t.Fatal(err)
	}
}

// bookOnline réserve en ligne (code SMS) un patient joignable par téléphone.
func bookOnline(t *testing.T, e *bookingEnv, start time.Time) service.BookResult {
	t.Helper()
	e.svc.BaseURL = baseURL
	in := e.start(t, start, patient("Léa", "Durand", "06 11 11 11 11"), "+33611111111")
	res, err := e.svc.Book(context.Background(), in)
	if err != nil {
		t.Fatalf("Book: %v", err)
	}
	// updated_at vient de now() côté base ; les tests pilotent l'horloge applicative.
	mustExec(t, e.db, "UPDATE appointment SET updated_at = $1 WHERE id = $2", e.clock.Now(), res.Appointment.ID)
	return res
}

func (e *bookingEnv) reminders() *service.Reminders {
	return &service.Reminders{Pool: e.db, Clock: e.clock}
}

func TestBookEnqueuesConfirmationInTransaction(t *testing.T) {
	t.Parallel()
	e := newBookingEnv(t, thursday)
	// Le patient a aussi un email, mais seul le canal du code (SMS) est vérifié.
	e.svc.BaseURL = baseURL
	p := patient("Léa", "Durand", "06 11 11 11 11")
	p.Email = "lea@example.org"
	in := e.start(t, tuesday9, p, "+33611111111")
	res, err := e.svc.Book(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	ms := outMsgs(t, e.db, e.pc.ID)
	if len(ms) != 1 {
		t.Fatalf("%d messages, attendu 1 (SMS, canal vérifié) : %+v", len(ms), ms)
	}
	m := ms[0]
	if m.Kind != "sms" || m.Template != "confirmation" || m.Recipient != "+33611111111" || m.Status != "pending" {
		t.Errorf("message = %+v", m)
	}
	if want := "confirmation:" + res.Appointment.ID.String() + ":sms"; m.DedupKey != want {
		t.Errorf("dedup_key = %q, attendu %q", m.DedupKey, want)
	}
	if b := m.body(t); !strings.Contains(b, baseURL+"/r/"+res.Token) {
		t.Errorf("le SMS ne contient pas le lien de gestion : %q", b)
	}
	if n, ucs2 := sms.Segments(m.body(t)); n != 1 || ucs2 {
		t.Errorf("SMS de %d segment(s), UCS-2=%v : %q", n, ucs2, m.body(t))
	}
}

func TestBookEmailChannelSendsEmail(t *testing.T) {
	t.Parallel()
	e := newBookingEnv(t, thursday)
	e.svc.BaseURL = baseURL
	p := patient("Léa", "Durand", "06 11 11 11 11")
	p.Email = "lea@example.org"
	vin := e.verifyInput(tuesday9, p)
	vin.Channel = "email"
	v, err := e.svc.StartVerification(context.Background(), vin)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Book(context.Background(), service.BookInput{VerifyInput: vin, ChallengeID: v.ChallengeID, Code: e.otp.code("lea@example.org")}); err != nil {
		t.Fatal(err)
	}
	ms := outMsgs(t, e.db, e.pc.ID)
	if len(ms) != 1 || ms[0].Kind != "email" || ms[0].Recipient != "lea@example.org" {
		t.Fatalf("messages = %+v, attendu 1 email", ms)
	}
	// Le payload est rendable par le dispatcher email.
	tpl, err := notify.NewTemplates()
	if err != nil {
		t.Fatal(err)
	}
	var data map[string]any
	if err := json.Unmarshal([]byte(ms[0].Payload), &data); err != nil {
		t.Fatal(err)
	}
	em, err := tpl.Render("confirmation", data)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(em.Text, "mardi 20 octobre à 9h00") {
		t.Errorf("texte : %q", em.Text)
	}
}

func TestReminderDayBefore(t *testing.T) {
	t.Parallel()
	e := newBookingEnv(t, thursday) // jeudi 15 octobre 2026, 9 h UTC
	res := bookOnline(t, e, tuesday9)
	apptID := res.Appointment.ID
	ctx := context.Background()

	// Mardi 9 h : rappel prévu lundi 19 octobre 18 h. Avant, rien.
	e.clock.Advance(time.Date(2026, 10, 19, 17, 55, 0, 0, paris).Sub(thursday))
	if n, err := e.reminders().RunOnce(ctx); err != nil || n != 0 {
		t.Fatalf("avant l'heure : n=%d err=%v", n, err)
	}
	e.clock.Advance(10 * time.Minute) // 18 h 05
	n, err := e.reminders().RunOnce(ctx)
	if err != nil || n != 1 {
		t.Fatalf("RunOnce : n=%d err=%v", n, err)
	}
	rem := only(outMsgs(t, e.db, e.pc.ID), "reminder")
	if len(rem) != 1 {
		t.Fatalf("%d rappels", len(rem))
	}
	r := rem[0]
	if r.Kind != "sms" || r.Status != "pending" {
		t.Errorf("rappel = %+v", r)
	}
	if want := time.Date(2026, 10, 19, 18, 0, 0, 0, paris); !r.RunAt.Equal(want) {
		t.Errorf("run_at = %v, attendu %v", r.RunAt, want)
	}
	if want := "reminder-24h:" + apptID.String() + ":sms:" + itoa(tuesday9.Unix()); r.DedupKey != want {
		t.Errorf("dedup_key = %q, attendu %q", r.DedupKey, want)
	}
	if strings.Contains(r.Payload, "/r/") {
		t.Errorf("le rappel ne doit pas contenir de lien (jeton non stocké en clair) : %s", r.Payload)
	}
	if b := r.body(t); !strings.Contains(b, "20/10") || !strings.Contains(b, "9h00") {
		t.Errorf("corps = %q", b)
	}

	// Idempotent : un second tour (même dans un autre processus) ne crée rien.
	e.clock.Advance(5 * time.Minute)
	if n, err := e.reminders().RunOnce(ctx); err != nil || n != 0 {
		t.Errorf("second tour : n=%d err=%v", n, err)
	}
	if got := len(only(outMsgs(t, e.db, e.pc.ID), "reminder")); got != 1 {
		t.Errorf("%d rappels après deux tours", got)
	}
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }
