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
	// ErrOnlineBookingBlocked : la réservation en ligne est refusée à ce patient
	// (absences répétées). Ne jamais en révéler la cause au patient.
	ErrOnlineBookingBlocked = errors.New("online booking blocked")
)

// ErrValidation signale une donnée invalide sur un champ.
type ErrValidation struct {
	Field string
	Msg   string
}

func (e ErrValidation) Error() string {
	return fmt.Sprintf("validation: %s: %s", e.Field, e.Msg)
}
