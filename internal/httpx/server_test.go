package httpx

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"testing"
	"time"

	"doctolibre/internal/config"
)

func TestNewTimeouts(t *testing.T) {
	t.Parallel()
	srv := New(config.Config{HTTPAddr: ":0"}, http.NotFoundHandler())
	if srv.ReadHeaderTimeout != 5*time.Second || srv.ReadTimeout != 15*time.Second ||
		srv.WriteTimeout != 30*time.Second || srv.IdleTimeout != 60*time.Second {
		t.Errorf("timeouts inattendus: %+v", srv)
	}
}

func TestRunGracefulShutdown(t *testing.T) {
	t.Parallel()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()

	srv := New(config.Config{HTTPAddr: addr}, http.HandlerFunc(ok))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Run(ctx, srv, slog.New(slog.NewTextHandler(io.Discard, nil))) }()

	var resp *http.Response
	for range 50 {
		resp, err = http.Get("http://" + addr + "/") //nolint:noctx // test local
		if err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("arrêt trop long")
	}
}
