package main

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"doctolibre/internal/config"
	"doctolibre/internal/outbox"
	"doctolibre/internal/testutil"
)

func discardLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func TestStartWorkerOff(t *testing.T) {
	t.Parallel()
	db := testutil.DB(t)
	called := false
	d := outbox.DispatcherFunc(func(context.Context, outbox.Msg) (string, error) { called = true; return "", nil })
	started, wait := startWorker(context.Background(), config.Config{WorkerEnabled: false}, db, d, discardLogger())
	wait()
	if started || called {
		t.Fatalf("worker démarré malgré DOCTOLIBRE_WORKER=off (started=%v)", started)
	}
}

func TestStartWorkerRunsAndStops(t *testing.T) {
	t.Parallel()
	db := testutil.DB(t)
	practice := testutil.NewPractice(t, db)
	ctx := context.Background()

	var mu sync.Mutex
	var got []string
	d := newDispatcher(
		outbox.DispatcherFunc(func(_ context.Context, m outbox.Msg) (string, error) {
			mu.Lock()
			defer mu.Unlock()
			got = append(got, "email:"+m.ID.String())
			return "ref", nil
		}), nil, discardLogger())

	wctx, cancel := context.WithCancel(ctx)
	started, wait := startWorker(wctx, config.Config{WorkerEnabled: true}, db, d, discardLogger())
	if !started {
		t.Fatal("worker non démarré")
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := outbox.Enqueue(ctx, tx, outbox.Msg{PracticeID: practice.ID, Kind: outbox.KindEmail, Template: "t", Recipient: "a@b.fr", DedupKey: uuid.NewString()}); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		var n int
		if err := db.QueryRow(ctx, `SELECT count(*) FROM outbox_message WHERE status='sent'`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("message non envoyé par le worker")
		}
		time.Sleep(50 * time.Millisecond)
	}
	cancel()
	done := make(chan struct{})
	go func() { wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(15 * time.Second):
		t.Fatal("le worker ne s'arrête pas")
	}
	mu.Lock()
	defer mu.Unlock()
	if len(got) != 1 || !strings.HasPrefix(got[0], "email:") {
		t.Errorf("dispatch email inattendu: %v", got)
	}
}

func TestDispatcherRouting(t *testing.T) {
	t.Parallel()
	var email, sms int
	d := newDispatcher(
		outbox.DispatcherFunc(func(context.Context, outbox.Msg) (string, error) { email++; return "", nil }),
		outbox.DispatcherFunc(func(context.Context, outbox.Msg) (string, error) { sms++; return "", nil }),
		discardLogger())
	ctx := context.Background()
	_, _ = d.Send(ctx, outbox.Msg{Kind: outbox.KindEmail})
	_, _ = d.Send(ctx, outbox.Msg{Kind: outbox.KindSMS})
	_, _ = d.Send(ctx, outbox.Msg{Kind: outbox.KindSMS})
	if email != 1 || sms != 2 {
		t.Errorf("email=%d sms=%d", email, sms)
	}
	if _, err := d.Send(ctx, outbox.Msg{Kind: "pigeon"}); err == nil || !outbox.IsPermanent(err) {
		t.Errorf("kind inconnu: %v", err)
	}
}

func TestLogDispatcherNoPII(t *testing.T) {
	t.Parallel()
	var sb strings.Builder
	logger := slog.New(slog.NewTextHandler(&sb, nil))
	d := newDispatcher(nil, nil, logger)
	id := uuid.New()
	if _, err := d.Send(context.Background(), outbox.Msg{ID: id, Kind: outbox.KindSMS, Recipient: "+33612345678", Payload: []byte(`{"name":"Jean"}`)}); err != nil {
		t.Fatal(err)
	}
	out := sb.String()
	if !strings.Contains(out, id.String()) || !strings.Contains(out, "sms") {
		t.Errorf("ID/kind absents: %s", out)
	}
	if strings.Contains(out, "+3361") || strings.Contains(out, "Jean") {
		t.Errorf("données personnelles loguées: %s", out)
	}
}
