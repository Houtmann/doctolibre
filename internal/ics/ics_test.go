package ics_test

import (
	"strings"
	"testing"
	"time"

	"doctolibre/internal/ics"
)

func TestCalendar(t *testing.T) {
	t.Parallel()
	paris, _ := time.LoadLocation("Europe/Paris")
	e := ics.Event{
		UID:      "abc@doctolibre",
		Start:    time.Date(2026, 10, 20, 14, 30, 0, 0, paris), // CEST : 12:30 UTC
		End:      time.Date(2026, 10, 20, 15, 0, 0, 0, paris),
		Stamp:    time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC),
		Summary:  "Séance, kiné; cabinet",
		Location: "1 rue de la Paix, Paris\n2e étage",
	}
	got := string(e.Calendar())
	for _, want := range []string{
		"BEGIN:VCALENDAR\r\n", "VERSION:2.0\r\n", "BEGIN:VEVENT\r\n",
		"UID:abc@doctolibre\r\n", "DTSTAMP:20261015T090000Z\r\n",
		"DTSTART:20261020T123000Z\r\n", "DTEND:20261020T130000Z\r\n",
		`SUMMARY:Séance\, kiné\; cabinet` + "\r\n",
		`LOCATION:1 rue de la Paix\, Paris\n2e étage` + "\r\n",
		"END:VEVENT\r\n", "END:VCALENDAR\r\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("fragment manquant %q dans\n%s", want, got)
		}
	}
	if strings.Contains(got, "DESCRIPTION") {
		t.Error("DESCRIPTION vide ne doit pas être émise")
	}
	if strings.Contains(strings.ReplaceAll(got, "\r\n", ""), "\n") {
		t.Error("LF isolé trouvé")
	}
}

func TestFoldKeepsRunesAndLimit(t *testing.T) {
	t.Parallel()
	e := ics.Event{
		UID: "x", Start: time.Unix(0, 0), End: time.Unix(60, 0), Stamp: time.Unix(0, 0),
		Summary: strings.Repeat("é", 100),
	}
	lines := strings.Split(strings.TrimSuffix(string(e.Calendar()), "\r\n"), "\r\n")
	var unfolded strings.Builder
	for _, l := range lines {
		if len(l) > 75 {
			t.Errorf("ligne de %d octets : %q", len(l), l)
		}
		if strings.HasPrefix(l, " ") {
			unfolded.WriteString(l[1:])
		} else {
			unfolded.WriteString("\n" + l)
		}
	}
	if !strings.Contains(unfolded.String(), "SUMMARY:"+strings.Repeat("é", 100)) {
		t.Error("le dépliage ne redonne pas le résumé d'origine")
	}
}
