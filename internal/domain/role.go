package domain

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
