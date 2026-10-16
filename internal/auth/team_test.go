package auth_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"doctolibre/internal/auth"
	"doctolibre/internal/domain"
	"doctolibre/internal/testutil"
)

const (
	teamBase  = "https://rdv.example.test"
	teamEmail = "Nouvelle@Example.test"
)

type teamEnv struct {
	svc   *auth.Service
	clock *testutil.FixedClock
	db    *pgxpool.Pool
	admin auth.User
}

func newTeamEnv(t *testing.T) teamEnv {
	t.Helper()
	db := testutil.DB(t)
	clock := testutil.NewFixedClock(t0)
	u, err := auth.CreateUser(context.Background(), db, auth.CreateUserParams{
		Email: "admin@example.test", Role: domain.RoleAdmin, PracticeName: "Cabinet test", Password: testPassword,
	})
	if err != nil {
		t.Fatal(err)
	}
	return teamEnv{
		svc: &auth.Service{Pool: db, Clock: clock}, clock: clock, db: db,
		admin: auth.User{ID: u.ID, PracticeID: u.PracticeID, Email: u.Email, Role: u.Role},
	}
}

// addMember crée un compte dans le cabinet de l'administrateur.
func (e teamEnv) addMember(t *testing.T, email, role string) auth.User {
	t.Helper()
	u, err := auth.CreateUser(context.Background(), e.db, auth.CreateUserParams{
		Email: email, Role: role, PracticeName: "Cabinet test", Password: testPassword,
	})
	if err != nil {
		t.Fatal(err)
	}
	return auth.User{ID: u.ID, PracticeID: u.PracticeID, Email: u.Email, Role: u.Role}
}

// invitationToken retourne le jeton du dernier email d'invitation de l'outbox.
func (e teamEnv) invitationToken(t *testing.T) string {
	t.Helper()
	var payload []byte
	if err := e.db.QueryRow(context.Background(),
		`SELECT payload FROM outbox_message WHERE template = $1 ORDER BY created_at DESC LIMIT 1`, auth.TemplateInvitation).Scan(&payload); err != nil {
		t.Fatalf("aucun email d'invitation: %v", err)
	}
	var p map[string]string
	if err := json.Unmarshal(payload, &p); err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(p["accept_url"])
	if err != nil || !strings.HasPrefix(p["accept_url"], teamBase+auth.AcceptPath+"?token=") {
		t.Fatalf("accept_url = %q (%v)", p["accept_url"], err)
	}
	return u.Query().Get("token")
}

func TestInviteAndAccept(t *testing.T) {
	t.Parallel()
	e := newTeamEnv(t)
	ctx := context.Background()

	if err := e.svc.Invite(ctx, e.admin, teamBase, teamEmail, domain.RolePractitioner, "192.0.2.1"); err != nil {
		t.Fatal(err)
	}
	token := e.invitationToken(t)
	if n := count(t, e.db, `SELECT count(*) FROM outbox_message WHERE template = $1 AND recipient = $2 AND kind = 'email'`, auth.TemplateInvitation, "nouvelle@example.test"); n != 1 {
		t.Fatalf("emails d'invitation = %d", n)
	}
	// Le jeton n'est stocké que haché.
	if n := count(t, e.db, `SELECT count(*) FROM user_invitation WHERE token_hash = $1`, auth.SessionID(token)); n != 1 {
		t.Errorf("jeton haché absent")
	}
	if n := count(t, e.db, `SELECT count(*) FROM user_invitation WHERE token_hash = $1`, token); n != 0 {
		t.Errorf("jeton stocké en clair")
	}
	info, err := e.svc.Invitation(ctx, token)
	if err != nil || info.Email != "nouvelle@example.test" || info.RoleLabel != "Praticien" || info.PracticeName != "Cabinet test" {
		t.Fatalf("Invitation = %+v, %v", info, err)
	}

	// Mot de passe trop court : le jeton reste utilisable.
	var verr domain.ErrValidation
	if err := e.svc.AcceptInvitation(ctx, token, "court", "192.0.2.1"); !errors.As(err, &verr) {
		t.Fatalf("mot de passe faible: %v", err)
	}
	if err := e.svc.AcceptInvitation(ctx, token, testPassword, "192.0.2.1"); err != nil {
		t.Fatal(err)
	}
	// Usage unique.
	if err := e.svc.AcceptInvitation(ctx, token, testPassword, "192.0.2.1"); !errors.Is(err, auth.ErrInvalidInvitation) {
		t.Errorf("second usage: %v", err)
	}
	if _, err := e.svc.Invitation(ctx, token); !errors.Is(err, auth.ErrInvalidInvitation) {
		t.Errorf("Invitation après usage: %v", err)
	}
	// Le compte existe avec le bon rôle, dans le bon cabinet, et peut se connecter.
	sess, err := e.svc.Login(ctx, auth.LoginParams{Email: teamEmail, Password: testPassword})
	if err != nil {
		t.Fatalf("connexion du nouveau membre: %v", err)
	}
	if sess.User.Role != domain.RolePractitioner || sess.User.PracticeID != e.admin.PracticeID {
		t.Errorf("utilisateur = %+v", sess.User)
	}
	// Inviter une adresse qui a déjà un compte est refusé.
	if err := e.svc.Invite(ctx, e.admin, teamBase, teamEmail, domain.RoleSecretary, ""); !errors.As(err, &verr) {
		t.Errorf("adresse déjà utilisée: %v", err)
	}
}

func TestInvitationExpiresAfter48Hours(t *testing.T) {
	t.Parallel()
	e := newTeamEnv(t)
	ctx := context.Background()
	if err := e.svc.Invite(ctx, e.admin, teamBase, teamEmail, domain.RoleSecretary, ""); err != nil {
		t.Fatal(err)
	}
	token := e.invitationToken(t)
	e.clock.Advance(auth.InvitationTTL - time.Minute)
	if _, err := e.svc.Invitation(ctx, token); err != nil {
		t.Fatalf("avant expiration: %v", err)
	}
	e.clock.Advance(2 * time.Minute)
	if _, err := e.svc.Invitation(ctx, token); !errors.Is(err, auth.ErrInvalidInvitation) {
		t.Errorf("après expiration: %v", err)
	}
	if err := e.svc.AcceptInvitation(ctx, token, testPassword, ""); !errors.Is(err, auth.ErrInvalidInvitation) {
		t.Errorf("acceptation après expiration: %v", err)
	}
	for _, bad := range []string{"", "inconnu", strings.Repeat("x", 500)} {
		if _, err := e.svc.Invitation(ctx, bad); !errors.Is(err, auth.ErrInvalidInvitation) {
			t.Errorf("jeton %q: %v", bad, err)
		}
	}
}

func TestReinviteReplacesPendingInvitation(t *testing.T) {
	t.Parallel()
	e := newTeamEnv(t)
	ctx := context.Background()
	if err := e.svc.Invite(ctx, e.admin, teamBase, teamEmail, domain.RoleSecretary, ""); err != nil {
		t.Fatal(err)
	}
	first := e.invitationToken(t)
	e.clock.Advance(time.Second)
	if err := e.svc.Invite(ctx, e.admin, teamBase, teamEmail, domain.RoleAdmin, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Invitation(ctx, first); !errors.Is(err, auth.ErrInvalidInvitation) {
		t.Errorf("ancienne invitation encore valable: %v", err)
	}
	tv, err := e.svc.Team(ctx, e.admin)
	if err != nil || len(tv.Invitations) != 1 || tv.Invitations[0].Role != domain.RoleAdmin {
		t.Fatalf("Team = %+v, %v", tv, err)
	}
	if err := e.svc.RevokeInvitation(ctx, e.admin, tv.Invitations[0].ID, ""); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.RevokeInvitation(ctx, e.admin, tv.Invitations[0].ID, ""); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("second revoke: %v", err)
	}
}

func TestInviteValidationAndPermissions(t *testing.T) {
	t.Parallel()
	e := newTeamEnv(t)
	ctx := context.Background()
	var verr domain.ErrValidation
	for _, tt := range []struct{ email, role string }{
		{"pas-un-email", domain.RoleSecretary},
		{"", domain.RoleSecretary},
		{"ok@example.test", "root"},
	} {
		if err := e.svc.Invite(ctx, e.admin, teamBase, tt.email, tt.role, ""); !errors.As(err, &verr) {
			t.Errorf("Invite(%q, %q) = %v", tt.email, tt.role, err)
		}
	}
	// Seul l'administrateur gère l'équipe.
	for _, role := range []string{domain.RolePractitioner, domain.RoleSecretary} {
		u := e.addMember(t, role+"@example.test", role)
		if err := e.svc.Invite(ctx, u, teamBase, "x@example.test", domain.RoleSecretary, ""); !errors.Is(err, domain.ErrForbidden) {
			t.Errorf("%s invite: %v", role, err)
		}
		if _, err := e.svc.Team(ctx, u); !errors.Is(err, domain.ErrForbidden) {
			t.Errorf("%s liste l'équipe: %v", role, err)
		}
		if err := e.svc.SetRole(ctx, u, e.admin.ID, domain.RoleSecretary); !errors.Is(err, domain.ErrForbidden) {
			t.Errorf("%s change un rôle: %v", role, err)
		}
		if err := e.svc.SetActive(ctx, u, e.admin.ID, false); !errors.Is(err, domain.ErrForbidden) {
			t.Errorf("%s désactive: %v", role, err)
		}
	}
	if n := count(t, e.db, `SELECT count(*) FROM outbox_message WHERE template = $1`, auth.TemplateInvitation); n != 0 {
		t.Errorf("emails envoyés malgré les refus: %d", n)
	}
}

func TestDeactivateRevokesAccess(t *testing.T) {
	t.Parallel()
	e := newTeamEnv(t)
	ctx := context.Background()
	member := e.addMember(t, "membre@example.test", domain.RolePractitioner)
	sess, err := e.svc.Login(ctx, auth.LoginParams{Email: member.Email, Password: testPassword})
	if err != nil {
		t.Fatal(err)
	}
	// Un praticien lié : sa fiche publique disparaît avec le compte.
	pr := testutil.NewPractitioner(t, e.db, member.PracticeID)
	if _, err := e.db.Exec(ctx, `UPDATE app_user SET practitioner_id = $1 WHERE id = $2`, pr.ID, member.ID); err != nil {
		t.Fatal(err)
	}

	if err := e.svc.SetActive(ctx, e.admin, member.ID, false); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Authenticate(ctx, sess.Token); !errors.Is(err, auth.ErrUnauthenticated) {
		t.Errorf("session d'un compte désactivé: %v", err)
	}
	if _, err := e.svc.Login(ctx, auth.LoginParams{Email: member.Email, Password: testPassword}); !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Errorf("connexion d'un compte désactivé: %v", err)
	}
	if n := count(t, e.db, `SELECT count(*) FROM practitioner WHERE id = $1 AND active`, pr.ID); n != 0 {
		t.Errorf("praticien encore actif")
	}
	// Pas de lien de réinitialisation pour un compte désactivé.
	if err := e.svc.RequestPasswordReset(ctx, teamBase, member.Email, ""); err != nil {
		t.Fatal(err)
	}
	if n := count(t, e.db, `SELECT count(*) FROM password_reset WHERE user_id = $1`, member.ID); n != 0 {
		t.Errorf("jeton de réinitialisation créé pour un compte désactivé")
	}

	if err := e.svc.SetActive(ctx, e.admin, member.ID, true); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Login(ctx, auth.LoginParams{Email: member.Email, Password: testPassword}); err != nil {
		t.Errorf("connexion après réactivation: %v", err)
	}
	if n := count(t, e.db, `SELECT count(*) FROM practitioner WHERE id = $1 AND active`, pr.ID); n != 1 {
		t.Errorf("praticien non réactivé")
	}
}

func TestLastAdminIsProtected(t *testing.T) {
	t.Parallel()
	e := newTeamEnv(t)
	ctx := context.Background()
	var verr domain.ErrValidation
	if err := e.svc.SetActive(ctx, e.admin, e.admin.ID, false); !errors.As(err, &verr) {
		t.Errorf("auto-désactivation: %v", err)
	}
	if err := e.svc.SetRole(ctx, e.admin, e.admin.ID, domain.RoleSecretary); !errors.As(err, &verr) {
		t.Errorf("rétrogradation du dernier admin: %v", err)
	}
	// Avec un second administrateur, la rétrogradation du premier est possible.
	second := e.addMember(t, "second@example.test", domain.RoleAdmin)
	if err := e.svc.SetRole(ctx, e.admin, e.admin.ID, domain.RoleSecretary); err != nil {
		t.Fatalf("rétrogradation avec un second admin: %v", err)
	}
	// Le second est désormais le dernier.
	if err := e.svc.SetRole(ctx, second, second.ID, domain.RolePractitioner); !errors.As(err, &verr) {
		t.Errorf("rétrogradation du nouveau dernier admin: %v", err)
	}
	if err := e.svc.SetRole(ctx, second, e.admin.ID, "root"); !errors.As(err, &verr) {
		t.Errorf("rôle inconnu: %v", err)
	}
	if err := e.svc.SetRole(ctx, second, uuid.New(), domain.RoleSecretary); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("compte inconnu: %v", err)
	}
}

// Un administrateur ne touche pas aux comptes d'un autre cabinet.
func TestTeamIsScopedToPractice(t *testing.T) {
	t.Parallel()
	e := newTeamEnv(t)
	ctx := context.Background()
	foreign, err := auth.CreateUser(ctx, e.db, auth.CreateUserParams{
		Email: "etranger@example.test", Role: domain.RoleAdmin, PracticeName: "Autre cabinet", Password: testPassword,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := e.svc.SetActive(ctx, e.admin, foreign.ID, false); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("désactivation cross-cabinet: %v", err)
	}
	if err := e.svc.SetRole(ctx, e.admin, foreign.ID, domain.RoleSecretary); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("changement de rôle cross-cabinet: %v", err)
	}
	tv, err := e.svc.Team(ctx, e.admin)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range tv.Members {
		if m.ID == foreign.ID {
			t.Errorf("membre d'un autre cabinet listé")
		}
	}
}
