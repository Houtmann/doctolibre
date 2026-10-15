package domain

import (
	"errors"
	"fmt"
)

// Erreurs métier sentinelles ; les handlers les mappent vers un statut HTTP.
var (
	// ErrSlotTaken : le créneau n'est plus disponible (contrainte d'exclusion).
	ErrSlotTaken = errors.New("slot taken")
	// ErrNotFound : ressource inexistante (ou hors du cabinet).
	ErrNotFound = errors.New("not found")
	// ErrForbidden : action interdite pour l'utilisateur courant.
	ErrForbidden = errors.New("forbidden")
)

// ErrValidation signale une donnée invalide sur un champ.
type ErrValidation struct {
	Field string
	Msg   string
}

func (e ErrValidation) Error() string {
	return fmt.Sprintf("validation: %s: %s", e.Field, e.Msg)
}
