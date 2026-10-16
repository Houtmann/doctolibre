package web

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"

	"doctolibre/internal/auth"
	"doctolibre/internal/domain"
	"doctolibre/internal/service"
)

// StatusSource fournit l'état de l'instance (*service.Status).
type StatusSource interface {
	Info(ctx context.Context, practiceID uuid.UUID) (service.StatusInfo, error)
}

// StatusPage est la donnée de la page /admin/status.
type StatusPage struct {
	Version          string
	Uptime           string
	MigrationVersion int64
	DBSize           string
	FailedMessages   int64
	BackupMonitored  bool
	LastBackup       time.Time
}

func (d Deps) statusRoutes(mux *http.ServeMux, rd *Renderer, logger *slog.Logger) {
	if d.Status == nil {
		return
	}
	admin := d.Auth.RequireUser(domain.RoleAdmin)
	mux.Handle("GET /admin/status", admin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, _ := auth.UserFrom(r.Context())
		info, err := d.Status.Info(r.Context(), u.PracticeID)
		if err != nil {
			logger.Error("status: lecture en échec", slog.String("err", err.Error()))
			rd.Error(w, r, http.StatusInternalServerError, "")
			return
		}
		rd.Page(w, r, "admin_status", StatusPage{
			Version: info.Version, Uptime: formatUptime(info.Uptime), MigrationVersion: info.MigrationVersion,
			DBSize: formatBytes(info.DBSizeBytes), FailedMessages: info.FailedMessages,
			BackupMonitored: info.BackupMonitored, LastBackup: info.LastBackup,
		})
	})))
}

func formatUptime(d time.Duration) string {
	switch {
	case d >= 24*time.Hour:
		return fmt.Sprintf("%d j %d h", int(d/(24*time.Hour)), int(d%(24*time.Hour)/time.Hour))
	case d >= time.Hour:
		return fmt.Sprintf("%d h %d min", int(d/time.Hour), int(d%time.Hour/time.Minute))
	default:
		return fmt.Sprintf("%d min", int(d/time.Minute))
	}
}

func formatBytes(n int64) string {
	const mo = 1 << 20
	if n < mo {
		return fmt.Sprintf("%d Ko", n>>10)
	}
	return fmt.Sprintf("%.1f Mo", float64(n)/mo)
}
