// Package ics produit des fichiers iCalendar (RFC 5545) minimaux, heures en UTC :
// un événement seul (lien patient) ou un flux d'événements (abonnement praticien).
package ics

import (
	"strconv"
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
	// Status : "" (non émis), "CONFIRMED" ou "CANCELLED".
	Status string
	// Sequence : numéro de révision (SEQUENCE) ; 0 = non émis.
	Sequence int
	// Alarms : rappels (VALARM) déclenchés avant le début, par ex. 24*time.Hour.
	// Les durées nulles ou négatives sont ignorées ; vide = aucune alarme.
	Alarms []time.Duration
}

const (
	crlf       = "\r\n"
	foldLimit  = 75
	timeLayout = "20060102T150405Z"
)

// Calendar rend le fichier .ics complet d'un seul événement (fins de ligne CRLF,
// lignes repliées à 75 octets).
func (e Event) Calendar() []byte { return Feed("", []Event{e}) }

// Feed rend un calendrier de plusieurs événements. name, s'il est non vide,
// devient le nom d'abonnement affiché par les clients (X-WR-CALNAME).
func Feed(name string, events []Event) []byte {
	var b strings.Builder
	line := func(s string) { b.WriteString(fold(s)) }
	line("BEGIN:VCALENDAR")
	line("VERSION:2.0")
	line("PRODID:-//Doctolibre//FR")
	line("CALSCALE:GREGORIAN")
	line("METHOD:PUBLISH")
	if name != "" {
		line("X-WR-CALNAME:" + escape(name))
	}
	for _, e := range events {
		line("BEGIN:VEVENT")
		line("UID:" + escape(e.UID))
		line("DTSTAMP:" + e.Stamp.UTC().Format(timeLayout))
		line("DTSTART:" + e.Start.UTC().Format(timeLayout))
		line("DTEND:" + e.End.UTC().Format(timeLayout))
		if e.Sequence > 0 {
			line("SEQUENCE:" + strconv.Itoa(e.Sequence))
		}
		if e.Status != "" {
			line("STATUS:" + e.Status)
		}
		line("SUMMARY:" + escape(e.Summary))
		if e.Location != "" {
			line("LOCATION:" + escape(e.Location))
		}
		if e.Description != "" {
			line("DESCRIPTION:" + escape(e.Description))
		}
		for _, a := range e.Alarms {
			if a <= 0 {
				continue
			}
			desc := e.Summary
			if desc == "" {
				desc = "Rendez-vous"
			}
			line("BEGIN:VALARM")
			line("ACTION:DISPLAY")
			line("DESCRIPTION:" + escape(desc))
			line("TRIGGER:" + triggerBefore(a))
			line("END:VALARM")
		}
		line("END:VEVENT")
	}
	line("END:VCALENDAR")
	return []byte(b.String())
}

// triggerBefore formate une durée RFC 5545 négative (déclenchement avant le
// début) : -P1D, -PT2H, -PT30M, -P1DT2H30M. Précision : la seconde.
func triggerBefore(d time.Duration) string {
	secs := int64(d / time.Second)
	days, secs := secs/86400, secs%86400
	h, secs := secs/3600, secs%3600
	m, sec := secs/60, secs%60
	out := "-P"
	if days > 0 {
		out += strconv.FormatInt(days, 10) + "D"
	}
	if h > 0 || m > 0 || sec > 0 || days == 0 {
		out += "T"
		if h > 0 {
			out += strconv.FormatInt(h, 10) + "H"
		}
		if m > 0 {
			out += strconv.FormatInt(m, 10) + "M"
		}
		if sec > 0 || (h == 0 && m == 0) {
			out += strconv.FormatInt(sec, 10) + "S"
		}
	}
	return out
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
