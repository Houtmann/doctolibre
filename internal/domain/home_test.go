package domain_test

import (
	"testing"
	"time"

	"doctolibre/internal/domain"
)

func TestHomeBuffers(t *testing.T) {
	t.Parallel()
	m := time.Minute
	b, a := domain.HomeBuffers(true, 5*m, 0, 15*m)
	if b != 20*m || a != 15*m {
		t.Fatalf("domicile: got %v/%v", b, a)
	}
	b, a = domain.HomeBuffers(false, 5*m, 0, 15*m)
	if b != 5*m || a != 0 {
		t.Fatalf("cabinet: got %v/%v", b, a)
	}
}

func TestInHomeZone(t *testing.T) {
	t.Parallel()
	zones := []string{"75011", "Saint-Ouen", " lyon "}
	tests := []struct {
		name, addr string
		want       bool
	}{
		{"code postal", "12 rue de la Paix, 75011 Paris", true},
		{"code postal hors zone", "12 rue de la Paix, 75012 Paris", false},
		{"nombre plus long", "175011 impasse", false},
		{"commune accents et casse", "3 rue X 93400 SAINT OUEN", true},
		{"commune avec tiret", "3 rue X, saint-ouen", true},
		{"commune mot entier", "3 rue X, Lyonnais-sur-Mer", false},
		{"commune", "5 quai Y Lyon", true},
		{"vide", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := domain.InHomeZone(tt.addr, zones); got != tt.want {
				t.Fatalf("InHomeZone(%q) = %v", tt.addr, got)
			}
		})
	}
	if domain.InHomeZone("75011 Paris", nil) {
		t.Fatal("aucune zone doit tout refuser")
	}
}
