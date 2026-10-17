package auth_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"doctolibre/internal/auth"
	"doctolibre/internal/domain"
	"doctolibre/internal/store/sqlcgen"
	"doctolibre/internal/testutil"
)

// delegEnv : un cabinet (Europe/Paris) avec un administrateur, un remplaçant
// (praticien) et un praticien titulaire.
type delegEnv struct {
	teamEnv
	replacement auth.User
	titular     sqlcgen.Practitioner
}

func newDelegEnv(t *testing.T) delegEnv {
	t.Helper()
	e := delegEnv{teamEnv: newTeamEnv(t)}
	e.replacement = e.addMember(t, "remplacant@example.test", domain.RolePractitioner)
	e.titular = testutil.NewPractitioner(t, e.db, e.admin.PracticeID)
	return e
}

// delegatedAt authentifie le remplaçant avec une horloge à l'instant at (session
// créée une heure avant, sans passer par Login) et retourne ses praticiens délégués.
func (e delegEnv) delegatedAt(t *testing.T, at time.Time) []uuid.UUID {
	t.Helper()
	ctx := context.Background()
	token := "tok-" + uuid.NewString()
	if _, err := sqlcgen.New(e.db).CreateSession(ctx, sqlcgen.CreateSessionParams{
		ID: auth.SessionID(token), UserID: e.replacement.ID, CreatedAt: at.Add(-time.Hour), ExpiresAt: at.Add(time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	svc := &auth.Service{Pool: e.db, Clock: testutil.NewFixedClock(at)}
	sess, err := svc.Authenticate(ctx, token)
	if err != nil {
		t.Fatalf("Authenticate à %s: %v", at, err)
	}
	return sess.User.Delegated
}

func (e delegEnv) delegate(t *testing.T, from, to string) error {
	t.Helper()
	return e.svc.CreateDelegation(context.Background(), e.admin, auth.DelegationInput{
		UserID: e.replacement.ID, PractitionerID: e.titular.ID, From: from, To: to,
	})
}

func utc(y int, m time.Month, d, h, mi, s int) time.Time {
	return time.Date(y, m, d, h, mi, s, 0, time.UTC)
}

// Frontières de jour en Europe/Paris, y compris le jour du changement d'heure
// d'automne (25/10/2026 : 25 h) et de printemps (29/03/2026 : 23 h).
func TestDelegationDayBoundariesParis(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		from, to string
		now      time.Time
		want     bool
	}{
		// Hiver : 00:00 locale = 23:00 UTC la veille.
		{"hiver, 23:59:59 la veille", "2026-12-10", "2026-12-12", utc(2026, 12, 9, 22, 59, 59), false},
		{"hiver, 00:00 locale", "2026-12-10", "2026-12-12", utc(2026, 12, 9, 23, 0, 0), true},
		{"hiver, 23:59:59 dernier jour", "2026-12-10", "2026-12-12", utc(2026, 12, 12, 22, 59, 59), true},
		{"hiver, 00:00 lendemain", "2026-12-10", "2026-12-12", utc(2026, 12, 12, 23, 0, 0), false},
		// Été (UTC+2).
		{"été, 23:59:59 la veille", "2026-07-10", "2026-07-10", utc(2026, 7, 9, 21, 59, 59), false},
		{"été, 00:00 locale", "2026-07-10", "2026-07-10", utc(2026, 7, 9, 22, 0, 0), true},
		{"été, 00:00 lendemain", "2026-07-10", "2026-07-10", utc(2026, 7, 10, 22, 0, 0), false},
		// Passage à l'heure d'hiver, dimanche 25/10/2026 (jour de 25 h).
		{"DST automne, veille 23:59:59", "2026-10-25", "2026-10-25", utc(2026, 10, 24, 21, 59, 59), false},
		{"DST automne, 00:00 locale", "2026-10-25", "2026-10-25", utc(2026, 10, 24, 22, 0, 0), true},
		{"DST automne, 23:59:59 locale", "2026-10-25", "2026-10-25", utc(2026, 10, 25, 22, 59, 59), true},
		{"DST automne, 00:00 lendemain", "2026-10-25", "2026-10-25", utc(2026, 10, 25, 23, 0, 0), false},
		// Passage à l'heure d'été, dimanche 29/03/2026 (jour de 23 h).
		{"DST printemps, veille 23:59:59", "2026-03-29", "2026-03-29", utc(2026, 3, 28, 22, 59, 59), false},
		{"DST printemps, 00:00 locale", "2026-03-29", "2026-03-29", utc(2026, 3, 28, 23, 0, 0), true},
		{"DST printemps, 23:59:59 locale", "2026-03-29", "2026-03-29", utc(2026, 3, 29, 21, 59, 59), true},
		{"DST printemps, 00:00 lendemain", "2026-03-29", "2026-03-29", utc(2026, 3, 29, 22, 0, 0), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			e := newDelegEnv(t)
			// Créée sous une horloge antérieure à la période (sinon « fin passée » la refuse) ;
			// delegatedAt évalue ensuite Authenticate à l'instant voulu.
			e.svc.Clock = testutil.NewFixedClock(utc(2026, 1, 1, 9, 0, 0))
			if err := e.delegate(t, tc.from, tc.to); err != nil {
				t.Fatal(err)
			}
			got := e.delegatedAt(t, tc.now)
			if has := len(got) == 1 && got[0] == e.titular.ID; has != tc.want || (!tc.want && len(got) != 0) {
				t.Errorf("délégués à %s = %v, accès attendu: %v", tc.now, got, tc.want)
			}
		})
	}
}

// Le fuseau du cabinet décide du jour, pas UTC : un cabinet à Cayenne (UTC-3).
func TestDelegationUsesPracticeTimezone(t *testing.T) {
	t.Parallel()
	e := newDelegEnv(t)
	if _, err := e.db.Exec(context.Background(), `UPDATE practice SET timezone = 'America/Cayenne' WHERE id = $1`, e.admin.PracticeID); err != nil {
		t.Fatal(err)
	}
	e.svc.Clock = testutil.NewFixedClock(utc(2026, 1, 1, 9, 0, 0))
	if err := e.delegate(t, "2026-12-10", "2026-12-10"); err != nil {
		t.Fatal(err)
	}
	// 00:00 à Cayenne = 03:00 UTC.
	if got := e.delegatedAt(t, utc(2026, 12, 10, 2, 59, 59)); len(got) != 0 {
		t.Errorf("avant minuit local: %v", got)
	}
	if got := e.delegatedAt(t, utc(2026, 12, 10, 3, 0, 0)); len(got) != 1 {
		t.Errorf("à minuit local: %v", got)
	}
	if got := e.delegatedAt(t, utc(2026, 12, 11, 2, 59, 59)); len(got) != 1 {
		t.Errorf("dernière seconde: %v", got)
	}
	if got := e.delegatedAt(t, utc(2026, 12, 11, 3, 0, 0)); len(got) != 0 {
		t.Errorf("lendemain local: %v", got)
	}
}

// Révocation immédiate : la requête suivante ne voit plus la délégation, et un
// praticien désactivé n'est plus délégué.
func TestRevokeDelegationIsImmediate(t *testing.T) {
	t.Parallel()
	e := newDelegEnv(t)
	ctx := context.Background()
	if err := e.delegate(t, "2026-10-15", "2026-10-20"); err != nil {
		t.Fatal(err)
	}
	now := utc(2026, 10, 16, 9, 0, 0)
	if got := e.delegatedAt(t, now); len(got) != 1 {
		t.Fatalf("délégués = %v", got)
	}
	ds, err := e.svc.Delegations(ctx, e.admin)
	if err != nil || len(ds) != 1 {
		t.Fatalf("Delegations = %v, %v", ds, err)
	}
	if err := e.svc.RevokeDelegation(ctx, e.admin, ds[0].ID); err != nil {
		t.Fatal(err)
	}
	if got := e.delegatedAt(t, now); len(got) != 0 {
		t.Errorf("après révocation: %v", got)
	}
	if err := e.svc.RevokeDelegation(ctx, e.admin, ds[0].ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("seconde révocation = %v, attendu ErrNotFound", err)
	}
}

func TestCreateDelegationGuards(t *testing.T) {
	t.Parallel()
	e := newDelegEnv(t)
	ctx := context.Background()
	secretary := e.addMember(t, "secretaire@example.test", domain.RoleSecretary)
	otherAdminU, err := auth.CreateUser(ctx, e.db, auth.CreateUserParams{
		Email: "admin-autre@example.test", Role: domain.RoleAdmin, PracticeName: "Autre cabinet", Password: testPassword,
	})
	if err != nil {
		t.Fatal(err)
	}
	otherAdmin := auth.User{ID: otherAdminU.ID, PracticeID: otherAdminU.PracticeID, Role: otherAdminU.Role}
	otherPractitioner := testutil.NewPractitioner(t, e.db, otherAdmin.PracticeID)

	in := auth.DelegationInput{UserID: e.replacement.ID, PractitionerID: e.titular.ID, From: "2026-10-16", To: "2026-10-18"}

	// Un non-administrateur ne crée ni ne révoque de délégation.
	for _, u := range []auth.User{e.replacement, secretary} {
		if err := e.svc.CreateDelegation(ctx, u, in); !errors.Is(err, domain.ErrForbidden) {
			t.Errorf("création par %s = %v, attendu ErrForbidden", u.Role, err)
		}
		if err := e.svc.RevokeDelegation(ctx, u, uuid.New()); !errors.Is(err, domain.ErrForbidden) {
			t.Errorf("révocation par %s = %v, attendu ErrForbidden", u.Role, err)
		}
		if _, err := e.svc.Delegations(ctx, u); !errors.Is(err, domain.ErrForbidden) {
			t.Errorf("liste par %s = %v, attendu ErrForbidden", u.Role, err)
		}
	}

	// Autre cabinet : le compte, ou le praticien, n'existe pas pour cet administrateur.
	if err := e.svc.CreateDelegation(ctx, otherAdmin, in); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("admin d'un autre cabinet = %v, attendu ErrNotFound", err)
	}
	cross := in
	cross.PractitionerID = otherPractitioner.ID
	if err := e.svc.CreateDelegation(ctx, e.admin, cross); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("praticien d'un autre cabinet = %v, attendu ErrNotFound", err)
	}
	cross = in
	cross.UserID = otherAdmin.ID
	if err := e.svc.CreateDelegation(ctx, e.admin, cross); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("compte d'un autre cabinet = %v, attendu ErrNotFound", err)
	}
	// Un admin d'un autre cabinet ne révoque pas la délégation d'un cabinet voisin.
	if err := e.svc.CreateDelegation(ctx, e.admin, in); err != nil {
		t.Fatal(err)
	}
	ds, _ := e.svc.Delegations(ctx, e.admin)
	if err := e.svc.RevokeDelegation(ctx, otherAdmin, ds[0].ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("révocation inter-cabinet = %v, attendu ErrNotFound", err)
	}
	if other, _ := e.svc.Delegations(ctx, otherAdmin); len(other) != 0 {
		t.Errorf("liste inter-cabinet = %v", other)
	}

	// Validations.
	for _, tc := range []struct {
		name string
		mod  func(*auth.DelegationInput)
	}{
		{"fin avant début", func(i *auth.DelegationInput) { i.From, i.To = "2026-10-20", "2026-10-19" }},
		{"date invalide", func(i *auth.DelegationInput) { i.From = "demain" }},
		{"fin passée", func(i *auth.DelegationInput) { i.From, i.To = "2026-10-01", "2026-10-10" }},
		{"trop longue", func(i *auth.DelegationInput) { i.From, i.To = "2026-10-16", "2028-10-16" }},
		{"chevauchement", func(i *auth.DelegationInput) { i.From, i.To = "2026-10-18", "2026-10-25" }},
		{"secrétariat", func(i *auth.DelegationInput) { i.UserID = secretary.ID; i.From, i.To = "2026-11-01", "2026-11-02" }},
	} {
		bad := in
		tc.mod(&bad)
		if err := e.svc.CreateDelegation(ctx, e.admin, bad); len(domain.FieldErrors(err)) == 0 {
			t.Errorf("%s: err = %v, attendu une erreur de validation", tc.name, err)
		}
	}
	// Jours adjacents : pas de chevauchement.
	adj := in
	adj.From, adj.To = "2026-10-19", "2026-10-20"
	if err := e.svc.CreateDelegation(ctx, e.admin, adj); err != nil {
		t.Errorf("période adjacente: %v", err)
	}
	// La date de fin « aujourd'hui » (jour local) est acceptée.
	e.svc.Clock = testutil.NewFixedClock(utc(2026, 10, 15, 22, 30, 0)) // 00:30 le 16 à Paris
	last := in
	last.From, last.To = "2026-11-01", "2026-11-01"
	if err := e.svc.CreateDelegation(ctx, e.admin, last); err != nil {
		t.Errorf("fin dans le futur: %v", err)
	}
	past := in
	past.From, past.To = "2026-10-15", "2026-10-15" // déjà passé en heure locale
	past.UserID = e.replacement.ID
	if err := e.svc.CreateDelegation(ctx, e.admin, past); len(domain.FieldErrors(err)) == 0 {
		t.Errorf("hier en heure locale accepté: %v", err)
	}
}

// Audit : création et révocation tracées au nom de l'administrateur, sans donnée personnelle.
func TestDelegationAudit(t *testing.T) {
	t.Parallel()
	e := newDelegEnv(t)
	ctx := context.Background()
	e.svc.Clock = testutil.NewFixedClock(utc(2026, 10, 15, 9, 0, 0))
	if err := e.delegate(t, "2026-10-16", "2026-10-18"); err != nil {
		t.Fatal(err)
	}
	ds, _ := e.svc.Delegations(ctx, e.admin)
	if err := e.svc.RevokeDelegation(ctx, e.admin, ds[0].ID); err != nil {
		t.Fatal(err)
	}
	rows, err := e.db.Query(ctx, `SELECT action, user_id, actor, entity_id, meta::text FROM audit_log WHERE entity = 'practitioner_delegation' ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var actions []string
	for rows.Next() {
		var action, actor, meta string
		var userID, entityID uuid.UUID
		if err := rows.Scan(&action, &userID, &actor, &entityID, &meta); err != nil {
			t.Fatal(err)
		}
		actions = append(actions, action)
		if userID != e.admin.ID || actor != "user:"+e.admin.ID.String() || entityID != ds[0].ID {
			t.Errorf("%s: user=%v actor=%q entity=%v", action, userID, actor, entityID)
		}
		if strings.Contains(meta, "@") || !strings.Contains(meta, e.replacement.ID.String()) {
			t.Errorf("%s: meta = %s", action, meta)
		}
	}
	if len(actions) != 2 || actions[0] != auth.ActionDelegationCreated || actions[1] != auth.ActionDelegationRevoked {
		t.Errorf("actions = %v", actions)
	}
}
