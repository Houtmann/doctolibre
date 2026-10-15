package ics_test

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"doctolibre/internal/ics"
)

var update = flag.Bool("update", false, "régénère les golden files")

func TestFeedGolden(t *testing.T) {
	t.Parallel()
	stamp := time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC)
	paris, _ := time.LoadLocation("Europe/Paris")
	events := []ics.Event{
		{ // avant le passage à l'heure d'hiver (CEST = UTC+2)
			UID: "11111111-1111-1111-1111-111111111111@doctolibre", Stamp: stamp, Sequence: 1767225600, Status: "CONFIRMED",
			Start: time.Date(2026, 10, 24, 9, 0, 0, 0, paris), End: time.Date(2026, 10, 24, 9, 30, 0, 0, paris),
			Summary: "RDV L.D. - Séance, kiné",
		},
		{ // après (CET = UTC+1), annulé
			UID: "22222222-2222-2222-2222-222222222222@doctolibre", Stamp: stamp, Sequence: 1767229200, Status: "CANCELLED",
			Start: time.Date(2026, 10, 26, 9, 0, 0, 0, paris), End: time.Date(2026, 10, 26, 9, 30, 0, 0, paris),
			Summary: "RDV A.M. - Rééducation; lombaire",
		},
	}
	got := ics.Feed("Agenda Doctolibre", events)
	path := filepath.Join("testdata", "feed.ics")
	if *update {
		if err := os.MkdirAll("testdata", 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, got, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path) //nolint:gosec // chemin constant de test
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("flux différent du golden file (-update pour régénérer)\n--- obtenu ---\n%s", got)
	}
	s := string(got)
	if !strings.Contains(s, "STATUS:CANCELLED\r\n") || !strings.Contains(s, "DTSTART:20261024T070000Z\r\n") || !strings.Contains(s, "DTSTART:20261026T080000Z\r\n") {
		t.Error("statut ou conversion UTC/DST inattendus")
	}
}

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
