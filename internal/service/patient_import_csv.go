package service

import (
	"bytes"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"doctolibre/internal/domain"
)

// Limites de l'import CSV des patients.
const (
	MaxImportBytes = 2 << 20
	MaxImportRows  = 5000
)

// ImportRow est une ligne du fichier, lue mais pas encore validée : les valeurs
// sont nettoyées (espaces) mais le téléphone et l'email sont bruts. Birth est
// au format AAAA-MM-JJ ou vide ; BirthBad signale une date illisible.
type ImportRow struct {
	Line                int // numéro de ligne dans le fichier (l'en-tête est la ligne 1)
	LastName, FirstName string
	Phone, Email        string
	Birth               string
	BirthBad            bool
	Address             string
}

// ParseImportCSV lit un fichier CSV de patients (fonction pure). Les lignes
// entièrement vides sont ignorées. Erreurs : domain.ErrValidation{Field: "file"}.
func ParseImportCSV(data []byte) ([]ImportRow, error) {
	if len(data) > MaxImportBytes {
		return nil, importErr("Le fichier dépasse 2 Mo.")
	}
	text := decodeImportText(data)
	if strings.TrimSpace(text) == "" {
		return nil, importErr("Le fichier est vide.")
	}
	r := csv.NewReader(strings.NewReader(text))
	r.Comma = detectDelimiter(text)
	r.FieldsPerRecord = -1
	r.LazyQuotes = true
	r.ReuseRecord = false

	header, err := r.Read()
	if err != nil {
		return nil, importErr("L'en-tête du fichier est illisible.")
	}
	cols := mapImportColumns(header)
	if len(cols.last) == 0 || len(cols.first) == 0 {
		return nil, importErr("L'en-tête doit contenir au minimum les colonnes « Nom » et « Prénom ».")
	}

	var rows []ImportRow
	for {
		rec, err := r.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		line, _ := r.FieldPos(0)
		if err != nil {
			return nil, importErr(fmt.Sprintf("Ligne %d illisible (guillemets mal fermés ?).", line))
		}
		if blankRecord(rec) {
			continue
		}
		if len(rows) >= MaxImportRows {
			return nil, importErr(fmt.Sprintf("Le fichier dépasse %d lignes.", MaxImportRows))
		}
		rows = append(rows, cols.row(line, rec))
	}
	if len(rows) == 0 {
		return nil, importErr("Le fichier ne contient aucune ligne de patient.")
	}
	return rows, nil
}

func importErr(msg string) error { return domain.ErrValidation{Field: "file", Msg: msg} }

func blankRecord(rec []string) bool {
	for _, f := range rec {
		if strings.TrimSpace(f) != "" {
			return false
		}
	}
	return true
}

// decodeImportText retire le BOM UTF-8 et décode en UTF-8 : un contenu UTF-8
// valide est gardé tel quel, sinon il est lu comme Windows-1252.
func decodeImportText(data []byte) string {
	data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})
	if utf8.Valid(data) {
		return string(data)
	}
	var b strings.Builder
	b.Grow(len(data) + len(data)/8)
	for _, c := range data {
		b.WriteRune(cp1252Rune(c))
	}
	return b.String()
}

// cp1252High : Windows-1252 de 0x80 à 0x9F (le reste est identique à Latin-1).
// Les cinq octets non définis (0x81, 0x8D, 0x8F, 0x90, 0x9D) restent U+0081… .
var cp1252High = [32]rune{
	0x20AC, 0x0081, 0x201A, 0x0192, 0x201E, 0x2026, 0x2020, 0x2021,
	0x02C6, 0x2030, 0x0160, 0x2039, 0x0152, 0x008D, 0x017D, 0x008F,
	0x0090, 0x2018, 0x2019, 0x201C, 0x201D, 0x2022, 0x2013, 0x2014,
	0x02DC, 0x2122, 0x0161, 0x203A, 0x0153, 0x009D, 0x017E, 0x0178,
}

func cp1252Rune(c byte) rune {
	if c >= 0x80 && c <= 0x9F {
		return cp1252High[c-0x80]
	}
	return rune(c)
}

// detectDelimiter choisit « ; » ou « , » d'après la ligne d'en-tête (hors guillemets).
func detectDelimiter(text string) rune {
	line := text
	if i := strings.IndexAny(text, "\r\n"); i >= 0 {
		line = text[:i]
	}
	semi, comma, quoted := 0, 0, false
	for _, c := range line {
		switch {
		case c == '"':
			quoted = !quoted
		case quoted:
		case c == ';':
			semi++
		case c == ',':
			comma++
		}
	}
	if semi >= comma && semi > 0 {
		return ';'
	}
	return ','
}

var headerAccents = strings.NewReplacer(
	"à", "a", "â", "a", "ä", "a", "á", "a", "ç", "c",
	"é", "e", "è", "e", "ê", "e", "ë", "e", "î", "i", "ï", "i", "í", "i",
	"ô", "o", "ö", "o", "ó", "o", "ù", "u", "û", "u", "ü", "u", "ú", "u",
	"ÿ", "y", "œ", "oe", "æ", "ae", "ñ", "n",
)

// normHeader : minuscules, sans accents, ponctuation et séparateurs réduits à un espace.
func normHeader(s string) string {
	s = headerAccents.Replace(strings.ToLower(strings.TrimSpace(s)))
	var b strings.Builder
	space := false
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			space = false
		} else if !space && b.Len() > 0 {
			b.WriteByte(' ')
			space = true
		}
	}
	return strings.TrimSpace(b.String())
}

var (
	lastNameHeaders  = setOf("nom", "nom de famille", "nom usuel", "nom patient", "nom du patient", "last name", "lastname", "family name", "surname")
	firstNameHeaders = setOf("prenom", "prenom usuel", "prenom patient", "prenom du patient", "first name", "firstname", "given name")
	phoneHeaders     = []string{"portable", "mobile", "telephone portable", "telephone mobile", "tel portable", "tel mobile", "gsm", "telephone", "tel", "numero de telephone", "phone", "mobile phone"}
	emailHeaders     = setOf("email", "e mail", "mail", "courriel", "adresse email", "adresse e mail", "adresse mail", "adresse electronique")
	birthHeaders     = setOf("date de naissance", "date naissance", "naissance", "ne le", "nee le", "ne e le", "date of birth", "birth date", "birthdate", "dob")
	addressHeaders   = setOf("adresse", "adresse postale", "adresse du patient", "address")
)

func setOf(vals ...string) map[string]bool {
	m := make(map[string]bool, len(vals))
	for _, v := range vals {
		m[v] = true
	}
	return m
}

// importColumns : indices des colonnes reconnues (plusieurs possibles pour le
// nom et le prénom afin que « les deux manquent » soit détectable, un seul est
// utilisé ; téléphones classés par priorité : portable avant fixe).
type importColumns struct {
	last, first, email, birth, address []int
	phones                             []int
}

func mapImportColumns(header []string) importColumns {
	var c importColumns
	type ph struct{ idx, rank int }
	var phones []ph
	for i, h := range header {
		n := normHeader(h)
		switch {
		case lastNameHeaders[n]:
			c.last = append(c.last, i)
		case firstNameHeaders[n]:
			c.first = append(c.first, i)
		case emailHeaders[n]:
			c.email = append(c.email, i)
		case birthHeaders[n]:
			c.birth = append(c.birth, i)
		case addressHeaders[n]:
			c.address = append(c.address, i)
		default:
			for rank, name := range phoneHeaders {
				if n == name {
					phones = append(phones, ph{i, rank})
					break
				}
			}
		}
	}
	for rank := range phoneHeaders {
		for _, p := range phones {
			if p.rank == rank {
				c.phones = append(c.phones, p.idx)
			}
		}
	}
	return c
}

func (c importColumns) row(line int, rec []string) ImportRow {
	get := func(idx []int) string {
		for _, i := range idx {
			if i < len(rec) {
				if v := cleanCell(rec[i]); v != "" {
					return v
				}
			}
		}
		return ""
	}
	r := ImportRow{
		Line: line, LastName: get(c.last), FirstName: get(c.first), Email: get(c.email),
		Address: strings.Join(strings.Fields(get(c.address)), " "),
	}
	// Premier téléphone lisible ; à défaut, le premier renseigné (rejeté ensuite).
	for _, i := range c.phones {
		if i >= len(rec) {
			continue
		}
		v := cleanCell(rec[i])
		if v == "" {
			continue
		}
		if r.Phone == "" {
			r.Phone = v
		}
		if _, err := domain.NormalizePhone(v); err == nil {
			r.Phone = v
			break
		}
	}
	if b := get(c.birth); b != "" {
		if t, ok := parseImportDate(b); ok {
			r.Birth = t.Format("2006-01-02")
		} else {
			r.BirthBad = true
		}
	}
	return r
}

func cleanCell(s string) string { return strings.TrimSpace(strings.Trim(s, "\u00a0\u200b")) }

// parseImportDate lit JJ/MM/AAAA (jour et mois sur 1 ou 2 chiffres) ou AAAA-MM-JJ.
func parseImportDate(s string) (time.Time, bool) {
	for _, layout := range []string{"2/1/2006", "2006-01-02"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}
