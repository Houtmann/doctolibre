package outbox_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"doctolibre/internal/domain"
	"doctolibre/internal/outbox"
	"doctolibre/internal/store"
	"doctolibre/internal/store/sqlcgen"
	"doctolibre/internal/testutil"
)

type env struct {
	db       *pgxpool.Pool
	practice sqlcgen.Practice
	clock    *testutil.FixedClock
}

func newEnv(t *testing.T) env {
	t.Helper()
	db := testutil.DB(t)
	// Horloge en avance sur now() de la base : les messages créés sans RunAt sont échus.
	return env{
		db:       db,
		practice: testutil.NewPractice(t, db),
		clock:    testutil.NewFixedClock(time.Now().UTC().Add(time.Hour).Truncate(time.Microsecond)),
	}
}

func (e env) msg(key string) outbox.Msg {
	return outbox.Msg{
		PracticeID: e.practice.ID, Kind: outbox.KindEmail, Template: "confirmation",
		Payload: []byte(`{"n":1}`), Recipient: "patient@example.test", DedupKey: key,
	}
}

func (e env) enqueue(t *testing.T, m outbox.Msg) {
	t.Helper()
	err := store.WithTx(context.Background(), e.db, func(_ *sqlcgen.Queries, tx pgx.Tx) error {
		return outbox.Enqueue(context.Background(), tx, m)
	})
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
}

func (e env) enqueueN(t *testing.T, n int) {
	t.Helper()
	for i := range n {
		e.enqueue(t, e.msg(fmt.Sprintf("k-%d", i)))
	}
}

func (e env) appointment(t *testing.T) sqlcgen.Appointment {
	t.Helper()
	start := time.Date(2030, 3, 10, 10, 0, 0, 0, time.UTC)
	a, err := store.CreateAppointment(context.Background(), sqlcgen.New(e.db), store.NewAppointment{
		PracticeID:        e.practice.ID,
		PractitionerID:    testutil.NewPractitioner(t, e.db, e.practice.ID).ID,
		PatientID:         testutil.NewPatient(t, e.db, e.practice.ID).ID,
		AppointmentTypeID: testutil.NewAppointmentType(t, e.db, e.practice.ID).ID,
		Start:             start, End: start.Add(30 * time.Minute), Source: "staff",
	})
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func (e env) worker(d outbox.Dispatcher) *outbox.Worker {
	return &outbox.Worker{
		Pool: e.db, Dispatcher: d, Clock: e.clock,
		Logger:       slog.New(slog.NewTextHandler(io.Discard, nil)),
		PollInterval: time.Hour,
	}
}

type row struct {
	Status   string
	Attempts int
	RunAt    time.Time
	LastErr  *string
	Ref      *string
}

func (e env) row(t *testing.T, dedup string) row {
	t.Helper()
	var r row
	err := e.db.QueryRow(context.Background(),
		"SELECT status, attempts, run_at, last_error, provider_ref FROM outbox_message WHERE dedup_key = $1", dedup).
		Scan(&r.Status, &r.Attempts, &r.RunAt, &r.LastErr, &r.Ref)
	if err != nil {
		t.Fatalf("row %s: %v", dedup, err)
	}
	return r
}

func (e env) count(t *testing.T, status string) int {
	t.Helper()
	var n int
	if err := e.db.QueryRow(context.Background(), "SELECT count(*) FROM outbox_message WHERE status = $1", status).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

var okDispatcher = outbox.DispatcherFunc(func(context.Context, outbox.Msg) (string, error) { return "ref-1", nil })

func TestEnqueueRollback(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	boom := errors.New("boom")
	err := store.WithTx(context.Background(), e.db, func(_ *sqlcgen.Queries, tx pgx.Tx) error {
		if err := outbox.Enqueue(context.Background(), tx, e.msg("a")); err != nil {
			return err
		}
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
	var n int
	if err := e.db.QueryRow(context.Background(), "SELECT count(*) FROM outbox_message").Scan(&n); err != nil || n != 0 {
		t.Fatalf("rollback doit annuler le message: n=%d err=%v", n, err)
	}
}

func TestEnqueueDedupAndValidation(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.enqueue(t, e.msg("same"))
	e.enqueue(t, e.msg("same")) // ignoré
	e.enqueue(t, e.msg(""))     // sans clé : jamais dédupliqué
	e.enqueue(t, e.msg(""))
	var n int
	if err := e.db.QueryRow(context.Background(), "SELECT count(*) FROM outbox_message").Scan(&n); err != nil || n != 3 {
		t.Fatalf("n=%d err=%v, attendu 3", n, err)
	}

	bad := []outbox.Msg{
		{PracticeID: e.practice.ID, Kind: "fax", Template: "t", Recipient: "x"},
		{PracticeID: e.practice.ID, Kind: outbox.KindSMS, Recipient: "x"},
		{PracticeID: e.practice.ID, Kind: outbox.KindSMS, Template: "t"},
		{PracticeID: e.practice.ID, Kind: outbox.KindSMS, Template: "t", Recipient: "x", Payload: []byte("{")},
	}
	for i, m := range bad {
		err := store.WithTx(context.Background(), e.db, func(_ *sqlcgen.Queries, tx pgx.Tx) error {
			return outbox.Enqueue(context.Background(), tx, m)
		})
		var ve domain.ErrValidation
		if !errors.As(err, &ve) {
			t.Errorf("cas %d: err = %v, attendu ErrValidation", i, err)
		}
	}
}

func TestCancelForAppointment(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	a1, a2 := e.appointment(t), e.appointmentOther(t)

	for _, tc := range []struct {
		key string
		a   uuid.UUID
	}{{"a1-pending", a1.ID}, {"a1-sent", a1.ID}, {"a2-pending", a2.ID}} {
		m := e.msg(tc.key)
		m.AppointmentID = tc.a
		e.enqueue(t, m)
	}
	if _, err := e.db.Exec(context.Background(), "UPDATE outbox_message SET status = 'sent' WHERE dedup_key = 'a1-sent'"); err != nil {
		t.Fatal(err)
	}

	err := store.WithTx(context.Background(), e.db, func(_ *sqlcgen.Queries, tx pgx.Tx) error {
		return outbox.CancelForAppointment(context.Background(), tx, e.clock, a1.ID)
	})
	if err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]string{"a1-pending": "cancelled", "a1-sent": "sent", "a2-pending": "pending"} {
		if got := e.row(t, key).Status; got != want {
			t.Errorf("%s: status = %s, attendu %s", key, got, want)
		}
	}

	// ON DELETE CASCADE : supprimer le RDV supprime ses messages.
	if _, err := e.db.Exec(context.Background(), "DELETE FROM appointment WHERE id = $1", a2.ID); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := e.db.QueryRow(context.Background(), "SELECT count(*) FROM outbox_message WHERE dedup_key = 'a2-pending'").Scan(&n); err != nil || n != 0 {
		t.Fatalf("cascade: n=%d err=%v", n, err)
	}
}

// appointmentOther crée un second RDV (autre créneau, autre praticien).
func (e env) appointmentOther(t *testing.T) sqlcgen.Appointment {
	return e.appointment(t)
}

func TestConcurrentWorkersSendExactlyOnce(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	const total = 120
	e.enqueueN(t, total)

	var mu sync.Mutex
	sent := map[uuid.UUID]int{}
	d := outbox.DispatcherFunc(func(_ context.Context, m outbox.Msg) (string, error) {
		time.Sleep(time.Millisecond)
		mu.Lock()
		sent[m.ID]++
		mu.Unlock()
		return "ref", nil
	})

	var wg sync.WaitGroup
	for range 4 {
		w := e.worker(d)
		w.BatchSize = 5
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				n, err := w.RunOnce(context.Background())
				if err != nil {
					t.Errorf("RunOnce: %v", err)
					return
				}
				if n == 0 {
					return
				}
			}
		}()
	}
	wg.Wait()

	if len(sent) != total {
		t.Fatalf("%d messages envoyés, attendu %d", len(sent), total)
	}
	for id, n := range sent {
		if n != 1 {
			t.Errorf("message %s envoyé %d fois", id, n)
		}
	}
	if got := e.count(t, "sent"); got != total {
		t.Errorf("sent = %d, attendu %d", got, total)
	}
}

func TestRetryBackoffThenFailed(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.enqueue(t, e.msg("r"))
	var calls atomic.Int32
	w := e.worker(outbox.DispatcherFunc(func(context.Context, outbox.Msg) (string, error) {
		calls.Add(1)
		return "", errors.New("smtp 451 patient@example.test")
	}))
	ctx := context.Background()

	for i, delay := range outbox.DefaultBackoff {
		if n, err := w.RunOnce(ctx); err != nil || n != 1 {
			t.Fatalf("tentative %d: n=%d err=%v", i+1, n, err)
		}
		r := e.row(t, "r")
		if r.Status != "pending" || r.Attempts != i+1 || r.LastErr == nil {
			t.Fatalf("tentative %d: %+v", i+1, r)
		}
		if want := e.clock.Now().Add(delay); !r.RunAt.Equal(want) {
			t.Fatalf("tentative %d: run_at = %v, attendu %v", i+1, r.RunAt, want)
		}
		// Pas encore échu : rien à faire.
		if n, err := w.RunOnce(ctx); err != nil || n != 0 {
			t.Fatalf("avant échéance: n=%d err=%v", n, err)
		}
		e.clock.Advance(delay)
	}
	// 6e tentative : échec définitif.
	if n, err := w.RunOnce(ctx); err != nil || n != 1 {
		t.Fatalf("dernière tentative: n=%d err=%v", n, err)
	}
	if r := e.row(t, "r"); r.Status != "failed" || r.Attempts != len(outbox.DefaultBackoff)+1 {
		t.Fatalf("final: %+v", r)
	}
	if got := calls.Load(); int(got) != len(outbox.DefaultBackoff)+1 {
		t.Fatalf("calls = %d", got)
	}
	e.clock.Advance(24 * time.Hour)
	if n, _ := w.RunOnce(ctx); n != 0 {
		t.Fatalf("un message failed ne doit plus être repris, n=%d", n)
	}
}

func TestPermanentErrorFailsImmediately(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.enqueue(t, e.msg("p"))
	w := e.worker(outbox.DispatcherFunc(func(context.Context, outbox.Msg) (string, error) {
		return "", fmt.Errorf("wrap: %w", outbox.Permanent(errors.New("adresse invalide")))
	}))
	if _, err := w.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if r := e.row(t, "p"); r.Status != "failed" || r.Attempts != 1 || r.LastErr == nil {
		t.Fatalf("%+v", r)
	}
}

func TestSuccessRecordsProviderRef(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.enqueue(t, e.msg("s"))
	if _, err := e.worker(okDispatcher).RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if r := e.row(t, "s"); r.Status != "sent" || r.Ref == nil || *r.Ref != "ref-1" {
		t.Fatalf("%+v", r)
	}
}

func TestRunAtInFutureNotClaimed(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	m := e.msg("future")
	m.RunAt = e.clock.Now().Add(time.Hour)
	e.enqueue(t, m)
	w := e.worker(okDispatcher)
	if n, err := w.RunOnce(context.Background()); err != nil || n != 0 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	e.clock.Advance(time.Hour)
	if n, err := w.RunOnce(context.Background()); err != nil || n != 1 {
		t.Fatalf("n=%d err=%v", n, err)
	}
}

func TestCrashRecovery(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.enqueue(t, e.msg("crash"))
	e.enqueue(t, e.msg("busy"))

	// Simule deux workers morts : l'un il y a 11 min, l'autre il y a 5 min.
	for key, age := range map[string]time.Duration{"crash": 11 * time.Minute, "busy": 5 * time.Minute} {
		_, err := e.db.Exec(context.Background(),
			"UPDATE outbox_message SET status = 'sending', attempts = 1, updated_at = $2 WHERE dedup_key = $1",
			key, e.clock.Now().Add(-age))
		if err != nil {
			t.Fatal(err)
		}
	}
	var sentIDs sync.Map
	w := e.worker(outbox.DispatcherFunc(func(_ context.Context, m outbox.Msg) (string, error) {
		sentIDs.Store(m.DedupKey, true)
		return "r", nil
	}))
	if n, err := w.RunOnce(context.Background()); err != nil || n != 1 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	if r := e.row(t, "crash"); r.Status != "sent" || r.Attempts != 2 {
		t.Fatalf("crash: %+v", r)
	}
	if r := e.row(t, "busy"); r.Status != "sending" {
		t.Fatalf("busy ne doit pas être repris trop tôt: %+v", r)
	}
	if _, ok := sentIDs.Load("busy"); ok {
		t.Fatal("busy envoyé")
	}
}

func TestRunWakesOnNotifyAndShutsDownGracefully(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	started := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	w := e.worker(outbox.DispatcherFunc(func(_ context.Context, _ outbox.Msg) (string, error) {
		once.Do(func() { close(started) })
		<-release
		return "r", nil
	}))
	w.PollInterval = time.Hour // seul LISTEN/NOTIFY peut réveiller le worker

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- w.Run(ctx) }()

	time.Sleep(200 * time.Millisecond) // laisse le LISTEN s'établir
	e.enqueue(t, e.msg("m1"))
	e.enqueue(t, e.msg("m2")) // même lot, jamais envoyé : doit être rendu à la file

	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("le worker n'a pas été réveillé par NOTIFY")
	}
	cancel()
	select {
	case err := <-done:
		t.Fatalf("Run a rendu la main pendant un envoi (err=%v)", err)
	case <-time.After(200 * time.Millisecond):
	}
	close(release)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run ne s'arrête pas")
	}

	if got := e.count(t, "sent"); got != 1 {
		t.Fatalf("sent = %d, attendu 1 (l'envoi en cours est terminé)", got)
	}
	var attempts int
	if err := e.db.QueryRow(context.Background(), "SELECT attempts FROM outbox_message WHERE status = 'pending'").Scan(&attempts); err != nil || attempts != 0 {
		t.Fatalf("le message non envoyé doit être rendu sans tentative comptée: attempts=%d err=%v", attempts, err)
	}
}

func TestQueueStatsResendList(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.enqueueN(t, 3)
	if _, err := e.db.Exec(context.Background(), "UPDATE outbox_message SET status = 'failed' WHERE dedup_key = 'k-0'"); err != nil {
		t.Fatal(err)
	}
	q := outbox.Queue{Pool: e.db, Clock: e.clock}

	st, err := q.OutboxStats(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if st.ByStatus["pending"] != 2 || st.ByStatus["failed"] != 1 || st.OldestPending <= 0 {
		t.Fatalf("stats = %+v", st)
	}

	rows, err := q.ListRecent(context.Background(), e.practice.ID, 2)
	if err != nil || len(rows) != 2 {
		t.Fatalf("rows=%d err=%v", len(rows), err)
	}

	var failedID uuid.UUID
	if err := e.db.QueryRow(context.Background(), "SELECT id FROM outbox_message WHERE status = 'failed'").Scan(&failedID); err != nil {
		t.Fatal(err)
	}
	if err := q.Resend(context.Background(), uuid.New(), failedID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("autre cabinet: err = %v", err)
	}
	if err := q.Resend(context.Background(), e.practice.ID, failedID); err != nil {
		t.Fatal(err)
	}
	if r := e.row(t, "k-0"); r.Status != "pending" || r.Attempts != 0 {
		t.Fatalf("%+v", r)
	}
	if err := q.Resend(context.Background(), e.practice.ID, failedID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("déjà pending: err = %v", err)
	}
}

func TestStaleClaimNotSentTwice(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	for i, key := range []string{"first", "second"} {
		m := e.msg(key)
		m.RunAt = e.clock.Now().Add(-time.Duration(2-i) * time.Hour) // ordre de claim déterministe
		e.enqueue(t, m)
	}
	var mu sync.Mutex
	sends := map[string]int{}
	record := func(m outbox.Msg) { mu.Lock(); sends[m.DedupKey]++; mu.Unlock() }

	b := e.worker(outbox.DispatcherFunc(func(_ context.Context, m outbox.Msg) (string, error) {
		record(m)
		return "b", nil
	}))
	var aCalls []string
	a := e.worker(outbox.DispatcherFunc(func(_ context.Context, m outbox.Msg) (string, error) {
		aCalls = append(aCalls, m.DedupKey)
		if m.DedupKey == "first" {
			// Envoi lent : le temps passe au-delà de StuckAfter, B récupère tout le lot de A.
			e.clock.Advance(11 * time.Minute)
			if n, err := b.RunOnce(context.Background()); err != nil || n != 2 {
				t.Errorf("B: n=%d err=%v", n, err)
			}
		} else {
			record(m)
		}
		return "a", nil
	}))
	if n, err := a.RunOnce(context.Background()); err != nil || n != 2 {
		t.Fatalf("A: n=%d err=%v", n, err)
	}
	if len(aCalls) != 1 || aCalls[0] != "first" {
		t.Fatalf("A ne doit pas envoyer le message récupéré par B: %v", aCalls)
	}
	if sends["second"] != 1 {
		t.Fatalf("second envoyé %d fois, attendu 1", sends["second"])
	}
	if got := e.count(t, "sent"); got != 2 {
		t.Fatalf("sent = %d", got)
	}
}

func TestInvalidTimeoutsRejected(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	w := e.worker(okDispatcher)
	w.StuckAfter, w.SendTimeout = time.Minute, time.Minute
	if _, err := w.RunOnce(context.Background()); err == nil {
		t.Fatal("StuckAfter < 2 x SendTimeout doit être refusé")
	}
}
