package sms

import (
	"strings"
	"unicode/utf16"
)

const gsmBasic = "@£$¥èéùìòÇ\nØø\rÅåΔ_ΦΓΛΩΠΨΣΘΞÆæßÉ !\"#¤%&'()*+,-./0123456789:;<=>?¡ABCDEFGHIJKLMNOPQRSTUVWXYZÄÖÑÜ§¿abcdefghijklmnopqrstuvwxyzäöñüà"

// gsmExt : caractères de la table d'extension (2 septets).
const gsmExt = "^{}\\[~]|€\f"

var (
	gsmBasicSet = runeSet(gsmBasic)
	gsmExtSet   = runeSet(gsmExt)
)

func runeSet(s string) map[rune]bool {
	m := make(map[rune]bool, len(s))
	for _, r := range s {
		m[r] = true
	}
	return m
}

// Segments retourne le nombre de SMS facturés pour body et l'encodage utilisé.
// GSM-7 : 160 septets (153 par segment si concaténé) ; UCS-2 : 70 unités
// UTF-16 (67 par segment). Un seul caractère hors GSM-7 (emoji, ê, œ…) fait
// passer tout le message en UCS-2.
func Segments(body string) (segments int, ucs2 bool) {
	septets := 0
	for _, r := range body {
		switch {
		case gsmBasicSet[r]:
			septets++
		case gsmExtSet[r]:
			septets += 2
		default:
			ucs2 = true
		}
	}
	if ucs2 {
		return ceilDiv(len(utf16.Encode([]rune(body))), 70, 67), true
	}
	return ceilDiv(septets, 160, 153), false
}

func ceilDiv(n, single, multi int) int {
	switch {
	case n == 0:
		return 0
	case n <= single:
		return 1
	}
	return (n + multi - 1) / multi
}

var translit = strings.NewReplacer(
	"ê", "e", "â", "a", "î", "i", "ô", "o", "û", "u", "ë", "e", "ï", "i", "ÿ", "y",
	"À", "A", "Â", "A", "È", "E", "Ê", "E", "Ë", "E", "Î", "I", "Ï", "I", "Ô", "O", "Ù", "U", "Û", "U",
	"œ", "oe", "Œ", "OE", "ç", "c",
	"‘", "'", "’", "'", "“", "\"", "”", "\"", "«", "\"", "»", "\"",
	"–", "-", "—", "-", "…", "...", " ", " ", " ", " ",
)

// Transliterate remplace les caractères courants hors GSM-7 par un équivalent
// GSM-7 (ê->e, œ->oe, apostrophe typographique…) pour tenir sur 1 segment.
// Les autres caractères (emoji…) sont laissés tels quels.
func Transliterate(s string) string { return translit.Replace(s) }
