package auth

import (
	"github.com/google/uuid"

	"doctolibre/internal/domain"
)

// Action est une opération soumise à la matrice de permissions.
type Action string

// Actions de la matrice (T033). Une seule table, testée : ne pas dupliquer les
// vérifications de rôle dans les handlers.
const (
	// ActionCalendarView : lire l'agenda d'un praticien.
	ActionCalendarView Action = "calendar.view"
	// ActionAppointmentManage : créer, déplacer, annuler un RDV d'un praticien.
	ActionAppointmentManage Action = "appointment.manage"
	// ActionPatientsManage : fiches patients du cabinet (lecture/écriture).
	ActionPatientsManage Action = "patients.manage"
	// ActionAvailabilityEdit : horaires hebdomadaires et absences d'un praticien.
	ActionAvailabilityEdit Action = "availability.edit"
	// ActionPracticeConfig : paramètres du cabinet, profil, motifs, notifications.
	ActionPracticeConfig Action = "practice.config"
	// ActionCarePlanManage : séries de séances (T030).
	ActionCarePlanManage Action = "careplan.manage"
	// ActionTeamManage : inviter, changer le rôle, désactiver un membre.
	ActionTeamManage Action = "team.manage"
	// ActionMessagesView : journal des messages envoyés.
	ActionMessagesView Action = "messages.view"
)

// Resource désigne la cible d'une action. Les champs à zéro sont ignorés :
// PracticeID zéro = cabinet de l'utilisateur ; PractitionerID zéro = « son propre agenda ».
type Resource struct {
	PracticeID     uuid.UUID
	PractitionerID uuid.UUID
}

// Can indique si u peut effectuer a sur res. Fonction pure ; refuse tout par
// défaut (rôle ou action inconnus, autre cabinet).
func Can(u User, a Action, res Resource) bool {
	if res.PracticeID != uuid.Nil && res.PracticeID != u.PracticeID {
		return false
	}
	own := uuid.Nil
	if u.PractitionerID.Valid {
		own = u.PractitionerID.UUID
	}
	switch a {
	case ActionCalendarView, ActionAppointmentManage:
		return domain.MayAccessPractitioner(u.Role, own, u.Delegated, res.PractitionerID)
	case ActionPatientsManage:
		return domain.ValidRole(u.Role)
	case ActionAvailabilityEdit:
		// Le secrétariat ne règle pas les horaires ; un praticien règle les siens.
		return u.Role != domain.RoleSecretary && domain.MayAccessPractitioner(u.Role, own, u.Delegated, res.PractitionerID)
	case ActionPracticeConfig, ActionCarePlanManage:
		return u.Role == domain.RoleAdmin || u.Role == domain.RolePractitioner
	case ActionTeamManage:
		return u.Role == domain.RoleAdmin
	case ActionMessagesView:
		return domain.ValidRole(u.Role)
	}
	return false
}
