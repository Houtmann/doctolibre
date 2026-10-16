package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"doctolibre/internal/domain"
	"doctolibre/internal/outbox"
	"doctolibre/internal/store"
	"doctolibre/internal/store/sqlcgen"
)

// Paramètres des alertes admin.
const (
	DefaultAlertEvery = 10 * time.Minute
	// AlertFailedWindow : on alerte si des messages ont échoué dans cette fenêtre.
	AlertFailedWindow = time.Hour
	// AlertBackupMaxAge : on alerte si la dernière sauvegarde est plus ancienne.
	AlertBackupMaxAge = 48 * time.Hour

	// TemplateAdminAlert : template email (internal/notify/templates/email/admin_alert.*).
	// Le nom est aussi utilisé par la requête CountFailedOutboxSince.
	TemplateAdminAlert = "admin_alert"
)

// Alerts enfile des emails d'alerte aux admins de chaque cabinet : messages en
// échec dans la dernière heure, pas de sauvegarde depuis 48 h. Idempotent : la
// clé de déduplication (par fenêtre : heure UTC pour les échecs, jour UTC pour
// la sauvegarde) garantit une seule alerte par fenêtre, même avec plusieurs
// processus. Les messages d'alerte eux-mêmes ne comptent pas comme échecs.
type Alerts struct {
	Pool   *pgxpool.Pool
	Clock  domain.Clock
	Logger *slog.Logger
	// BackupDir : dossier des sauvegardes ; vide = pas d'alerte de sauvegarde.
	BackupDir string
	// BaseURL : adresse publique, pour le lien vers /admin/status (vide = sans lien).
	BaseURL string
	// Every : période de Run (défaut DefaultAlertEvery).
	Every time.Duration
}

// Run appelle RunOnce périodiquement jusqu'à l'annulation de ctx.
func (a *Alerts) Run(ctx context.Context) error {
	every := a.Every
	if every <= 0 {
		every = DefaultAlertEvery
	}
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		if n, err := a.RunOnce(ctx); err != nil {
			if ctx.Err() == nil {
				a.log().Error("alertes: tour en échec", "error", err)
			}
		} else if n > 0 {
			a.log().Info("alertes: messages enfilés", "count", n)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
		}
	}
}

func (a *Alerts) log() *slog.Logger {
	if a.Logger != nil {
		return a.Logger
	}
	return slog.Default()
}

// RunOnce évalue les conditions pour tous les cabinets et retourne le nombre
// d'emails effectivement enfilés (les doublons ne comptent pas).
func (a *Alerts) RunOnce(ctx context.Context) (int, error) {
	now := a.Clock.Now()
	q := sqlcgen.New(a.Pool)
	total := 0
	after := uuid.Nil
	for {
		practices, err := q.ListPracticesPage(ctx, sqlcgen.ListPracticesPageParams{AfterID: after, PageSize: practicePageSize})
		if err != nil {
			return total, fmt.Errorf("liste des cabinets: %w", err)
		}
		for _, pc := range practices {
			n, err := a.runPractice(ctx, q, pc, now)
			total += n
			if err != nil {
				a.log().Error("alertes: cabinet en échec", "practice_id", pc.ID, "error", err)
			}
		}
		if len(practices) < practicePageSize {
			return total, nil
		}
		after = practices[len(practices)-1].ID
	}
}

func (a *Alerts) runPractice(ctx context.Context, q *sqlcgen.Queries, pc sqlcgen.Practice, now time.Time) (int, error) {
	total := 0
	failed, err := q.CountFailedOutboxSince(ctx, sqlcgen.CountFailedOutboxSinceParams{PracticeID: pc.ID, Since: now.Add(-AlertFailedWindow)})
	if err != nil {
		return total, fmt.Errorf("échecs récents: %w", err)
	}
	if failed > 0 {
		n, err := a.alert(ctx, q, pc.ID, "failed:"+now.UTC().Format("2006010215"),
			"Messages en échec",
			fmt.Sprintf("%d message(s) email ou SMS n'ont pas pu être envoyés depuis moins d'une heure. Consultez la liste des messages pour les renvoyer.", failed))
		total += n
		if err != nil {
			return total, err
		}
	}
	if a.BackupDir != "" {
		last, err := ReadLastBackup(a.BackupDir)
		if err != nil {
			return total, err
		}
		// Sans sauvegarde enregistrée, on part de la création du cabinet (pas d'alerte le jour de l'installation).
		ref := last
		if ref.IsZero() || pc.CreatedAt.After(ref) {
			ref = pc.CreatedAt
		}
		if now.Sub(ref) > AlertBackupMaxAge {
			n, err := a.alert(ctx, q, pc.ID, "backup:"+now.UTC().Format("20060102"),
				"Aucune sauvegarde récente",
				"Aucune sauvegarde réussie n'est enregistrée depuis plus de 48 heures. Vérifiez le service de sauvegarde.")
			total += n
			if err != nil {
				return total, err
			}
		}
	}
	return total, nil
}

// alert enfile l'alerte pour chaque admin du cabinet qui ne l'a pas déjà reçue
// pour cette fenêtre.
func (a *Alerts) alert(ctx context.Context, q *sqlcgen.Queries, practiceID uuid.UUID, window, title, message string) (int, error) {
	admins, err := q.ListPracticeAdminEmails(ctx, practiceID)
	if err != nil {
		return 0, fmt.Errorf("admins du cabinet: %w", err)
	}
	statusURL := ""
	if a.BaseURL != "" {
		statusURL = a.BaseURL + "/admin/status"
	}
	payload, err := json.Marshal(map[string]string{"title": title, "message": message, "status_url": statusURL})
	if err != nil {
		return 0, fmt.Errorf("payload alerte: %w", err)
	}
	queued := 0
	for _, ad := range admins {
		key := fmt.Sprintf("alert:%s:%s:%s", window, practiceID, ad.ID)
		exists, err := q.OutboxDedupExists(ctx, pgtype.Text{String: key, Valid: true})
		if err != nil {
			return queued, fmt.Errorf("lecture alerte: %w", err)
		}
		if exists {
			continue // déjà alerté dans cette fenêtre
		}
		err = store.WithTx(ctx, a.Pool, func(_ *sqlcgen.Queries, tx pgx.Tx) error {
			return outbox.Enqueue(ctx, tx, outbox.Msg{
				PracticeID: practiceID, Kind: outbox.KindEmail, Template: TemplateAdminAlert,
				Payload: payload, Recipient: ad.Email, DedupKey: key,
			})
		})
		if err != nil {
			return queued, err
		}
		queued++
	}
	return queued, nil
}
