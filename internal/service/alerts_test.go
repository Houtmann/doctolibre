package service_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"doctolibre/internal/auth"
	"doctolibre/internal/domain"
	"doctolibre/internal/service"
	"doctolibre/internal/testutil"
)

func alertsEnv(t *testing.T, now time.Time) (*pgxpool.Pool, *testutil.FixedClock, *service.Alerts, auth.CreateUserParams) {
	t.Helper()
	db := testutil.DB(t)
	//nolint:gosec // mot de passe de test
	p := auth.CreateUserParams{Email: "admin@example.test", Role: domain.RoleAdmin, PracticeName: "Cabinet alertes", Password: "un-mot-de-passe-solide"}
	if _, err := auth.CreateUser(context.Background(), db, p); err != nil {
		t.Fatal(err)
	}
	// Un secrétaire ne reçoit pas les alertes.
	sec := p
	sec.Email, sec.Role = "secretaire@example.test", domain.RoleSecretary
	if _, err := auth.CreateUser(context.Background(), db, sec); err != nil {
		t.Fatal(err)
	}
	clock := testutil.NewFixedClock(now)
	return db, clock, &service.Alerts{Pool: db, Clock: clock}, p
}

func setClock(c *testutil.FixedClock, t time.Time) { c.Advance(t.Sub(c.Now())) }

func alertCount(t *testing.T, db *pgxpool.Pool) int {
	t.Helper()
	var n int
	if err := db.QueryRow(context.Background(), "SELECT count(*) FROM outbox_message WHERE template = 'admin_alert'").Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestAlertsFailedMessagesOncePerWindow(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC().Truncate(time.Hour).Add(5 * time.Minute)
	db, clock, al, _ := alertsEnv(t, now)
	ctx := context.Background()

	if n, err := al.RunOnce(ctx); err != nil || n != 0 {
		t.Fatalf("sans échec: n=%d err=%v", n, err)
	}
	// Un message en échec il y a 3 minutes.
	if _, err := db.Exec(ctx, `INSERT INTO outbox_message (practice_id, kind, template, recipient, status, updated_at)
		SELECT id, 'sms', 'reminder', '+33600000000', 'failed', $1 FROM practice`, now.Add(-3*time.Minute)); err != nil {
		t.Fatal(err)
	}
	n, err := al.RunOnce(ctx)
	if err != nil || n != 1 {
		t.Fatalf("1re passe: n=%d err=%v (un seul admin attendu)", n, err)
	}
	for i, d := range []time.Duration{0, 10 * time.Minute, 20 * time.Minute} {
		setClock(clock, now.Add(d))
		if n, err := al.RunOnce(ctx); err != nil || n != 0 {
			t.Fatalf("passe %d dans la même fenêtre: n=%d err=%v", i, n, err)
		}
	}
	if got := alertCount(t, db); got != 1 {
		t.Errorf("alertes enfilées = %d, attendu 1", got)
	}
	// Une alerte elle-même en échec ne déclenche pas de nouvelle alerte.
	if _, err := db.Exec(ctx, "UPDATE outbox_message SET status = 'failed', updated_at = $1 WHERE template = 'admin_alert'", now.Add(15*time.Minute)); err != nil {
		t.Fatal(err)
	}
	setClock(clock, now.Add(20*time.Minute))
	if n, err := al.RunOnce(ctx); err != nil || n != 0 {
		t.Fatalf("alerte en échec: n=%d err=%v", n, err)
	}
	// Le message en échec sort de la fenêtre d'une heure : plus d'alerte, même à l'heure suivante.
	setClock(clock, now.Add(2*time.Hour))
	if n, err := al.RunOnce(ctx); err != nil || n != 0 {
		t.Fatalf("échec hors fenêtre: n=%d err=%v", n, err)
	}
}

func TestAlertsIgnoreScrubbedFailedMessages(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC().Truncate(time.Hour).Add(5 * time.Minute)
	db, _, al, _ := alertsEnv(t, now)
	ctx := context.Background()

	// Vieux message en échec, effacé par la purge à l'instant (updated_at = scrubbed_at = now).
	if _, err := db.Exec(ctx, `INSERT INTO outbox_message (practice_id, kind, template, recipient, status, updated_at, scrubbed_at)
		SELECT id, 'sms', 'reminder', '', 'failed', $1, $1 FROM practice`, now.Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	if n, err := al.RunOnce(ctx); err != nil || n != 0 {
		t.Fatalf("échec effacé: n=%d err=%v", n, err)
	}
	if got := alertCount(t, db); got != 0 {
		t.Errorf("alertes enfilées = %d, attendu 0", got)
	}
}

func TestAlertsSkipInactiveAdmins(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC().Truncate(time.Hour).Add(5 * time.Minute)
	db, _, al, _ := alertsEnv(t, now)
	ctx := context.Background()

	if _, err := db.Exec(ctx, "UPDATE app_user SET active = false WHERE role = 'admin'"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `INSERT INTO outbox_message (practice_id, kind, template, recipient, status, updated_at)
		SELECT id, 'sms', 'reminder', '+33600000000', 'failed', $1 FROM practice`, now.Add(-3*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if n, err := al.RunOnce(ctx); err != nil || n != 0 {
		t.Fatalf("admin désactivé: n=%d err=%v", n, err)
	}
	if got := alertCount(t, db); got != 0 {
		t.Errorf("alertes enfilées = %d, attendu 0", got)
	}
}

func TestAlertsBackupStale(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	now := time.Now().UTC().Add(72 * time.Hour) // le cabinet de test a 72 h
	db, clock, al, _ := alertsEnv(t, now)
	al.BackupDir = dir
	ctx := context.Background()

	// Pas de marqueur et cabinet de plus de 48 h : alerte, une seule par jour.
	if n, err := al.RunOnce(ctx); err != nil || n != 1 {
		t.Fatalf("sans marqueur: n=%d err=%v", n, err)
	}
	setClock(clock, now.Add(time.Hour))
	if n, err := al.RunOnce(ctx); err != nil || n != 0 {
		t.Fatalf("même jour: n=%d err=%v", n, err)
	}
	// Sauvegarde récente : plus d'alerte, même le lendemain.
	stamp := func(at time.Time) {
		if err := os.WriteFile(filepath.Join(dir, service.BackupMarker), []byte(at.UTC().Format(time.RFC3339)+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	next := now.Add(24 * time.Hour)
	stamp(next.Add(-2 * time.Hour))
	setClock(clock, next)
	if n, err := al.RunOnce(ctx); err != nil || n != 0 {
		t.Fatalf("sauvegarde récente: n=%d err=%v", n, err)
	}
	// Marqueur vieux de 49 h : nouvelle alerte (nouveau jour).
	setClock(clock, next.Add(24*time.Hour))
	stamp(clock.Now().Add(-49 * time.Hour))
	if n, err := al.RunOnce(ctx); err != nil || n != 1 {
		t.Fatalf("sauvegarde ancienne: n=%d err=%v", n, err)
	}
	if got := alertCount(t, db); got != 2 {
		t.Errorf("alertes enfilées = %d, attendu 2", got)
	}
}

func TestAlertsBackupNotCheckedOnFreshPracticeOrWithoutDir(t *testing.T) {
	t.Parallel()
	db, _, al, _ := alertsEnv(t, time.Now().UTC())
	ctx := context.Background()
	al.BackupDir = t.TempDir()
	if n, err := al.RunOnce(ctx); err != nil || n != 0 {
		t.Fatalf("cabinet tout neuf: n=%d err=%v", n, err)
	}
	al.BackupDir = ""
	if n, err := al.RunOnce(ctx); err != nil || n != 0 {
		t.Fatalf("suivi désactivé: n=%d err=%v", n, err)
	}
	if got := alertCount(t, db); got != 0 {
		t.Errorf("alertes = %d", got)
	}
}
