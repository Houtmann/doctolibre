package domain

import (
	"errors"
	"strings"
)

// ErrInvalidPhone : le numéro n'est pas exploitable.
var ErrInvalidPhone = errors.New("numéro de téléphone invalide")

// FormatPhone met un numéro E.164 en forme lisible pour l'affichage : un numéro
// français « +33612345678 » devient « 06 12 34 56 78 ». Tout autre texte est rendu tel quel.
func FormatPhone(e164 string) string {
	rest, ok := strings.CutPrefix(e164, "+33")
	if !ok || len(rest) != 9 {
		return e164
	}
	for _, r := range rest {
		if r < '0' || r > '9' {
			return e164
		}
	}
	return "0" + rest[:1] + " " + rest[1:3] + " " + rest[3:5] + " " + rest[5:7] + " " + rest[7:9]
}

// NormalizePhone convertit un numéro saisi à la main en E.164 ("+33612345678").
// Sans indicatif, le numéro est français (« 06 12 34 56 78 » → « +33612345678 »).
// Espaces, points, tirets et parenthèses sont ignorés ; « 0033… » vaut « + » ;
// « +33 (0)6… » est accepté. Un numéro étranger doit commencer par « + » et
// compter 8 à 15 chiffres. Pas de dépendance : ce n'est pas une validation de
// plan de numérotation, seulement une forme canonique stable.
func NormalizePhone(s string) (string, error) {
	var digits strings.Builder
	plus := false
	for i, r := range strings.TrimSpace(s) {
		switch {
		case r >= '0' && r <= '9':
			digits.WriteRune(r)
		case r == '+' && i == 0:
			plus = true
		case r == ' ' || r == '.' || r == '-' || r == '(' || r == ')' || r == ' ':
		default:
			return "", ErrInvalidPhone
		}
	}
	d := digits.String()
	if !plus && strings.HasPrefix(d, "00") {
		plus, d = true, d[2:]
	}
	if !plus {
		if len(d) != 10 || d[0] != '0' || d[1] == '0' {
			return "", ErrInvalidPhone
		}
		return "+33" + d[1:], nil
	}
	if strings.HasPrefix(d, "330") { // +33 (0)6… : zéro de trunk superflu
		d = "33" + d[3:]
	}
	if len(d) < 8 || len(d) > 15 || d[0] == '0' {
		return "", ErrInvalidPhone
	}
	if strings.HasPrefix(d, "33") && len(d) != 11 {
		return "", ErrInvalidPhone
	}
	return "+" + d, nil
}
