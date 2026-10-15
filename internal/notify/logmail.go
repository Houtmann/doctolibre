package notify

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"
)

// LogSender est l'expéditeur de dev : écrit chaque email en .eml dans Dir et
// journalise seulement l'ID du message et le chemin du fichier (le fichier
// contient les données personnelles, pas les logs).
type LogSender struct {
	from   string
	dir    string
	logger *slog.Logger
	// Now est remplaçable dans les tests.
	Now func() time.Time
}

var _ EmailSender = (*LogSender)(nil)

// NewLogSender crée l'expéditeur de dev. dir est créé (0700) à la demande.
func NewLogSender(from, dir string, logger *slog.Logger) (*LogSender, error) {
	if _, err := buildMessage(from, Email{To: "a@example.test", Text: "x"}, time.Time{}); err != nil {
		return nil, fmt.Errorf("logmail: expéditeur invalide: %w", err)
	}
	if dir == "" {
		dir = "tmp/mails"
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &LogSender{from: from, dir: dir, logger: logger, Now: time.Now}, nil
}

// Send implémente EmailSender.
func (l *LogSender) Send(ctx context.Context, e Email) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	now := l.Now()
	msg, err := buildMessage(l.from, e, now)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(l.dir, 0o700); err != nil {
		return "", fmt.Errorf("logmail: dossier: %w", err)
	}
	r := make([]byte, 4)
	if _, err := rand.Read(r); err != nil {
		return "", fmt.Errorf("logmail: %w", err)
	}
	path := filepath.Join(l.dir, now.UTC().Format("20060102T150405Z")+"-"+hex.EncodeToString(r)+".eml")
	if err := os.WriteFile(path, msg.Raw, 0o600); err != nil {
		return "", fmt.Errorf("logmail: écriture: %w", err)
	}
	l.logger.Info("email écrit (driver log)", slog.String("message_id", msg.MessageID), slog.String("file", path))
	return msg.MessageID, nil
}
