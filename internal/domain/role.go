package domain

import "github.com/google/uuid"

// Rôles des utilisateurs du cabinet (contrainte CHECK de app_user.role).
const (
	RoleAdmin        = "admin"
	RolePractitioner = "practitioner"
	RoleSecretary    = "secretary"
)

// ValidRole indique si r est un rôle connu.
func ValidRole(r string) bool {
	return r == RoleAdmin || r == RolePractitioner || r == RoleSecretary
}

// MayAccessPractitioner indique si un utilisateur de ce rôle, lié au praticien
// own (uuid.Nil = aucun), peut agir sur l'agenda du praticien target
// (uuid.Nil = « le sien »). Règle unique, partagée par auth.Can et la couche
// service : admin et secrétariat voient tous les agendas, un praticien le sien seulement.
func MayAccessPractitioner(role string, own, target uuid.UUID) bool {
	switch role {
	case RoleAdmin, RoleSecretary:
		return true
	case RolePractitioner:
		return target == uuid.Nil || (own != uuid.Nil && target == own)
	}
	return false
}
