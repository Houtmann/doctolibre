package domain

import (
	"errors"
	"fmt"
)

// ValidationErrors regroupe plusieurs erreurs de validation (un formulaire peut
// en avoir plusieurs à afficher d'un coup).
type ValidationErrors []ErrValidation

func (v ValidationErrors) Error() string {
	if len(v) == 0 {
		return "validation"
	}
	return fmt.Sprintf("validation: %d erreur(s), première : %s: %s", len(v), v[0].Field, v[0].Msg)
}

// FieldErrors extrait les erreurs de validation de err (une seule ou un groupe) ;
// nil si err n'est pas une erreur de validation.
func FieldErrors(err error) []ErrValidation {
	var vs ValidationErrors
	if errors.As(err, &vs) {
		return vs
	}
	var v ErrValidation
	if errors.As(err, &v) {
		return []ErrValidation{v}
	}
	return nil
}
