package web_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"doctolibre/internal/auth"
	"doctolibre/internal/config"
	"doctolibre/internal/domain"
	"doctolibre/internal/outbox"
	"doctolibre/internal/testutil"
	"doctolibre/internal/web"
)

func newMessagesApp(t *testing.T) (h http.Handler, db *pgxpool.Pool, practiceID uuid.UUID) {
	t.Helper()
	db = testutil.DB(t)
	u, err := auth.CreateUser(context.Background(), db, auth.CreateUserParams{
		Email: userEmail, Role: domain.RoleAdmin, PracticeName: "Cabinet test", Password: pw,
	})
	if err != nil {
		t.Fatal(err)
	}
	clock := testutil.NewFixedClock(time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC))
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	h = web.Routes(web.Deps{
		Config:   config.Config{Env: config.EnvDev},
		Logger:   logger,
		DB:       db,
		Auth:     &auth.Service{Pool: db, Clock: clock, Logger: logger},
		Messages: outbox.Queue{Pool: db, Clock: clock},
	})
	return h, db, u.PracticeID
}

func enqueue(t *testing.T, db *pgxpool.Pool, practiceID uuid.UUID, kind, recipient, status, lastErr string) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	key := uuid.NewString()
	if err := outbox.Enqueue(ctx, tx, outbox.Msg{PracticeID: practiceID, Kind: kind, Template: "t", Recipient: recipient, DedupKey: key}); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	var id uuid.UUID
	if err := db.QueryRow(ctx,
		`UPDATE outbox_message SET status=$1, attempts=3, last_error=NULLIF($2,'') WHERE dedup_key=$3 RETURNING id`,
		status, lastErr, key).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestAdminMessages(t *testing.T) {
	t.Parallel()
	h, db, practiceID := newMessagesApp(t)

	c := newClient(t, h, "192.0.2.1")
	rec := c.do("GET", "/admin/messages", nil)
	if rec.Code != 303 || rec.Header().Get("Location") != "/admin/login?next=%2Fadmin%2Fmessages" {
		t.Fatalf("sans session: %d %q", rec.Code, rec.Header().Get("Location"))
	}
	if rec := c.do("POST", "/admin/messages/"+uuid.NewString()+"/resend", url.Values{}); rec.Code != 303 {
		t.Errorf("renvoi sans session: %d", rec.Code)
	}

	enqueue(t, db, practiceID, "sms", "+33612345678", "sent", "")
	enqueue(t, db, practiceID, "email", "jean@exemple.fr", "failed", "refusé pour jean@exemple.fr")
	otherPractice := testutil.NewPractice(t, db)
	enqueue(t, db, otherPractice.ID, "sms", "+33699999999", "sent", "")

	c.login(userEmail, pw, "")
	rec = c.do("GET", "/admin/messages", nil)
	body := rec.Body.String()
	if rec.Code != 200 {
		t.Fatalf("liste: %d", rec.Code)
	}
	for _, want := range []string{"&#43;33•••••••78", "j•••@exemple.fr", "Renvoyer", "refusé pour j•••@exemple.fr"} {
		if !strings.Contains(body, want) {
			t.Errorf("fragment %q absent", want)
		}
	}
	for _, leak := range []string{"+33612345678", "jean@exemple.fr", "&#43;33699999999", "•99"} {
		if strings.Contains(body, leak) {
			t.Errorf("fuite ou message d'un autre cabinet: %q", leak)
		}
	}
}

func TestAdminMessagesResend(t *testing.T) {
	t.Parallel()
	h, db, practiceID := newMessagesApp(t)
	failed := enqueue(t, db, practiceID, "email", "jean@exemple.fr", "failed", "boom")
	sent := enqueue(t, db, practiceID, "email", "jean@exemple.fr", "sent", "")
	other := enqueue(t, db, testutil.NewPractice(t, db).ID, "email", "x@exemple.fr", "failed", "boom")

	c := newClient(t, h, "192.0.2.1")
	c.login(userEmail, pw, "")

	// Sans CSRF : refusé.
	if rec := c.do("POST", "/admin/messages/"+failed.String()+"/resend", nil); rec.Code != 403 {
		t.Errorf("sans CSRF: %d", rec.Code)
	}
	status := func(id uuid.UUID) string {
		var s string
		if err := db.QueryRow(context.Background(), `SELECT status FROM outbox_message WHERE id=$1`, id).Scan(&s); err != nil {
			t.Fatal(err)
		}
		return s
	}
	rec := c.do("POST", "/admin/messages/"+failed.String()+"/resend", url.Values{})
	if rec.Code != 303 || rec.Header().Get("Location") != "/admin/messages" {
		t.Fatalf("renvoi: %d %q", rec.Code, rec.Header().Get("Location"))
	}
	if got := status(failed); got != "pending" {
		t.Errorf("statut après renvoi = %q", got)
	}
	// Message d'un autre cabinet, déjà envoyé, inconnu, id invalide : 404, rien ne change.
	for _, id := range []string{other.String(), sent.String(), uuid.NewString(), "nope"} {
		if rec := c.do("POST", "/admin/messages/"+id+"/resend", url.Values{}); rec.Code != 404 {
			t.Errorf("renvoi %s: %d", id, rec.Code)
		}
	}
	if got := status(other); got != "failed" {
		t.Errorf("message d'un autre cabinet modifié: %q", got)
	}
	if got := status(sent); got != "sent" {
		t.Errorf("message envoyé modifié: %q", got)
	}
}
