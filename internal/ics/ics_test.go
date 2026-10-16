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

func TestCalendarGolden(t *testing.T) {
	t.Parallel()
	paris, _ := time.LoadLocation("Europe/Paris")
	e := ics.Event{
		UID:         "00000000-0000-0000-0000-000000000001@doctolibre",
		Start:       time.Date(2026, 10, 20, 9, 0, 0, 0, paris),
		End:         time.Date(2026, 10, 20, 9, 30, 0, 0, paris),
		Stamp:       time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC),
		Summary:     "Séance de kinésithérapie - Cabinet des Lilas, Paris",
		Location:    "12 rue des Lilas, 75020 Paris",
		Description: "Cabinet : 01 23 45 67 89",
		Alarms:      []time.Duration{24 * time.Hour, 2 * time.Hour},
	}
	got := e.Calendar()
	const path = "testdata/appointment.ics"
	if *update {
		if err := os.WriteFile(path, got, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Errorf("ICS différent du golden file (-update pour régénérer)\n--- got\n%s\n--- want\n%s", got, want)
	}
}

func TestAlarms(t *testing.T) {
	t.Parallel()
	e := ics.Event{
		UID: "x", Start: time.Unix(7200, 0), End: time.Unix(9000, 0), Stamp: time.Unix(0, 0),
		Summary: "Séance, kiné; " + strings.Repeat("é", 60) + `\`,
		Alarms:  []time.Duration{24 * time.Hour, 2 * time.Hour, 30 * time.Minute, 0, -time.Hour, 26*time.Hour + 30*time.Minute},
	}
	got := string(e.Calendar())
	if n := strings.Count(got, "BEGIN:VALARM\r\n"); n != 4 || strings.Count(got, "END:VALARM\r\n") != 4 {
		t.Fatalf("alarmes = %d, attendu 4 (0 et négatif ignorés)", n)
	}
	for _, want := range []string{"TRIGGER:-P1D\r\n", "TRIGGER:-PT2H\r\n", "TRIGGER:-PT30M\r\n", "TRIGGER:-P1DT2H30M\r\n", "ACTION:DISPLAY\r\n"} {
		if !strings.Contains(got, want) {
			t.Errorf("fragment manquant %q", want)
		}
	}
	// L'alarme est dans le VEVENT, et la DESCRIPTION est échappée puis pliée.
	if i, j, k := strings.Index(got, "BEGIN:VALARM"), strings.Index(got, "END:VALARM"), strings.Index(got, "END:VEVENT"); i >= j || j >= k {
		t.Error("VALARM hors du VEVENT")
	}
	unfolded := strings.ReplaceAll(got, "\r\n ", "")
	if !strings.Contains(unfolded, `DESCRIPTION:Séance\, kiné\; `+strings.Repeat("é", 60)+`\\`+"\r\n") {
		t.Errorf("DESCRIPTION d'alarme mal échappée :\n%s", unfolded)
	}
	for _, l := range strings.Split(strings.TrimSuffix(got, "\r\n"), "\r\n") {
		if len(l) > 75 {
			t.Errorf("ligne de %d octets : %q", len(l), l)
		}
	}
	if strings.Contains(string(ics.Event{UID: "y", Start: time.Unix(0, 0), End: time.Unix(1, 0), Stamp: time.Unix(0, 0)}.Calendar()), "VALARM") {
		t.Error("VALARM émise sans Alarms")
	}
}
