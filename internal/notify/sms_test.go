package notify

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"testing"
	"time"

	"doctolibre/internal/config"
	"doctolibre/internal/notify/sms"
	"doctolibre/internal/outbox"
	"doctolibre/internal/testutil"
)

func TestSMSRunAt(t *testing.T) {
	t.Parallel()
	paris, err := time.LoadLocation("Europe/Paris")
	if err != nil {
		t.Fatal(err)
	}
	at := func(y int, m time.Month, d, h, mi int) time.Time { return time.Date(y, m, d, h, mi, 0, 0, paris) }
	tests := []struct {
		name string
		now  time.Time
		otp  bool
		want time.Time
	}{
		{"journée : immédiat", at(2026, 10, 15, 14, 0), false, at(2026, 10, 15, 14, 0)},
		{"8h pile : immédiat", at(2026, 10, 15, 8, 0), false, at(2026, 10, 15, 8, 0)},
		{"19h59 : immédiat", at(2026, 10, 15, 19, 59), false, at(2026, 10, 15, 19, 59)},
		{"20h : lendemain 8h", at(2026, 10, 15, 20, 0), false, at(2026, 10, 16, 8, 0)},
		{"3h : même jour 8h", at(2026, 10, 15, 3, 0), false, at(2026, 10, 15, 8, 0)},
		{"7h59 : même jour 8h", at(2026, 10, 15, 7, 59), false, at(2026, 10, 15, 8, 0)},
		{"nuit mais OTP", at(2026, 10, 15, 23, 0), true, at(2026, 10, 15, 23, 0)},
		// Passage à l'heure d'été (2026-03-29 à 2h) : 8h CEST = 06:00Z, pas 07:00Z.
		{"veille DST été", at(2026, 3, 28, 21, 0), false, time.Date(2026, 3, 29, 6, 0, 0, 0, time.UTC)},
		{"nuit DST été", at(2026, 3, 29, 3, 30), false, time.Date(2026, 3, 29, 6, 0, 0, 0, time.UTC)},
		// Passage à l'heure d'hiver (2026-10-25 à 3h) : 8h CET = 07:00Z.
		{"veille DST hiver", at(2026, 10, 24, 22, 0), false, time.Date(2026, 10, 25, 7, 0, 0, 0, time.UTC)},
		{"soir DST hiver", at(2026, 10, 25, 21, 0), false, time.Date(2026, 10, 26, 7, 0, 0, 0, time.UTC)},
		// Fin de mois / d'année.
		{"31 décembre soir", at(2026, 12, 31, 22, 0), false, time.Date(2027, 1, 1, 7, 0, 0, 0, time.UTC)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := SMSRunAt(tc.now.UTC(), paris, tc.otp); !got.Equal(tc.want) {
				t.Errorf("SMSRunAt = %s, attendu %s", got, tc.want.UTC())
			}
		})
	}
}

type fakeSender struct {
	got sms.SMS
	err error
}

func (f *fakeSender) Send(_ context.Context, m sms.SMS) (string, error) {
	f.got = m
	return "ref1", f.err
}

func TestSMSDispatch(t *testing.T) {
	t.Parallel()
	f := &fakeSender{}
	d := SMSDispatch(f, "KINE", true)
	msg := func(p string) outbox.Msg {
		return outbox.Msg{Kind: outbox.KindSMS, Recipient: "+33612345678", Payload: json.RawMessage(p)}
	}
	ref, err := d(t.Context(), msg(`{"body":"Rendez-vous à l’œil"}`))
	if err != nil || ref != "ref1" {
		t.Fatalf("ref=%q err=%v", ref, err)
	}
	if f.got.To != "+33612345678" || f.got.Sender != "KINE" || f.got.Body != "Rendez-vous à l'oeil" {
		t.Errorf("sms = %+v", f.got)
	}

	for name, p := range map[string]string{"corps vide": `{}`, "json invalide": `nope`, "opt-out": `{"body":"x","sms_opt_in":false}`} {
		if _, err := d(t.Context(), msg(p)); !outbox.IsPermanent(err) {
			t.Errorf("%s : doit être permanent (%v)", name, err)
		}
	}
	if _, err := d(t.Context(), msg(`{"body":"123456","sms_opt_in":false,"otp":true}`)); err != nil {
		t.Errorf("OTP ignore l'opt-out : %v", err)
	}
	if _, err := d(t.Context(), msg(`{"body":"x","sms_opt_in":true}`)); err != nil {
		t.Errorf("opt-in : %v", err)
	}
}

func TestSMSDispatchErrorMapping(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		err  error
		perm bool
	}{
		"400":     {sms.StatusError{Status: 400}, true},
		"429":     {sms.StatusError{Status: 429}, false},
		"500":     {sms.StatusError{Status: 500}, false},
		"réseau":  {errors.New("dial tcp: refused"), false},
		"permane": {sms.PermanentError{Err: errors.New("numéro")}, true},
	} {
		d := SMSDispatch(&fakeSender{err: tc.err}, "", false)
		_, err := d(t.Context(), outbox.Msg{Recipient: "+33612345678", Payload: json.RawMessage(`{"body":"x"}`)})
		if err == nil || outbox.IsPermanent(err) != tc.perm {
			t.Errorf("%s : err=%v permanent=%v", name, err, outbox.IsPermanent(err))
		}
	}
}

func TestNewSMSSenderDrivers(t *testing.T) {
	t.Parallel()
	clock := testutil.NewFixedClock(time.Now())
	log := slog.Default()
	ok := []config.SMSConfig{
		{Driver: "log"},
		{Driver: "http", HTTP: config.SMSHTTPConfig{URL: "http://x", Body: "{{.Body}}"}},
		{Driver: "android", AndroidUser: "u", AndroidPass: "p"},
		{Driver: "ovh", OVHService: "s", OVHAppKey: "a", OVHAppSecret: "b", OVHConsumerKey: "c"},
	}
	for _, c := range ok {
		if s, err := NewSMSSender(c, clock, log); err != nil || s == nil {
			t.Errorf("%s : %v", c.Driver, err)
		}
	}
	if _, err := NewSMSSender(config.SMSConfig{Driver: "nope"}, clock, log); err == nil {
		t.Error("driver inconnu accepté")
	}
}
