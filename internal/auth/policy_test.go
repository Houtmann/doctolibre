package auth_test

import (
	"testing"

	"github.com/google/uuid"

	"doctolibre/internal/auth"
	"doctolibre/internal/domain"
)

func TestCan(t *testing.T) {
	t.Parallel()
	practice, other := uuid.New(), uuid.New()
	mine, theirs := uuid.New(), uuid.New()
	user := func(role string, own uuid.UUID) auth.User {
		u := auth.User{ID: uuid.New(), PracticeID: practice, Role: role}
		if own != uuid.Nil {
			u.PractitionerID = uuid.NullUUID{UUID: own, Valid: true}
		}
		return u
	}
	admin, secretary := user(domain.RoleAdmin, uuid.Nil), user(domain.RoleSecretary, uuid.Nil)
	practitioner := user(domain.RolePractitioner, mine)
	unlinked := user(domain.RolePractitioner, uuid.Nil) // praticien dont le profil n'est pas encore créé

	allActions := []auth.Action{
		auth.ActionCalendarView, auth.ActionAppointmentManage, auth.ActionPatientsManage, auth.ActionAvailabilityEdit,
		auth.ActionPracticeConfig, auth.ActionCarePlanManage, auth.ActionTeamManage, auth.ActionMessagesView,
	}
	tests := []struct {
		name string
		u    auth.User
		a    auth.Action
		res  auth.Resource
		want bool
	}{
		// admin : tout, sur n'importe quel praticien du cabinet.
		{"admin agenda d'un autre", admin, auth.ActionCalendarView, auth.Resource{PractitionerID: theirs}, true},
		{"admin horaires d'un autre", admin, auth.ActionAvailabilityEdit, auth.Resource{PractitionerID: theirs}, true},
		{"admin équipe", admin, auth.ActionTeamManage, auth.Resource{}, true},
		{"admin réglages", admin, auth.ActionPracticeConfig, auth.Resource{}, true},
		// secrétariat : agendas et patients de tous, pas les réglages.
		{"secrétaire agenda d'un autre", secretary, auth.ActionCalendarView, auth.Resource{PractitionerID: theirs}, true},
		{"secrétaire gère les RDV d'un autre", secretary, auth.ActionAppointmentManage, auth.Resource{PractitionerID: theirs}, true},
		{"secrétaire patients", secretary, auth.ActionPatientsManage, auth.Resource{}, true},
		{"secrétaire horaires", secretary, auth.ActionAvailabilityEdit, auth.Resource{PractitionerID: theirs}, false},
		{"secrétaire horaires sans cible", secretary, auth.ActionAvailabilityEdit, auth.Resource{}, false},
		{"secrétaire réglages", secretary, auth.ActionPracticeConfig, auth.Resource{}, false},
		{"secrétaire équipe", secretary, auth.ActionTeamManage, auth.Resource{}, false},
		{"secrétaire séries", secretary, auth.ActionCarePlanManage, auth.Resource{}, false},
		// praticien : son agenda, ses horaires, les patients du cabinet.
		{"praticien son agenda", practitioner, auth.ActionCalendarView, auth.Resource{PractitionerID: mine}, true},
		{"praticien agenda implicite", practitioner, auth.ActionCalendarView, auth.Resource{}, true},
		{"praticien agenda d'un autre", practitioner, auth.ActionCalendarView, auth.Resource{PractitionerID: theirs}, false},
		{"praticien RDV d'un autre", practitioner, auth.ActionAppointmentManage, auth.Resource{PractitionerID: theirs}, false},
		{"praticien ses horaires", practitioner, auth.ActionAvailabilityEdit, auth.Resource{PractitionerID: mine}, true},
		{"praticien horaires d'un autre", practitioner, auth.ActionAvailabilityEdit, auth.Resource{PractitionerID: theirs}, false},
		{"praticien patients", practitioner, auth.ActionPatientsManage, auth.Resource{}, true},
		{"praticien équipe", practitioner, auth.ActionTeamManage, auth.Resource{}, false},
		{"praticien sans profil, cible explicite", unlinked, auth.ActionCalendarView, auth.Resource{PractitionerID: theirs}, false},
		{"praticien sans profil, agenda implicite", unlinked, auth.ActionCalendarView, auth.Resource{}, true},
		// cabinet différent.
		{"autre cabinet (admin)", admin, auth.ActionCalendarView, auth.Resource{PracticeID: other}, false},
		{"même cabinet explicite", admin, auth.ActionCalendarView, auth.Resource{PracticeID: practice}, true},
		// défaut : refus.
		{"rôle inconnu", user("intrus", uuid.Nil), auth.ActionPatientsManage, auth.Resource{}, false},
		{"action inconnue", admin, auth.Action("inconnue"), auth.Resource{}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := auth.Can(tt.u, tt.a, tt.res); got != tt.want {
				t.Errorf("Can(%s, %s) = %v, attendu %v", tt.u.Role, tt.a, got, tt.want)
			}
		})
	}

	// Aucun rôle ne sort du cabinet, quelle que soit l'action.
	for _, u := range []auth.User{admin, secretary, practitioner} {
		for _, a := range allActions {
			if auth.Can(u, a, auth.Resource{PracticeID: other}) {
				t.Errorf("%s peut %s dans un autre cabinet", u.Role, a)
			}
		}
	}
}

// Une délégation n'ouvre que les agendas visés, pour les actions d'agenda ; jamais
// les réglages du cabinet ni un autre cabinet.
func TestCanWithDelegation(t *testing.T) {
	t.Parallel()
	practice := uuid.New()
	mine, lent, other := uuid.New(), uuid.New(), uuid.New()
	u := auth.User{
		ID: uuid.New(), PracticeID: practice, Role: domain.RolePractitioner,
		PractitionerID: uuid.NullUUID{UUID: mine, Valid: true}, Delegated: []uuid.UUID{lent},
	}
	for _, a := range []auth.Action{auth.ActionCalendarView, auth.ActionAppointmentManage, auth.ActionAvailabilityEdit} {
		if !auth.Can(u, a, auth.Resource{PracticeID: practice, PractitionerID: lent}) {
			t.Errorf("%s sur l'agenda délégué refusé", a)
		}
		if auth.Can(u, a, auth.Resource{PracticeID: practice, PractitionerID: other}) {
			t.Errorf("%s sur un agenda non délégué accepté", a)
		}
		if auth.Can(u, a, auth.Resource{PracticeID: uuid.New(), PractitionerID: lent}) {
			t.Errorf("%s sur un autre cabinet accepté", a)
		}
	}
	if auth.Can(u, auth.ActionTeamManage, auth.Resource{PracticeID: practice}) {
		t.Error("la délégation ouvre la gestion d'équipe")
	}
	u.Delegated = nil
	if auth.Can(u, auth.ActionCalendarView, auth.Resource{PracticeID: practice, PractitionerID: lent}) {
		t.Error("accès sans délégation")
	}
}
