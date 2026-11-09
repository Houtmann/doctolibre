package web

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "régénère les golden files")

func TestQRSVGGolden(t *testing.T) {
	t.Parallel()
	got, err := qrSVG("https://rdv.example.test/p/cabinet-test")
	if err != nil {
		t.Fatal(err)
	}
	again, _ := qrSVG("https://rdv.example.test/p/cabinet-test")
	if got != again {
		t.Fatal("SVG non déterministe")
	}
	if strings.Contains(got, "rdv.example.test") {
		t.Error("l'URL ne doit pas figurer en texte dans le SVG")
	}
	path := filepath.Join("testdata", "poster_qr.svg")
	if *update {
		if err := os.MkdirAll("testdata", 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil { //nolint:gosec // golden
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path) //nolint:gosec // chemin constant
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Errorf("SVG différent du golden (lancer -update si voulu)")
	}
}
