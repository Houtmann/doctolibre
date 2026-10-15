// Commande doctolibre : serveur de prise de rendez-vous.
package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"doctolibre/internal/config"
	"doctolibre/internal/store"
)

// version est injectée au build : -ldflags "-X main.version=...".
var version = "dev"

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		usage(stderr)
		return 2
	}
	switch args[0] {
	case "version":
		_, _ = fmt.Fprintln(stdout, version)
		return 0
	case "serve":
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		if err := serve(ctx, stderr); err != nil {
			_, _ = fmt.Fprintf(stderr, "doctolibre serve: %v\n", err)
			return 1
		}
		return 0
	case "migrate":
		return runMigrate(args[1:], stdout, stderr)
	case "create-user":
		return runCreateUser(args[1:], os.Stdin, stdout, stderr)
	case "-h", "--help", "help":
		usage(stdout)
		return 0
	default:
		_, _ = fmt.Fprintf(stderr, "doctolibre: commande inconnue %q\n", args[0])
		usage(stderr)
		return 2
	}
}

func usage(w io.Writer) {
	_, _ = fmt.Fprintln(w, "usage: doctolibre <serve|migrate|create-user|version>")
}

func runMigrate(args []string, stdout, stderr io.Writer) int {
	action := "up"
	if len(args) > 0 {
		action = args[0]
	}
	if action != "up" && action != "down" && action != "status" {
		_, _ = fmt.Fprintf(stderr, "usage: doctolibre migrate [up|down|status]\n")
		return 2
	}
	cfg, err := config.Load()
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "doctolibre migrate: %v\n", err)
		return 1
	}
	if cfg.DatabaseURL == "" {
		_, _ = fmt.Fprintln(stderr, "doctolibre migrate: DOCTOLIBRE_DATABASE_URL est vide")
		return 1
	}
	ctx := context.Background()
	pool, err := store.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "doctolibre migrate: %v\n", err)
		return 1
	}
	defer pool.Close()
	switch action {
	case "up":
		err = store.Migrate(ctx, pool)
	case "down":
		err = store.MigrateDown(ctx, pool)
	case "status":
		err = store.MigrateStatus(ctx, pool, stdout)
	}
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "doctolibre migrate: %v\n", err)
		return 1
	}
	return 0
}
