package domain

import "strings"

var accentFold = strings.NewReplacer(
	"à", "a", "â", "a", "ä", "a", "é", "e", "è", "e", "ê", "e", "ë", "e",
	"î", "i", "ï", "i", "ô", "o", "ö", "o", "ù", "u", "û", "u", "ü", "u",
	"ç", "c", "œ", "oe", "æ", "ae", "ÿ", "y", "ñ", "n",
)

// Slugify transforme un nom en slug [a-z0-9-] (« Cabinet Kiné Étoile » → « cabinet-kine-etoile »).
// Un nom sans caractère exploitable donne « cabinet ».
func Slugify(name string) string {
	s := accentFold.Replace(strings.ToLower(name))
	var b strings.Builder
	dash := true // évite un tiret en tête
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			dash = false
		case !dash:
			b.WriteByte('-')
			dash = true
		}
	}
	out := strings.TrimRight(b.String(), "-")
	if out == "" {
		return "cabinet"
	}
	return out
}
