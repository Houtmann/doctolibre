// Package ics produit des fichiers iCalendar (RFC 5545) minimaux : un
// événement par fichier, heures en UTC. Le flux complet d'un praticien est
// l'affaire de T042.
package ics

import (
	"strings"
	"time"
	"unicode/utf8"
)

// Event est un événement unique. Aucune donnée personnelle de patient ne doit
// y figurer.
type Event struct {
	UID         string // identifiant stable, ex. "<appointment_id>@doctolibre"
	Start, End  time.Time
	Stamp       time.Time // DTSTAMP : date de création de l'événement
	Summary     string
	Location    string
	Description string
}

const (
	crlf       = "\r\n"
	foldLimit  = 75
	timeLayout = "20060102T150405Z"
)

// Calendar rend le fichier .ics complet (fins de ligne CRLF, lignes repliées à 75 octets).
func (e Event) Calendar() []byte {
	var b strings.Builder
	line := func(s string) { b.WriteString(fold(s)) }
	line("BEGIN:VCALENDAR")
	line("VERSION:2.0")
	line("PRODID:-//Doctolibre//FR")
	line("CALSCALE:GREGORIAN")
	line("METHOD:PUBLISH")
	line("BEGIN:VEVENT")
	line("UID:" + escape(e.UID))
	line("DTSTAMP:" + e.Stamp.UTC().Format(timeLayout))
	line("DTSTART:" + e.Start.UTC().Format(timeLayout))
	line("DTEND:" + e.End.UTC().Format(timeLayout))
	line("SUMMARY:" + escape(e.Summary))
	if e.Location != "" {
		line("LOCATION:" + escape(e.Location))
	}
	if e.Description != "" {
		line("DESCRIPTION:" + escape(e.Description))
	}
	line("END:VEVENT")
	line("END:VCALENDAR")
	return []byte(b.String())
}

// escape applique l'échappement TEXT de la RFC 5545 (§3.3.11).
func escape(s string) string {
	r := strings.NewReplacer(`\`, `\\`, ";", `\;`, ",", `\,`, "\r\n", `\n`, "\n", `\n`, "\r", `\n`)
	return r.Replace(s)
}

// fold replie une ligne à 75 octets (sans couper un caractère UTF-8) et la
// termine par CRLF ; les suites commencent par une espace.
func fold(s string) string {
	var b strings.Builder
	n := 0
	for _, r := range s {
		size := utf8.RuneLen(r)
		if n+size > foldLimit {
			b.WriteString(crlf + " ")
			n = 1
		}
		b.WriteRune(r)
		n += size
	}
	b.WriteString(crlf)
	return b.String()
}
