package main

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	"doctolibre/internal/config"
)

const healthcheckTimeout = 3 * time.Second

// runHealthcheck interroge /healthz du serveur local (HEALTHCHECK Docker : l'image
// distroless n'a ni shell ni curl). Code 0 si le serveur répond 200.
func runHealthcheck(stderr io.Writer) int {
	cfg, err := config.Load()
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "doctolibre healthcheck: %v\n", err)
		return 1
	}
	if err := checkHealth(context.Background(), healthURL(cfg.HTTPAddr)); err != nil {
		_, _ = fmt.Fprintf(stderr, "doctolibre healthcheck: %v\n", err)
		return 1
	}
	return 0
}

// healthURL construit l'URL locale à partir de l'adresse d'écoute (":8080", "0.0.0.0:80"…).
func healthURL(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "http://127.0.0.1:8080/healthz"
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, port) + "/healthz"
}

func checkHealth(ctx context.Context, url string) error {
	ctx, cancel := context.WithTimeout(ctx, healthcheckTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("statut %d", resp.StatusCode)
	}
	return nil
}
