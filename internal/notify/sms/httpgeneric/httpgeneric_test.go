package httpgeneric

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"doctolibre/internal/notify/sms"
)

func TestSendJSONBearer(t *testing.T) {
	t.Parallel()
	var gotBody, gotAuth, gotCT, gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody, gotAuth, gotCT, gotMethod = string(b), r.Header.Get("Authorization"), r.Header.Get("Content-Type"), r.Method
		_, _ = w.Write([]byte(`{"id":"abc123"}`))
	}))
	defer srv.Close()
	s, err := New(Config{
		URL: srv.URL, Headers: map[string]string{"Content-Type": "application/json"}, Bearer: "tok",
		Body: `{"to":{{json .To}},"text":{{json .Body}},"from":{{json .Sender}}}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	ref, err := s.Send(t.Context(), sms.SMS{To: "+33612345678", Body: `dit "oui"`, Sender: "KINE"})
	if err != nil {
		t.Fatal(err)
	}
	if ref != "abc123" || gotMethod != "POST" || gotAuth != "Bearer tok" || gotCT != "application/json" {
		t.Errorf("ref=%q method=%q auth=%q ct=%q", ref, gotMethod, gotAuth, gotCT)
	}
	if want := `{"to":"+33612345678","text":"dit \"oui\"","from":"KINE"}`; gotBody != want {
		t.Errorf("body = %s", gotBody)
	}
}

func TestSendFormBasic(t *testing.T) {
	t.Parallel()
	var gotBody, user, pass string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		user, pass, _ = r.BasicAuth()
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()
	s, err := New(Config{
		URL: srv.URL, BasicUser: "sid", BasicPass: "secret",
		Headers: map[string]string{"Content-Type": "application/x-www-form-urlencoded"},
		Body:    "To={{urlquery .To}}&Body={{urlquery .Body}}",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Send(t.Context(), sms.SMS{To: "+33612345678", Body: "a b&c"}); err != nil {
		t.Fatal(err)
	}
	if gotBody != "To=%2B33612345678&Body=a+b%26c" || user != "sid" || pass != "secret" {
		t.Errorf("body=%q user=%q pass=%q", gotBody, user, pass)
	}
}

func TestSendErrors(t *testing.T) {
	t.Parallel()
	for status, perm := range map[int]bool{400: true, 401: true, 429: false, 500: false, 503: false} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(status) }))
		s, err := New(Config{URL: srv.URL, Body: "x"})
		if err != nil {
			t.Fatal(err)
		}
		_, err = s.Send(t.Context(), sms.SMS{To: "+33612345678", Body: "x"})
		srv.Close()
		if err == nil || sms.IsPermanent(err) != perm {
			t.Errorf("status %d: err=%v permanent=%v", status, err, sms.IsPermanent(err))
		}
	}
}

func TestNewValidation(t *testing.T) {
	t.Parallel()
	if _, err := New(Config{Body: "x"}); err == nil {
		t.Error("URL vide acceptée")
	}
	if _, err := New(Config{URL: "http://x", Body: "{{.Oops"}); err == nil {
		t.Error("template invalide accepté")
	}
	s, _ := New(Config{URL: "http://x", Body: "{{.Inconnu}}"})
	if _, err := s.Send(t.Context(), sms.SMS{}); err == nil || !sms.IsPermanent(err) || errors.Is(err, nil) {
		t.Errorf("champ inconnu : %v", err)
	}
}
