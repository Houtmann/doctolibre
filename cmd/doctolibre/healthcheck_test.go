package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealthURL(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]string{
		":8080":          "http://127.0.0.1:8080/healthz",
		"0.0.0.0:9000":   "http://127.0.0.1:9000/healthz",
		"127.0.0.1:1234": "http://127.0.0.1:1234/healthz",
		"n'importe quoi": "http://127.0.0.1:8080/healthz",
	} {
		if got := healthURL(in); got != want {
			t.Errorf("healthURL(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCheckHealth(t *testing.T) {
	t.Parallel()
	ok := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) }))
	defer ok.Close()
	ko := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(503) }))
	defer ko.Close()
	if err := checkHealth(context.Background(), ok.URL); err != nil {
		t.Fatal(err)
	}
	if err := checkHealth(context.Background(), ko.URL); err == nil {
		t.Fatal("503 doit échouer")
	}
}
