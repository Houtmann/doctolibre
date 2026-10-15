package ovh

import (
	"crypto/sha1" //nolint:gosec // vecteur de test
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"doctolibre/internal/notify/sms"
	"doctolibre/internal/testutil"
)

func TestSignatureVector(t *testing.T) {
	t.Parallel()
	// Vecteur calculé indépendamment : sha1 de la chaîne jointe par "+".
	raw := "secret+consumer+POST+https://eu.api.ovh.com/1.0/sms/sms-ab1-1/jobs+{\"a\":1}+1760500000"
	sum := sha1.Sum([]byte(raw)) //nolint:gosec
	want := "$1$" + hex.EncodeToString(sum[:])
	got := Signature("secret", "consumer", "POST", "https://eu.api.ovh.com/1.0/sms/sms-ab1-1/jobs", `{"a":1}`, 1760500000)
	if got != want {
		t.Errorf("signature = %s, attendu %s", got, want)
	}
	// Valeur littérale figée (régression).
	if got != "$1$"+hex.EncodeToString(sum[:]) || len(got) != 43 {
		t.Errorf("format inattendu : %s", got)
	}
}

func TestSend(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	var hdr http.Header
	var body []byte
	var url string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hdr, url = r.Header.Clone(), r.URL.Path
		body, _ = io.ReadAll(r.Body)
		_, _ = w.Write([]byte(`{"ids":[123456],"invalidReceivers":[],"validReceivers":["+33612345678"],"totalCreditsRemoved":1}`))
	}))
	defer srv.Close()
	s, err := New(Config{Endpoint: srv.URL + "/1.0", ServiceName: "sms-ab1-1", AppKey: "ak", AppSecret: "as", ConsumerKey: "ck", Clock: testutil.NewFixedClock(now)})
	if err != nil {
		t.Fatal(err)
	}
	ref, err := s.Send(t.Context(), sms.SMS{To: "+33612345678", Body: "Rappel", Sender: "KINE"})
	if err != nil {
		t.Fatal(err)
	}
	if ref != "123456" || url != "/1.0/sms/sms-ab1-1/jobs" {
		t.Errorf("ref=%q url=%q", ref, url)
	}
	if hdr.Get("X-Ovh-Application") != "ak" || hdr.Get("X-Ovh-Consumer") != "ck" || hdr.Get("X-Ovh-Timestamp") != "1792065600" {
		t.Errorf("en-têtes : %v", hdr)
	}
	wantSig := Signature("as", "ck", "POST", srv.URL+"/1.0/sms/sms-ab1-1/jobs", string(body), now.Unix())
	if hdr.Get("X-Ovh-Signature") != wantSig {
		t.Errorf("signature = %s, attendu %s", hdr.Get("X-Ovh-Signature"), wantSig)
	}
	var job map[string]any
	if err := json.Unmarshal(body, &job); err != nil {
		t.Fatal(err)
	}
	if job["message"] != "Rappel" || job["sender"] != "KINE" || job["noStopClause"] != true || job["receivers"].([]any)[0] != "+33612345678" {
		t.Errorf("job = %v", job)
	}
}

func TestSendInvalidReceiverAndStatus(t *testing.T) {
	t.Parallel()
	mk := func(status int, resp string) *Sender {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(status)
			_, _ = w.Write([]byte(resp))
		}))
		t.Cleanup(srv.Close)
		s, _ := New(Config{Endpoint: srv.URL, ServiceName: "s", AppKey: "a", AppSecret: "b", ConsumerKey: "c", Clock: testutil.NewFixedClock(time.Unix(1, 0))})
		return s
	}
	m := sms.SMS{To: "+33612345678", Body: "x"}
	if _, err := mk(200, `{"ids":[],"invalidReceivers":["+33612345678"]}`).Send(t.Context(), m); !sms.IsPermanent(err) {
		t.Errorf("numéro invalide doit être permanent : %v", err)
	}
	if _, err := mk(403, `{}`).Send(t.Context(), m); !sms.IsPermanent(err) {
		t.Errorf("403 doit être permanent : %v", err)
	}
	if _, err := mk(500, `{}`).Send(t.Context(), m); err == nil || sms.IsPermanent(err) {
		t.Errorf("500 doit être temporaire : %v", err)
	}
}
