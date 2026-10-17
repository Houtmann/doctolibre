package domain

import (
	"slices"

	"github.com/google/uuid"
)

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
// service : admin et secrétariat voient tous les agendas, un praticien le sien et
// ceux qui lui sont délégués aujourd'hui (delegated, T059 : praticiens dont
// l'accès est ouvert au jour courant du cabinet, rechargés à chaque requête).
func MayAccessPractitioner(role string, own uuid.UUID, delegated []uuid.UUID, target uuid.UUID) bool {
	switch role {
	case RoleAdmin, RoleSecretary:
		return true
	case RolePractitioner:
		if target == uuid.Nil || (own != uuid.Nil && target == own) {
			return true
		}
		return slices.Contains(delegated, target)
	}
	return false
}
