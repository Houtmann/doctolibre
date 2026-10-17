package auth_test

import (
	"context"
	"errors"
	"testing"

	"doctolibre/internal/auth"
	"doctolibre/internal/domain"
)

func TestInviteRefusedWhenEmailDisabled(t *testing.T) {
	t.Parallel()
	e := newTeamEnv(t)
	ctx := context.Background()
	if _, err := e.db.Exec(ctx, `INSERT INTO practice_notify_transport (practice_id, kind, mode) VALUES ($1, 'email', 'disabled')`, e.admin.PracticeID); err != nil {
		t.Fatal(err)
	}
	err := e.svc.Invite(ctx, e.admin, teamBase, teamEmail, domain.RolePractitioner, "")
	if !errors.Is(err, auth.ErrEmailDisabled) {
		t.Fatalf("Invite = %v", err)
	}
	var n int
	if err := e.db.QueryRow(ctx, `SELECT (SELECT count(*) FROM user_invitation) + (SELECT count(*) FROM outbox_message WHERE template = $1)`, auth.TemplateInvitation).Scan(&n); err != nil || n != 0 {
		t.Errorf("invitation ou email créés malgré le refus : %d, %v", n, err)
	}
	// Réactivé : l'invitation passe.
	if _, err := e.db.Exec(ctx, `UPDATE practice_notify_transport SET mode = 'server'`); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.Invite(ctx, e.admin, teamBase, teamEmail, domain.RolePractitioner, ""); err != nil {
		t.Fatal(err)
	}
}
