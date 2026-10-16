package service

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"doctolibre/internal/domain"
	"doctolibre/internal/store"
	"doctolibre/internal/store/sqlcgen"
)

// BackupMarker est le fichier écrit par deploy/backup/backup.sh à la fin d'une
// sauvegarde réussie (horodatage RFC 3339 UTC). Le volume des sauvegardes est
// monté en lecture seule dans le conteneur de l'application : aucun accès Docker.
const BackupMarker = ".last_success"

// Status fournit l'état de l'instance pour /admin/status.
type Status struct {
	Pool      *pgxpool.Pool
	Clock     domain.Clock
	Version   string
	StartedAt time.Time
	// BackupDir : dossier des sauvegardes (DOCTOLIBRE_BACKUP_DIR) ; vide = suivi désactivé.
	BackupDir string
}

// StatusInfo est l'état affiché sur /admin/status.
type StatusInfo struct {
	Version          string
	Uptime           time.Duration
	MigrationVersion int64
	DBSizeBytes      int64
	FailedMessages   int64
	// BackupMonitored : false si DOCTOLIBRE_BACKUP_DIR n'est pas configuré.
	BackupMonitored bool
	// LastBackup : zéro si aucune sauvegarde réussie n'est enregistrée.
	LastBackup time.Time
}

// Info lit l'état de l'instance et du cabinet practiceID.
func (s *Status) Info(ctx context.Context, practiceID uuid.UUID) (StatusInfo, error) {
	now := s.Clock.Now()
	info := StatusInfo{Version: s.Version, Uptime: now.Sub(s.StartedAt), BackupMonitored: s.BackupDir != ""}
	if info.Uptime < 0 {
		info.Uptime = 0
	}
	var err error
	if info.MigrationVersion, err = store.MigrationVersion(ctx, s.Pool); err != nil {
		return StatusInfo{}, err
	}
	q := sqlcgen.New(s.Pool)
	if info.DBSizeBytes, err = q.DatabaseSize(ctx); err != nil {
		return StatusInfo{}, fmt.Errorf("taille de la base: %w", err)
	}
	if info.FailedMessages, err = q.CountFailedOutbox(ctx, practiceID); err != nil {
		return StatusInfo{}, fmt.Errorf("messages en échec: %w", err)
	}
	if info.BackupMonitored {
		if info.LastBackup, err = ReadLastBackup(s.BackupDir); err != nil {
			return StatusInfo{}, err
		}
	}
	return info, nil
}

// ReadLastBackup lit la date de la dernière sauvegarde réussie ; zéro si le
// marqueur n'existe pas (ou est illisible : un marqueur corrompu = pas de sauvegarde).
func ReadLastBackup(dir string) (time.Time, error) {
	b, err := os.ReadFile(filepath.Join(dir, BackupMarker)) //nolint:gosec // dossier fixé par la configuration, nom de fichier constant
	if errors.Is(err, fs.ErrNotExist) {
		return time.Time{}, nil
	}
	if err != nil {
		return time.Time{}, fmt.Errorf("lecture du marqueur de sauvegarde: %w", err)
	}
	t, err := time.Parse(time.RFC3339, strings.TrimSpace(string(b)))
	if err != nil {
		return time.Time{}, nil
	}
	return t, nil
}
