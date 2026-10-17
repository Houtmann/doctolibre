package service_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"doctolibre/internal/config"
	"doctolibre/internal/domain"
	"doctolibre/internal/notify"
	"doctolibre/internal/secretbox"
	"doctolibre/internal/service"
	"doctolibre/internal/store/sqlcgen"
	"doctolibre/internal/testutil"
)

type recordedEmail struct {
	mu   sync.Mutex
	to   []string
	fail error
}

func (r *recordedEmail) Send(_ context.Context, e notify.Email) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.fail != nil {
		return "", r.fail
	}
	r.to = append(r.to, e.To)
	return "ref", nil
}

type transportFixture struct {
	fixture
	tr    *service.NotifyTransport
	clock *testutil.FixedClock
	email *recordedEmail
	logs  *bytes.Buffer
	ts    *notify.Transports
}

func newTransportFixture(t *testing.T) transportFixture {
	t.Helper()
	f := newFixture(t)
	box, err := secretbox.New(testutil.ManageSecret)
	if err != nil {
		t.Fatal(err)
	}
	em := &recordedEmail{}
	logs := &bytes.Buffer{}
	logger := slog.New(slog.NewTextHandler(logs, nil))
	clock := testutil.NewFixedClock(time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC))
	ts := &notify.Transports{
		Pool: f.db, Box: box, Clock: clock, Logger: logger,
		Env:      config.Config{Email: config.EmailConfig{Driver: config.EmailDriverLog, LogDir: t.TempDir()}, SMS: config.SMSConfig{Driver: config.SMSDriverLog}},
		NewEmail: func(config.EmailConfig, bool) (notify.EmailSender, error) { return em, nil },
	}
	return transportFixture{fixture: f, tr: service.NewNotifyTransport(f.db, clock, ts, logger), clock: clock, email: em, logs: logs, ts: ts}
}

func smtpInput(host, password string) service.TransportInput {
	return service.TransportInput{
		Kind: notify.KindEmail, Mode: notify.ModeCustom, Driver: "smtp",
		Settings: map[string]string{"host": host, "from": "Cabinet <rdv@exemple.fr>", "user": "utilisateur-smtp"},
		Secrets:  map[string]string{"password": password}, Clear: map[string]bool{},
	}
}

func TestTransportUpdateEncryptsAndKeepsSecrets(t *testing.T) {
	t.Parallel()
	f := newTransportFixture(t)
	ctx := context.Background()
	const secret = "mot-de-passe-tres-secret" //nolint:gosec // valeur de test

	if _, err := f.tr.Update(ctx, f.act, smtpInput("smtp.example.org", secret)); err != nil {
		t.Fatal(err)
	}
	v, err := f.tr.View(ctx, f.act)
	if err != nil {
		t.Fatal(err)
	}
	if v.Email.Mode != "custom" || v.Email.Settings["host"] != "smtp.example.org" || !v.Email.SecretSet["password"] {
		t.Fatalf("vue = %+v", v.Email)
	}
	for k, val := range v.Email.Settings {
		if strings.Contains(val, secret) {
			t.Errorf("réglage %s contient le secret", k)
		}
	}
	var raw []byte
	var meta string
	if err := f.db.QueryRow(ctx, `SELECT secrets FROM practice_notify_transport WHERE practice_id = $1 AND kind = 'email'`, f.act.PracticeID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte(secret)) {
		t.Fatal("secret en clair dans la colonne")
	}
	if err := f.db.QueryRow(ctx, `SELECT string_agg(meta::text, ' ') FROM audit_log WHERE action = $1`, service.ActionNotifyTransportUpdated).Scan(&meta); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(meta, secret) || !strings.Contains(meta, `"secrets": "modified"`) {
		t.Errorf("audit = %s", meta)
	}

	read := func() string {
		tr, err := f.ts.Load(ctx, sqlcgen.New(f.db), f.act.PracticeID, notify.KindEmail)
		if err != nil {
			t.Fatal(err)
		}
		return tr.Secrets["password"]
	}
	// Champ vide : valeur conservée.
	if _, err := f.tr.Update(ctx, f.act, smtpInput("smtp2.example.org", "")); err != nil {
		t.Fatal(err)
	}
	if read() != secret {
		t.Error("secret perdu alors que le champ était vide")
	}
	// Case « effacer » : retiré.
	in := smtpInput("smtp2.example.org", "")
	in.Settings["user"] = ""
	in.Clear["password"] = true
	if _, err := f.tr.Update(ctx, f.act, in); err != nil {
		t.Fatal(err)
	}
	if read() != "" {
		t.Error("secret toujours présent après « effacer »")
	}
	// Retour au serveur : plus aucun identifiant en base.
	if _, err := f.tr.Update(ctx, f.act, service.TransportInput{Kind: notify.KindEmail, Mode: notify.ModeServer}); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := f.db.QueryRow(ctx, `SELECT count(*) FROM practice_notify_transport WHERE practice_id = $1 AND secrets IS NOT NULL`, f.act.PracticeID).Scan(&n); err != nil || n != 0 {
		t.Errorf("secrets restants = %d, %v", n, err)
	}
}

func TestTransportUpdateValidation(t *testing.T) {
	t.Parallel()
	f := newTransportFixture(t)
	ctx := context.Background()
	in := smtpInput("", "x")
	in.Settings["from"] = ""
	_, err := f.tr.Update(ctx, f.act, in)
	msg := messages(err)
	if !strings.Contains(msg, "Serveur SMTP") || !strings.Contains(msg, "Expéditeur") || strings.Contains(msg, "DOCTOLIBRE_") {
		t.Errorf("messages = %q", msg)
	}
	// Mot de passe sans identifiant.
	in = smtpInput("smtp.example.org", "pw")
	in.Settings["user"] = ""
	if _, err := f.tr.Update(ctx, f.act, in); len(domain.FieldErrors(err)) == 0 {
		t.Errorf("identifiant manquant accepté : %v", err)
	}
	// Mode et canal inconnus.
	if _, err := f.tr.Update(ctx, f.act, service.TransportInput{Kind: "email", Mode: "pigeon"}); len(domain.FieldErrors(err)) == 0 {
		t.Error("mode inconnu accepté")
	}
	if _, err := f.tr.Update(ctx, f.act, service.TransportInput{Kind: "fax", Mode: "server"}); len(domain.FieldErrors(err)) == 0 {
		t.Error("canal inconnu accepté")
	}
	// Le driver « log » n'est pas offert aux cabinets.
	if _, err := f.tr.Update(ctx, f.act, service.TransportInput{Kind: "sms", Mode: "custom", Driver: "log"}); len(domain.FieldErrors(err)) == 0 {
		t.Error("driver log accepté pour un cabinet")
	}
	// Rôle sans droit.
	sec := f.act
	sec.Role = domain.RoleSecretary
	if _, err := f.tr.Update(ctx, sec, service.TransportInput{Kind: "email", Mode: "disabled"}); !errors.Is(err, domain.ErrForbidden) {
		t.Errorf("secrétaire : %v", err)
	}
	if _, err := f.tr.View(ctx, sec); !errors.Is(err, domain.ErrForbidden) {
		t.Errorf("secrétaire (vue) : %v", err)
	}
	var n int
	if err := f.db.QueryRow(ctx, `SELECT count(*) FROM practice_notify_transport`).Scan(&n); err != nil || n != 0 {
		t.Errorf("lignes écrites malgré les refus : %d, %v", n, err)
	}
}

func TestDisablingChannelRewritesNotificationSettings(t *testing.T) {
	t.Parallel()
	f := newTransportFixture(t)
	ctx := context.Background()
	in, err := f.svc.Notifications(ctx, f.act)
	if err != nil {
		t.Fatal(err)
	}
	in.ConfirmationChannel, in.ReminderChannel, in.ChangeChannel = service.ChannelBoth, service.ChannelSMS, service.ChannelEmail
	if err := f.svc.UpdateNotifications(ctx, f.act, in); err != nil {
		t.Fatal(err)
	}
	res, err := f.tr.Update(ctx, f.act, service.TransportInput{Kind: notify.KindEmail, Mode: notify.ModeDisabled})
	if err != nil || !res.SettingsAdjusted {
		t.Fatalf("Update = %+v, %v", res, err)
	}
	got, err := f.svc.Notifications(ctx, f.act)
	if err != nil {
		t.Fatal(err)
	}
	if got.ConfirmationChannel != "sms" || got.ReminderChannel != "sms" || got.ChangeChannel != "none" {
		t.Errorf("réglages = %s/%s/%s, attendu sms/sms/none", got.ConfirmationChannel, got.ReminderChannel, got.ChangeChannel)
	}
	// Le service refuse aussi le canal désactivé.
	got.ChangeChannel = service.ChannelEmail
	if err := f.svc.UpdateNotifications(ctx, f.act, got); len(domain.FieldErrors(err)) == 0 {
		t.Errorf("canal désactivé accepté : %v", err)
	}
	// Désactiver l'autre canal ramène tout à « none ».
	if _, err := f.tr.Update(ctx, f.act, service.TransportInput{Kind: notify.KindSMS, Mode: notify.ModeDisabled}); err != nil {
		t.Fatal(err)
	}
	got, _ = f.svc.Notifications(ctx, f.act)
	if got.ConfirmationChannel != "none" || got.ReminderChannel != "none" {
		t.Errorf("réglages = %s/%s, attendu none", got.ConfirmationChannel, got.ReminderChannel)
	}
	ch, err := f.svc.Channels(ctx, f.act)
	if err != nil || ch.Any() {
		t.Errorf("Channels = %+v, %v", ch, err)
	}
}

func TestSendTest(t *testing.T) {
	t.Parallel()
	f := newTransportFixture(t)
	ctx := context.Background()
	const secret = "mdp-du-test" //nolint:gosec // valeur de test
	if _, err := f.tr.Update(ctx, f.act, smtpInput("smtp.example.org", secret)); err != nil {
		t.Fatal(err)
	}
	if err := f.tr.SendTest(ctx, f.act, notify.KindEmail, ""); err != nil {
		t.Fatal(err)
	}
	if len(f.email.to) != 1 || f.email.to[0] != "kine@example.test" {
		t.Fatalf("destinataires = %v", f.email.to)
	}

	// Échec : message générique + identifiant de log, ni secret ni identifiant dans les logs.
	f.email.fail = errors.New("535 authentification refusée pour utilisateur-smtp avec " + secret)
	err := f.tr.SendTest(ctx, f.act, notify.KindEmail, "")
	var te service.TestError
	if !errors.As(err, &te) || te.LogID == "" {
		t.Fatalf("erreur = %v", err)
	}
	if strings.Contains(err.Error(), "utilisateur-smtp") || strings.Contains(err.Error(), secret) {
		t.Errorf("erreur affichable trop bavarde : %v", err)
	}
	logs := f.logs.String()
	if !strings.Contains(logs, te.LogID) || strings.Contains(logs, secret) || strings.Contains(logs, "utilisateur-smtp") {
		t.Errorf("logs : %s", logs)
	}
	var meta string
	if err := f.db.QueryRow(ctx, `SELECT string_agg(meta::text, ' ') FROM audit_log WHERE action = $1`, service.ActionNotifyTransportTest).Scan(&meta); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(meta, "failed") || !strings.Contains(meta, "ok") || strings.Contains(meta, secret) {
		t.Errorf("audit = %s", meta)
	}

	// Limite : 5 par heure et par cabinet (2 déjà faits).
	f.email.fail = nil
	for i := range service.TestSendsPerHour - 2 {
		if err := f.tr.SendTest(ctx, f.act, notify.KindEmail, ""); err != nil {
			t.Fatalf("test %d : %v", i, err)
		}
	}
	if err := f.tr.SendTest(ctx, f.act, notify.KindEmail, ""); !errors.Is(err, service.ErrTestRateLimited) {
		t.Fatalf("6e test : %v", err)
	}
	f.clock.Advance(time.Hour + time.Minute)
	if err := f.tr.SendTest(ctx, f.act, notify.KindEmail, ""); err != nil {
		t.Errorf("après une heure : %v", err)
	}

	// Canal désactivé, rôle sans droit, numéro invalide.
	if _, err := f.tr.Update(ctx, f.act, service.TransportInput{Kind: notify.KindEmail, Mode: notify.ModeDisabled}); err != nil {
		t.Fatal(err)
	}
	if err := f.tr.SendTest(ctx, f.act, notify.KindEmail, ""); !errors.Is(err, service.ErrChannelDisabled) {
		t.Errorf("canal désactivé : %v", err)
	}
	sec := f.act
	sec.Role = domain.RoleSecretary
	if err := f.tr.SendTest(ctx, sec, notify.KindSMS, "0612345678"); !errors.Is(err, domain.ErrForbidden) {
		t.Errorf("secrétaire : %v", err)
	}
	if err := f.tr.SendTest(ctx, f.act, notify.KindSMS, "pas un numéro"); len(domain.FieldErrors(err)) == 0 {
		t.Errorf("numéro invalide : %v", err)
	}
}

func TestOnlineBookingClosesWhenNoChannelIsActive(t *testing.T) {
	t.Parallel()
	e := newBookingEnv(t, thursday)
	ctx := context.Background()
	ts := &notify.Transports{Pool: e.db}
	box, _ := secretbox.New(testutil.ManageSecret)
	ts.Box = box
	set := func(kind, mode string) {
		t.Helper()
		settings, secrets, err := ts.Encode(e.pc.ID, kind, notify.Transport{})
		if err != nil {
			t.Fatal(err)
		}
		if err := sqlcgen.New(e.db).UpsertNotifyTransport(ctx, sqlcgen.UpsertNotifyTransportParams{PracticeID: e.pc.ID, Kind: kind, Mode: mode, Settings: settings, Secrets: secrets}); err != nil {
			t.Fatal(err)
		}
	}
	in := e.verifyInput(tuesday9, service.PatientDetails{FirstName: "Alice", LastName: "Durand", Phone: "0611111111", Email: "alice@example.test", SMSOptIn: true, EmailOptIn: true})

	// SMS désactivé : le code ne peut plus partir par SMS (le service refuse), mais par email oui.
	set("sms", "disabled")
	in.Channel = "sms"
	_, err := e.svc.StartVerification(ctx, in)
	if len(domain.FieldErrors(err)) == 0 || e.otp.sent != 0 {
		t.Fatalf("SMS désactivé : %v (codes envoyés : %d)", err, e.otp.sent)
	}
	in.Channel = "email"
	if _, err := e.svc.StartVerification(ctx, in); err != nil {
		t.Fatalf("email actif : %v", err)
	}
	tg, err := e.svc.Target(ctx, e.pc.Slug, e.pr.Slug, e.typ.ID)
	if err != nil || tg.Channels.SMS || !tg.Channels.Email {
		t.Fatalf("Target = %+v, %v", tg.Channels, err)
	}

	// Plus aucun canal : réservation fermée.
	set("email", "disabled")
	sent := e.otp.sent
	for _, ch := range []string{"sms", "email"} {
		in.Channel = ch
		if _, err := e.svc.StartVerification(ctx, in); !errors.Is(err, service.ErrBookingClosed) {
			t.Errorf("canal %s, aucun actif : %v", ch, err)
		}
	}
	if e.otp.sent != sent {
		t.Error("code envoyé alors que la réservation est fermée")
	}
	if _, err := e.svc.Target(ctx, e.pc.Slug, e.pr.Slug, e.typ.ID); !errors.Is(err, service.ErrBookingClosed) {
		t.Errorf("Target fermé : %v", err)
	}
	if p, err := e.svc.Practitioner(ctx, e.pc.Slug, e.pr.Slug); err != nil || p.Channels.Any() {
		t.Errorf("Practitioner = %+v, %v", p.Channels, err)
	}
}
