package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"

	"doctolibre/internal/config"
	"doctolibre/internal/httpx"
	"doctolibre/internal/web"
)

// serve lance le serveur HTTP jusqu'à l'annulation de ctx.
func serve(ctx context.Context, logOut io.Writer) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("configuration: %w", err)
	}
	logger := slog.New(slog.NewJSONHandler(logOut, nil))
	handler := web.Routes(web.Deps{Config: cfg, Logger: logger})
	return httpx.Run(ctx, httpx.New(cfg, handler), logger)
}
