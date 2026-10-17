package notify_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"doctolibre/internal/config"
	"doctolibre/internal/domain"
	"doctolibre/internal/notify"
	"doctolibre/internal/outbox"
	"doctolibre/internal/secretbox"
	"doctolibre/internal/store/sqlcgen"
	"doctolibre/internal/testutil"
)

type fakeEmail struct {
	mu   sync.Mutex
	host string
	sent []string
}

func (f *fakeEmail) Send(_ context.Context, e notify.Email) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, e.To)
	return "ref-" + f.host, nil
}

type fakeSMS struct {
	mu   sync.Mutex
	user string
	sent int
}

func (f *fakeSMS) Send(_ context.Context, _ notify.SMS) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent++
	return "sms-" + f.user, nil
}

// serverCalls compte les envois du fournisseur du serveur (variables d'environnement).
type serverCalls struct {
	mu sync.Mutex
	n  int
}

func (s *serverCalls) Send(_ context.Context, _ outbox.Msg) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.n++
	return "server", nil
}

type transportEnv struct {
	db      *pgxpool.Pool
	t       *notify.Transports
	d       *notify.PracticeDispatcher
	server  *serverCalls
	emails  map[string]*fakeEmail // par hôte SMTP
	builds  int
	smsUser map[string]*fakeSMS
}

func newTransportEnv(t *testing.T) *transportEnv {
	t.Helper()
	db := testutil.DB(t)
	box, err := secretbox.New(testutil.ManageSecret)
	if err != nil {
		t.Fatal(err)
	}
	e := &transportEnv{db: db, server: &serverCalls{}, emails: map[string]*fakeEmail{}, smsUser: map[string]*fakeSMS{}}
	e.t = &notify.Transports{
		Pool: db, Box: box, Clock: domain.SystemClock{},
		NewEmail: func(cfg config.EmailConfig, _ bool) (notify.EmailSender, error) {
			e.builds++
			f := &fakeEmail{host: cfg.SMTPHost}
			e.emails[cfg.SMTPHost] = f
			return f, nil
		},
		NewSMS: func(cfg config.SMSConfig, _ bool) (notify.SMSSender, error) {
			f := &fakeSMS{user: cfg.AndroidUser}
			e.smsUser[cfg.AndroidUser] = f
			return f, nil
		},
	}
	e.d = &notify.PracticeDispatcher{Server: e.server, Transports: e.t}
	return e
}

func (e *transportEnv) set(t *testing.T, practice uuid.UUID, kind string, tr notify.Transport) {
	t.Helper()
	settings, secrets, err := e.t.Encode(practice, kind, tr)
	if err != nil {
		t.Fatal(err)
	}
	if err := sqlcgen.New(e.db).UpsertNotifyTransport(context.Background(), sqlcgen.UpsertNotifyTransportParams{
		PracticeID: practice, Kind: kind, Mode: tr.Mode, Driver: pgtype.Text{String: tr.Driver, Valid: tr.Driver != ""},
		Settings: settings, Secrets: secrets,
	}); err != nil {
		t.Fatal(err)
	}
}

func smtpTransport(host string) notify.Transport {
	return notify.Transport{
		Mode: notify.ModeCustom, Driver: "smtp",
		Settings: map[string]string{"host": host, "from": "Cabinet <rdv@exemple.fr>", "user": "u-" + host},
		Secrets:  map[string]string{"password": "pw-secret-" + host},
	}
}

func emailMsg(practice uuid.UUID, to string) outbox.Msg {
	return outbox.Msg{
		ID: uuid.New(), PracticeID: practice, Kind: outbox.KindEmail, Template: "otp", Recipient: to,
		Payload: json.RawMessage(`{"code":"123456","valid_for":"10 minutes","practice_name":"Cabinet"}`),
	}
}

func smsMsg(practice uuid.UUID) outbox.Msg {
	return outbox.Msg{ID: uuid.New(), PracticeID: practice, Kind: outbox.KindSMS, Recipient: "+33612345678", Payload: json.RawMessage(`{"body":"Bonjour"}`)}
}

func TestDispatcherWithoutRowUsesServer(t *testing.T) {
	t.Parallel()
	e := newTransportEnv(t)
	pc := testutil.NewPractice(t, e.db)
	ctx := context.Background()
	if ref, err := e.d.Send(ctx, emailMsg(pc.ID, "a@example.test")); err != nil || ref != "server" {
		t.Fatalf("email: %q, %v", ref, err)
	}
	if ref, err := e.d.Send(ctx, smsMsg(pc.ID)); err != nil || ref != "server" {
		t.Fatalf("sms: %q, %v", ref, err)
	}
	// mode « server » explicite : idem.
	e.set(t, pc.ID, notify.KindEmail, notify.Transport{Mode: notify.ModeServer})
	if ref, _ := e.d.Send(ctx, emailMsg(pc.ID, "a@example.test")); ref != "server" || e.server.n != 3 {
		t.Errorf("ref=%q appels serveur=%d", ref, e.server.n)
	}
}

func TestDispatcherRoutesPerPractice(t *testing.T) {
	t.Parallel()
	e := newTransportEnv(t)
	a, b := testutil.NewPractice(t, e.db), testutil.NewPractice(t, e.db)
	ctx := context.Background()
	hostA := "smtp-a-" + a.ID.String()
	e.set(t, a.ID, notify.KindEmail, smtpTransport(hostA))
	e.set(t, a.ID, notify.KindSMS, notify.Transport{
		Mode: notify.ModeCustom, Driver: "android", Settings: map[string]string{"user": "ua"}, Secrets: map[string]string{"password": "x"},
	})

	if ref, err := e.d.Send(ctx, emailMsg(a.ID, "p@example.test")); err != nil || ref != "ref-"+hostA {
		t.Fatalf("email A: %q, %v", ref, err)
	}
	if ref, err := e.d.Send(ctx, emailMsg(b.ID, "p@example.test")); err != nil || ref != "server" {
		t.Fatalf("email B: %q, %v", ref, err)
	}
	if ref, err := e.d.Send(ctx, smsMsg(a.ID)); err != nil || ref != "sms-ua" {
		t.Fatalf("sms A: %q, %v", ref, err)
	}
	if ref, err := e.d.Send(ctx, smsMsg(b.ID)); err != nil || ref != "server" {
		t.Fatalf("sms B: %q, %v", ref, err)
	}
	if got := e.emails[hostA].sent; len(got) != 1 || got[0] != "p@example.test" {
		t.Errorf("destinataires du faux SMTP = %v", got)
	}
	if e.server.n != 2 {
		t.Errorf("appels serveur = %d, attendu 2 (cabinet B)", e.server.n)
	}
}

func TestDispatcherPicksUpChangesWithoutRestart(t *testing.T) {
	t.Parallel()
	e := newTransportEnv(t)
	pc := testutil.NewPractice(t, e.db)
	ctx := context.Background()
	h1, h2 := "smtp-1-"+pc.ID.String(), "smtp-2-"+pc.ID.String()
	e.set(t, pc.ID, notify.KindEmail, smtpTransport(h1))
	for range 2 {
		if ref, err := e.d.Send(ctx, emailMsg(pc.ID, "p@example.test")); err != nil || ref != "ref-"+h1 {
			t.Fatalf("avant : %q, %v", ref, err)
		}
	}
	if e.builds != 1 {
		t.Errorf("le sender doit être mis en cache : %d constructions", e.builds)
	}
	e.set(t, pc.ID, notify.KindEmail, smtpTransport(h2))
	if ref, err := e.d.Send(ctx, emailMsg(pc.ID, "p@example.test")); err != nil || ref != "ref-"+h2 {
		t.Fatalf("après modification : %q, %v", ref, err)
	}
}

func TestDisabledChannelCancelsQueuedMessages(t *testing.T) {
	t.Parallel()
	e := newTransportEnv(t)
	pc := testutil.NewPractice(t, e.db)
	ctx := context.Background()
	// Un message déjà en file avant la désactivation.
	tx, err := e.db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	m := emailMsg(pc.ID, "p@example.test")
	if err := outbox.Enqueue(ctx, tx, m); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	e.set(t, pc.ID, notify.KindEmail, notify.Transport{Mode: notify.ModeDisabled})

	if _, err := e.d.Send(ctx, m); !outbox.IsCancelled(err) || !errors.Is(err, notify.ErrChannelDisabled) || outbox.IsPermanent(err) {
		t.Fatalf("Send = %v", err)
	}
	w := &outbox.Worker{Pool: e.db, Dispatcher: e.d, Clock: domain.SystemClock{}}
	for range 5 { // le lot peut contenir des messages d'autres tests parallèles
		if n, err := w.RunOnce(ctx); err != nil || n < 20 {
			if err != nil {
				t.Fatal(err)
			}
			break
		}
	}
	var status, lastErr string
	var attempts int
	if err := e.db.QueryRow(ctx, `SELECT status, coalesce(last_error, ''), attempts FROM outbox_message WHERE practice_id = $1`, pc.ID).Scan(&status, &lastErr, &attempts); err != nil {
		t.Fatal(err)
	}
	if status != "cancelled" || lastErr != "channel_disabled" || attempts != 1 {
		t.Errorf("status=%q last_error=%q attempts=%d", status, lastErr, attempts)
	}
	if e.server.n != 0 || e.builds != 0 {
		t.Errorf("aucun appel réseau attendu : serveur=%d constructions=%d", e.server.n, e.builds)
	}
}

func TestSecretsEncryptedAndBoundToPracticeAndKind(t *testing.T) {
	t.Parallel()
	e := newTransportEnv(t)
	a, b := testutil.NewPractice(t, e.db), testutil.NewPractice(t, e.db)
	ctx := context.Background()
	tr := smtpTransport("smtp.example.test")
	e.set(t, a.ID, notify.KindEmail, tr)

	var raw []byte
	var settings string
	if err := e.db.QueryRow(ctx, `SELECT secrets, settings::text FROM practice_notify_transport WHERE practice_id = $1`, a.ID).Scan(&raw, &settings); err != nil {
		t.Fatal(err)
	}
	if len(raw) == 0 || bytes.Contains(raw, []byte("pw-secret")) || bytes.Contains([]byte(settings), []byte("pw-secret")) {
		t.Fatalf("la colonne contient le secret en clair (secrets=%q settings=%q)", raw, settings)
	}
	q := sqlcgen.New(e.db)
	got, err := e.t.Load(ctx, q, a.ID, notify.KindEmail)
	if err != nil || got.Unreadable || got.Secrets["password"] != "pw-secret-smtp.example.test" {
		t.Fatalf("Load = %+v, %v", got, err)
	}

	// Chiffré copié vers un autre cabinet, ou vers l'autre canal : illisible.
	row, err := q.GetNotifyTransport(ctx, sqlcgen.GetNotifyTransportParams{PracticeID: a.ID, Kind: notify.KindEmail})
	if err != nil {
		t.Fatal(err)
	}
	row.PracticeID = b.ID
	if !e.t.Decode(row).Unreadable {
		t.Error("chiffré déplacé vers un autre cabinet : déchiffrement possible")
	}
	row.PracticeID, row.Kind = a.ID, notify.KindSMS
	if !e.t.Decode(row).Unreadable {
		t.Error("chiffré déplacé vers un autre canal : déchiffrement possible")
	}

	// Clé changée : le message est en erreur temporaire (retentée), jamais envoyé.
	other, _ := secretbox.New("another-secret-another-secret-0000")
	d2 := &notify.PracticeDispatcher{Server: e.server, Transports: &notify.Transports{Pool: e.db, Box: other, NewEmail: e.t.NewEmail}}
	_, err = d2.Send(ctx, emailMsg(a.ID, "p@example.test"))
	if !errors.Is(err, notify.ErrUnreadable) || outbox.IsPermanent(err) || outbox.IsCancelled(err) {
		t.Errorf("clé changée : %v", err)
	}
}

func TestForbiddenHostsRefusedAtConnection(t *testing.T) {
	t.Parallel()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("pas de socket local: %v", err)
	}
	defer func() { _ = ln.Close() }()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			_ = c.Close()
		}
	}()
	port := ln.Addr().(*net.TCPAddr).Port
	cfg := config.EmailConfig{Driver: "smtp", SMTPHost: "127.0.0.1", SMTPPort: port, SMTPFrom: "a@example.test", SMTPTLS: config.SMTPTLSNone}
	msg := notify.Email{To: "p@example.test", Subject: "x", Text: "x"}

	s, err := notify.NewCustomEmailSender(cfg, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Send(context.Background(), msg); !errors.Is(err, notify.ErrForbiddenHost) {
		t.Errorf("SMTP sur 127.0.0.1 : %v", err)
	}
	s, _ = notify.NewCustomEmailSender(cfg, true)
	if _, err := s.Send(context.Background(), msg); err == nil || errors.Is(err, notify.ErrForbiddenHost) {
		t.Errorf("SMTP avec DOCTOLIBRE_NOTIFY_ALLOW_PRIVATE_HOSTS : %v (la connexion doit être tentée)", err)
	}

	// HTTP : refus à la connexion, puis acceptation, sans suivre les redirections.
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		http.Redirect(w, r, "/ailleurs", http.StatusFound)
	}))
	defer srv.Close()
	resp, err := notify.GuardedHTTPClient(false).Get(srv.URL)
	if !errors.Is(err, notify.ErrForbiddenHost) {
		t.Errorf("HTTP sur 127.0.0.1 : %v", err)
	}
	if resp != nil {
		_ = resp.Body.Close()
	}
	resp, err = notify.GuardedHTTPClient(true).Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusFound || hits != 1 {
		t.Errorf("redirection suivie : statut %d, %d requêtes", resp.StatusCode, hits)
	}

	// http:// refusé hors mode « adresses privées ».
	smsCfg := config.SMSConfig{Driver: config.SMSDriverHTTP, HTTP: config.SMSHTTPConfig{Method: "POST", URL: "http://example.com/sms", Body: "x"}}
	if _, err := notify.NewCustomSMSSender(smsCfg, domain.SystemClock{}, false); err == nil {
		t.Error("URL http:// acceptée")
	}
	if _, err := notify.NewCustomSMSSender(smsCfg, domain.SystemClock{}, true); err != nil {
		t.Errorf("URL http:// avec adresses privées autorisées : %v", err)
	}
}
