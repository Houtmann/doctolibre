package domain

import "regexp"

// DefaultColor est la couleur d'un motif ou d'un praticien sans couleur valide
// (même valeur que le défaut des colonnes `color` en base).
const DefaultColor = "#2563eb"

var colorRe = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

// ValidColor indique si s est une couleur au format #rrggbb.
func ValidColor(s string) bool { return colorRe.MatchString(s) }

// ColorOrDefault retourne s si c'est une couleur #rrggbb, sinon DefaultColor.
// À appliquer à toute couleur lue en base avant de l'exposer (agenda, JSON).
func ColorOrDefault(s string) string {
	if ValidColor(s) {
		return s
	}
	return DefaultColor
}
