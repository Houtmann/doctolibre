package notify_test

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/mail"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"doctolibre/internal/config"
	"doctolibre/internal/notify"
	"doctolibre/internal/outbox"
)

var fixedNow = time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC)

const (
	from       = "Cabinet Dupont <rdv@example.test>"
	subjectFR  = "Rappel : séance d'été à 14 h — Hélène"
	textFR     = "Bonjour Hélène,\nVotre séance est confirmée à 14 h 30. À bientôt ! Ça va très bien… ✓\n"
	htmlFR     = "<p>Bonjour Hélène,</p><p>Séance confirmée à <b>14 h 30</b>. Ça va très bien… ✓</p>"
	smtpPass   = "s3cret-pass"
	recipientA = "malade@example.test"
)

func newSender(t *testing.T, f *fakeSMTP, mode string, user string, client *tls.Config) *notify.SMTPSender {
	t.Helper()
	s, err := notify.NewSMTPSender(notify.SMTPConfig{
		Host: "127.0.0.1", Port: f.port(), User: user, Password: smtpPass, From: from, TLS: mode,
		TLSConfig: client, Now: func() time.Time { return fixedNow }, Timeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// decodeParts retourne le sujet décodé, le type du message et le corps de chaque
// partie décodé (quoted-printable).
func decodeParts(t *testing.T, raw string) (subject, mediaType string, bodies map[string]string, hdr mail.Header) {
	t.Helper()
	m, err := mail.ReadMessage(strings.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	subject, err = new(mime.WordDecoder).DecodeHeader(m.Header.Get("Subject"))
	if err != nil {
		t.Fatal(err)
	}
	mediaType, params, err := mime.ParseMediaType(m.Header.Get("Content-Type"))
	if err != nil {
		t.Fatal(err)
	}
	bodies = map[string]string{}
	readQP := func(r io.Reader) string {
		b, err := io.ReadAll(quotedprintable.NewReader(r))
		if err != nil {
			t.Fatal(err)
		}
		return strings.ReplaceAll(string(b), "\r\n", "\n")
	}
	if mediaType == "multipart/alternative" {
		mr := multipart.NewReader(m.Body, params["boundary"])
		for {
			p, err := mr.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Fatal(err)
			}
			typ, _, _ := mime.ParseMediaType(p.Header.Get("Content-Type"))
			bodies[typ] = readQP(p)
		}
	} else {
		bodies[mediaType] = readQP(m.Body)
	}
	return subject, mediaType, bodies, m.Header
}

func TestSMTPSendAccentsAndMIME(t *testing.T) {
	t.Parallel()
	f := newFakeSMTP(t, &fakeSMTP{authMechs: "PLAIN LOGIN"})
	s := newSender(t, f, config.SMTPTLSNone, "kine", nil) // 127.0.0.1 : AUTH autorisé sans TLS
	ref, err := s.Send(context.Background(), notify.Email{
		To: recipientA, Subject: subjectFR, Text: textFR, HTML: htmlFR,
		ReplyTo: "contact@example.test", Headers: map[string]string{"List-Unsubscribe": "<mailto:stop@example.test>"},
	})
	if err != nil {
		t.Fatal(err)
	}
	msgs := f.messages()
	if len(msgs) != 1 {
		t.Fatalf("%d messages reçus", len(msgs))
	}
	m := msgs[0]
	if m.From != "rdv@example.test" || m.To != recipientA || m.AuthUser != "kine" || m.AuthPass != smtpPass {
		t.Errorf("enveloppe/auth inattendues: %+v", m)
	}
	if !strings.Contains(m.Data, "Subject: =?utf-8?q?") {
		t.Errorf("sujet non encodé RFC 2047:\n%s", m.Data)
	}
	subject, typ, bodies, hdr := decodeParts(t, m.Data)
	if subject != subjectFR {
		t.Errorf("sujet décodé = %q", subject)
	}
	if typ != "multipart/alternative" {
		t.Errorf("type = %q", typ)
	}
	if bodies["text/plain"] != textFR {
		t.Errorf("texte décodé = %q", bodies["text/plain"])
	}
	if bodies["text/html"] != htmlFR {
		t.Errorf("html décodé = %q", bodies["text/html"])
	}
	if hdr.Get("Message-ID") != ref || !strings.HasSuffix(ref, "@example.test>") {
		t.Errorf("Message-ID = %q, ref = %q", hdr.Get("Message-ID"), ref)
	}
	if d, err := hdr.Date(); err != nil || !d.Equal(fixedNow) {
		t.Errorf("Date = %v %v", d, err)
	}
	if hdr.Get("Reply-To") != "<contact@example.test>" || hdr.Get("List-Unsubscribe") != "<mailto:stop@example.test>" || hdr.Get("MIME-Version") != "1.0" {
		t.Errorf("en-têtes: %v", hdr)
	}
	if !strings.Contains(hdr.Get("From"), "rdv@example.test") {
		t.Errorf("From = %q", hdr.Get("From"))
	}
}

func TestSMTPTextOnly(t *testing.T) {
	t.Parallel()
	f := newFakeSMTP(t, &fakeSMTP{})
	s := newSender(t, f, config.SMTPTLSNone, "", nil)
	if _, err := s.Send(context.Background(), notify.Email{To: recipientA, Subject: "Été", Text: textFR}); err != nil {
		t.Fatal(err)
	}
	subject, typ, bodies, _ := decodeParts(t, f.messages()[0].Data)
	if subject != "Été" || typ != "text/plain" || bodies["text/plain"] != textFR {
		t.Errorf("%q %q %q", subject, typ, bodies)
	}
}

func TestSMTPStartTLSAndLoginAuth(t *testing.T) {
	t.Parallel()
	srvTLS, cliTLS := testCert(t)
	f := newFakeSMTP(t, &fakeSMTP{tlsConf: srvTLS, authMechs: "LOGIN"})
	s := newSender(t, f, config.SMTPTLSStartTLS, "kine", cliTLS)
	if _, err := s.Send(context.Background(), notify.Email{To: recipientA, Subject: "é", Text: "é"}); err != nil {
		t.Fatal(err)
	}
	m := f.messages()
	if len(m) != 1 || !m[0].TLS || m[0].AuthUser != "kine" || m[0].AuthPass != smtpPass {
		t.Fatalf("starttls/login: %+v", m)
	}
}

func TestSMTPImplicitTLS(t *testing.T) {
	t.Parallel()
	srvTLS, cliTLS := testCert(t)
	f := newFakeSMTP(t, &fakeSMTP{tlsConf: srvTLS, implicit: true, authMechs: "PLAIN"})
	s := newSender(t, f, config.SMTPTLSImplicit, "kine", cliTLS)
	if _, err := s.Send(context.Background(), notify.Email{To: recipientA, Subject: "é", Text: "é"}); err != nil {
		t.Fatal(err)
	}
	if m := f.messages(); len(m) != 1 || !m[0].TLS || m[0].AuthPass != smtpPass {
		t.Fatalf("tls implicite: %+v", m)
	}
}

func TestSMTPStartTLSRequired(t *testing.T) {
	t.Parallel()
	f := newFakeSMTP(t, &fakeSMTP{}) // n'annonce pas STARTTLS
	s := newSender(t, f, config.SMTPTLSStartTLS, "", nil)
	_, err := s.Send(context.Background(), notify.Email{To: recipientA, Subject: "x", Text: "x"})
	if err == nil || !strings.Contains(err.Error(), "STARTTLS") {
		t.Fatalf("erreur attendue sur STARTTLS absent: %v", err)
	}
	if len(f.messages()) != 0 {
		t.Error("message envoyé en clair")
	}
}

func TestSMTPErrorClassification(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		rcpt      string
		permanent bool
	}{
		{"5xx permanent", "550 no such user", true},
		{"4xx temporaire", "451 try later", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := newFakeSMTP(t, &fakeSMTP{rcptReply: tt.rcpt})
			s := newSender(t, f, config.SMTPTLSNone, "", nil)
			_, err := s.Send(context.Background(), notify.Email{To: recipientA, Subject: "x", Text: "x"})
			if err == nil || notify.IsPermanentSMTP(err) != tt.permanent {
				t.Fatalf("err=%v permanent=%v", err, notify.IsPermanentSMTP(err))
			}
		})
	}
	t.Run("réseau temporaire", func(t *testing.T) {
		t.Parallel()
		f := newFakeSMTP(t, &fakeSMTP{})
		s := newSender(t, f, config.SMTPTLSNone, "", nil)
		_ = f.ln.Close()
		_, err := s.Send(context.Background(), notify.Email{To: recipientA, Subject: "x", Text: "x"})
		if err == nil || notify.IsPermanentSMTP(err) {
			t.Fatalf("connexion refusée doit être temporaire: %v", err)
		}
	})
}

func TestEmailValidation(t *testing.T) {
	t.Parallel()
	f := newFakeSMTP(t, &fakeSMTP{})
	s := newSender(t, f, config.SMTPTLSNone, "", nil)
	bad := []notify.Email{
		{To: recipientA, Subject: "x\r\nBcc: evil@example.test", Text: "x"},
		{To: "pas une adresse", Subject: "x", Text: "x"},
		{To: recipientA + "\r\nBcc: evil@example.test", Subject: "x", Text: "x"},
		{To: recipientA, Subject: "x", Text: "  "},
		{To: recipientA, Subject: "x", Text: "x", Headers: map[string]string{"X-A": "v\r\nBcc: e@example.test"}},
		{To: recipientA, Subject: "x", Text: "x", Headers: map[string]string{"Subject": "écrasé"}},
	}
	for i, e := range bad {
		_, err := s.Send(context.Background(), e)
		if !errors.Is(err, notify.ErrInvalidEmail) || !notify.IsPermanentSMTP(err) {
			t.Errorf("cas %d: ErrInvalidEmail attendue, obtenu %v", i, err)
		}
	}
	if len(f.messages()) != 0 {
		t.Error("un message invalide a été envoyé")
	}
}

func TestLogSenderWritesEML(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "mails")
	ls, err := notify.NewLogSender(from, dir, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	ls.Now = func() time.Time { return fixedNow }
	ref, err := ls.Send(context.Background(), notify.Email{To: recipientA, Subject: subjectFR, Text: textFR, HTML: htmlFR})
	if err != nil {
		t.Fatal(err)
	}
	files, err := filepath.Glob(filepath.Join(dir, "*.eml"))
	if err != nil || len(files) != 1 {
		t.Fatalf("fichiers: %v %v", files, err)
	}
	raw, err := os.ReadFile(files[0]) //nolint:gosec // chemin de test
	if err != nil {
		t.Fatal(err)
	}
	subject, _, bodies, hdr := decodeParts(t, string(raw))
	if subject != subjectFR || bodies["text/plain"] != textFR || hdr.Get("Message-ID") != ref {
		t.Errorf("eml: %q %q", subject, bodies)
	}
}

func TestNewEmailSenderDriver(t *testing.T) {
	t.Parallel()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	s, err := notify.NewEmailSender(config.EmailConfig{Driver: config.EmailDriverLog, LogDir: t.TempDir()}, logger)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := s.(*notify.LogSender); !ok {
		t.Errorf("type = %T", s)
	}
	s, err = notify.NewEmailSender(config.EmailConfig{
		Driver: config.EmailDriverSMTP, SMTPHost: "smtp.example.test", SMTPPort: 587, SMTPFrom: from, SMTPTLS: config.SMTPTLSStartTLS,
	}, logger)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := s.(*notify.SMTPSender); !ok {
		t.Errorf("type = %T", s)
	}
	if _, err := notify.NewEmailSender(config.EmailConfig{Driver: "x"}, logger); err == nil {
		t.Error("driver inconnu accepté")
	}
}

func TestTemplatesPasswordReset(t *testing.T) {
	t.Parallel()
	tpl, err := notify.NewTemplates()
	if err != nil {
		t.Fatal(err)
	}
	e, err := tpl.Render("password_reset", map[string]any{"reset_url": "https://x.test/admin/password/reset?token=a&b=é", "valid_for": "1 heure"})
	if err != nil {
		t.Fatal(err)
	}
	if e.Subject != "Réinitialisation de votre mot de passe" {
		t.Errorf("sujet = %q", e.Subject)
	}
	if !strings.Contains(e.Text, "?token=a&b=é") || strings.Contains(e.Text, "&amp;") {
		t.Errorf("texte échappé à tort: %q", e.Text)
	}
	if !strings.Contains(e.HTML, `href="https://x.test/admin/password/reset?token=a&amp;b=%c3%a9"`) || !strings.Contains(e.HTML, `lang="fr"`) {
		t.Errorf("html: %q", e.HTML)
	}
	if _, err := tpl.Render("password_reset", nil); err == nil {
		t.Error("payload incomplet accepté")
	}
	if _, err := tpl.Render("nope", nil); !errors.Is(err, notify.ErrUnknownTemplate) {
		t.Errorf("template inconnu: %v", err)
	}
}

type stubSender struct {
	got notify.Email
	err error
}

func (s *stubSender) Send(_ context.Context, e notify.Email) (string, error) {
	s.got = e
	return "<ref@x>", s.err
}

func TestEmailDispatcher(t *testing.T) {
	t.Parallel()
	payload := json.RawMessage(`{"reset_url":"https://x.test/r?token=t","valid_for":"1 heure"}`)
	msg := outbox.Msg{ID: uuid.New(), Kind: outbox.KindEmail, Template: "password_reset", Payload: payload, Recipient: recipientA}

	st := &stubSender{}
	d, err := notify.NewEmailDispatcher(st)
	if err != nil {
		t.Fatal(err)
	}
	ref, err := d(context.Background(), msg)
	if err != nil || ref != "<ref@x>" || st.got.To != recipientA || !strings.Contains(st.got.Text, "https://x.test/r?token=t") {
		t.Fatalf("envoi: ref=%q err=%v email=%+v", ref, err, st.got)
	}

	tests := []struct {
		name      string
		mod       func(*outbox.Msg)
		sendErr   error
		permanent bool
	}{
		{"kind sms", func(m *outbox.Msg) { m.Kind = outbox.KindSMS }, nil, true},
		{"template inconnu", func(m *outbox.Msg) { m.Template = "nope" }, nil, true},
		{"payload invalide", func(m *outbox.Msg) { m.Payload = json.RawMessage(`[1]`) }, nil, true},
		{"payload incomplet", func(m *outbox.Msg) { m.Payload = json.RawMessage(`{}`) }, nil, true},
		{"email invalide", func(*outbox.Msg) {}, notify.ErrInvalidEmail, true},
		{"réseau", func(*outbox.Msg) {}, errors.New("connection reset"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			d, err := notify.NewEmailDispatcher(&stubSender{err: tt.sendErr})
			if err != nil {
				t.Fatal(err)
			}
			m := msg
			tt.mod(&m)
			_, err = d(context.Background(), m)
			if err == nil || outbox.IsPermanent(err) != tt.permanent {
				t.Fatalf("err=%v permanent=%v", err, outbox.IsPermanent(err))
			}
		})
	}
}
