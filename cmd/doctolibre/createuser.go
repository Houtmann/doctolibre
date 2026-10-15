package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"

	"doctolibre/internal/auth"
	"doctolibre/internal/config"
	"doctolibre/internal/domain"
	"doctolibre/internal/store"
)

// runCreateUser implémente `doctolibre create-user --email --role --practice-name`.
// Le mot de passe est demandé sur stdin, sans écho si c'est un terminal ; sinon
// (script, pipe) la première ligne de stdin est utilisée.
func runCreateUser(args []string, stdin *os.File, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("create-user", flag.ContinueOnError)
	fs.SetOutput(stderr)
	email := fs.String("email", "", "adresse email de connexion (requis)")
	role := fs.String("role", domain.RoleAdmin, "rôle : admin, practitioner ou secretary")
	practice := fs.String("practice-name", "", "nom du cabinet, créé s'il n'existe pas (requis)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *email == "" || *practice == "" {
		_, _ = fmt.Fprintln(stderr, "usage: doctolibre create-user --email <email> --role <admin|practitioner|secretary> --practice-name <nom>")
		return 2
	}
	cfg, err := config.Load()
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "doctolibre create-user: %v\n", err)
		return 1
	}
	if cfg.DatabaseURL == "" {
		_, _ = fmt.Fprintln(stderr, "doctolibre create-user: DOCTOLIBRE_DATABASE_URL est vide")
		return 1
	}
	password, err := readPassword(stdin, stderr)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "doctolibre create-user: %v\n", err)
		return 1
	}
	ctx := context.Background()
	pool, err := store.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "doctolibre create-user: %v\n", err)
		return 1
	}
	defer pool.Close()
	u, err := auth.CreateUser(ctx, pool, auth.CreateUserParams{
		Email: *email, Role: *role, PracticeName: *practice, Password: password, Timezone: cfg.Timezone,
	})
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "doctolibre create-user: %v\n", err)
		return 1
	}
	_, _ = fmt.Fprintf(stdout, "utilisateur créé : %s (rôle %s)\n", u.ID, u.Role)
	return 0
}

func readPassword(stdin *os.File, stderr io.Writer) (string, error) {
	fd := int(stdin.Fd()) //nolint:gosec // descripteur de fichier standard
	if !term.IsTerminal(fd) {
		line, err := bufio.NewReader(stdin).ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return "", fmt.Errorf("lecture du mot de passe: %w", err)
		}
		return strings.TrimRight(line, "\r\n"), nil
	}
	_, _ = fmt.Fprint(stderr, "Mot de passe : ")
	pw, err := term.ReadPassword(fd)
	_, _ = fmt.Fprintln(stderr)
	if err != nil {
		return "", fmt.Errorf("lecture du mot de passe: %w", err)
	}
	_, _ = fmt.Fprint(stderr, "Confirmation : ")
	pw2, err := term.ReadPassword(fd)
	_, _ = fmt.Fprintln(stderr)
	if err != nil {
		return "", fmt.Errorf("lecture du mot de passe: %w", err)
	}
	if string(pw) != string(pw2) {
		return "", errors.New("les mots de passe ne correspondent pas")
	}
	return string(pw), nil
}
