package domain

import (
	"strings"
	"time"
	"unicode"
)

// HomeBuffers retourne les marges (avant, après) d'un RDV : celles du motif,
// augmentées du temps de trajet pour une visite à domicile. C'est LA règle,
// partagée par le moteur de créneaux et la création de RDV, donc par la
// contrainte EXCLUDE (during_blocked inclut le trajet).
func HomeBuffers(atHome bool, before, after, travel time.Duration) (time.Duration, time.Duration) {
	if !atHome {
		return before, after
	}
	return before + travel, after + travel
}

// foldWords met en minuscules, retire les accents et remplace toute ponctuation
// par des espaces, puis entoure d'espaces pour des recherches de mots entiers.
func foldWords(s string) string {
	s = accentFold.Replace(strings.ToLower(s))
	var b strings.Builder
	b.WriteByte(' ')
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		} else {
			b.WriteByte(' ')
		}
	}
	b.WriteByte(' ')
	return strings.Join(strings.Fields(b.String()), " ")
}

// InHomeZone indique si l'adresse appartient à l'une des zones. Une zone est un
// code postal (5 chiffres, comparé aux nombres de 5 chiffres de l'adresse) ou un
// nom de commune (mot ou suite de mots présents dans l'adresse, sans casse ni
// accents). Aucune zone = aucune adresse acceptée.
func InHomeZone(address string, zones []string) bool {
	addr := " " + foldWords(address) + " "
	for _, z := range zones {
		z = foldWords(z)
		if z == "" {
			continue
		}
		if strings.Contains(addr, " "+z+" ") {
			return true
		}
	}
	return false
}
