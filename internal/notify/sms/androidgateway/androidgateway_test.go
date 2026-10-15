package androidgateway

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"doctolibre/internal/notify/sms"
)

func TestSend(t *testing.T) {
	t.Parallel()
	var path, user, pass string
	var got struct {
		Message      string   `json:"message"`
		PhoneNumbers []string `json:"phoneNumbers"`
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		user, pass, _ = r.BasicAuth()
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"id":"m42","state":"Pending"}`))
	}))
	defer srv.Close()
	s, err := New(Config{URL: srv.URL + "/", User: "u", Pass: "p"})
	if err != nil {
		t.Fatal(err)
	}
	ref, err := s.Send(t.Context(), sms.SMS{To: "+33612345678", Body: "Bonjour"})
	if err != nil {
		t.Fatal(err)
	}
	if ref != "m42" || path != "/message" || user != "u" || pass != "p" {
		t.Errorf("ref=%q path=%q user=%q", ref, path, user)
	}
	if got.Message != "Bonjour" || len(got.PhoneNumbers) != 1 || got.PhoneNumbers[0] != "+33612345678" {
		t.Errorf("payload = %+v", got)
	}
}

func TestSendStatusAndValidation(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusUnauthorized) }))
	defer srv.Close()
	s, _ := New(Config{URL: srv.URL, User: "u", Pass: "p"})
	if _, err := s.Send(t.Context(), sms.SMS{To: "+33612345678", Body: "x"}); !sms.IsPermanent(err) {
		t.Errorf("401 doit être permanent : %v", err)
	}
	if _, err := New(Config{}); err == nil {
		t.Error("identifiants vides acceptés")
	}
	d, _ := New(Config{User: "u", Pass: "p"})
	if d.cfg.URL != CloudURL {
		t.Errorf("URL par défaut = %q", d.cfg.URL)
	}
}
