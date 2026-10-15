package sms

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestSegments(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		body string
		want int
		ucs2 bool
	}{
		{"vide", "", 0, false},
		{"160 gsm7", strings.Repeat("a", 160), 1, false},
		{"161 gsm7", strings.Repeat("a", 161), 2, false},
		{"306 gsm7", strings.Repeat("a", 306), 2, false},
		{"307 gsm7", strings.Repeat("a", 307), 3, false},
		{"accents gsm7", strings.Repeat("é", 160), 1, false},
		{"extension compte double", strings.Repeat("€", 80), 1, false},
		{"extension 81", strings.Repeat("€", 81), 2, false},
		{"emoji force ucs2", "Bonjour 😀", 1, true},
		{"ucs2 70", strings.Repeat("ê", 70), 1, true},
		{"ucs2 71", strings.Repeat("ê", 71), 2, true},
		{"emoji = 2 unités", strings.Repeat("😀", 36), 2, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, ucs2 := Segments(tc.body)
			if got != tc.want || ucs2 != tc.ucs2 {
				t.Errorf("Segments = (%d, %v), attendu (%d, %v)", got, ucs2, tc.want, tc.ucs2)
			}
		})
	}
}

func TestTransliterateKeepsGSM7(t *testing.T) {
	t.Parallel()
	in := "Rendez-vous à l’œil, ça va être prêt, où ? Très bien… « ok »"
	if _, ucs2 := Segments(in); !ucs2 {
		t.Fatal("le texte de test devrait être UCS-2")
	}
	out := Transliterate(in)
	if n, ucs2 := Segments(out); ucs2 || n != 1 {
		t.Errorf("après translittération : %q (%d, ucs2=%v)", out, n, ucs2)
	}
	if got := Transliterate("œuvre"); got != "oeuvre" {
		t.Errorf("œuvre -> %q", got)
	}
	if got := Transliterate("😀"); got != "😀" {
		t.Errorf("l'emoji doit rester intact : %q", got)
	}
}

func TestIsPermanent(t *testing.T) {
	t.Parallel()
	tests := []struct {
		err  error
		want bool
	}{
		{StatusError{400}, true},
		{StatusError{401}, true},
		{StatusError{404}, true},
		{StatusError{408}, false},
		{StatusError{429}, false},
		{StatusError{500}, false},
		{StatusError{503}, false},
		{fmt.Errorf("wrap: %w", StatusError{422}), true},
		{PermanentError{errors.New("x")}, true},
		{errors.New("connexion refusée"), false},
	}
	for _, tc := range tests {
		if got := IsPermanent(tc.err); got != tc.want {
			t.Errorf("IsPermanent(%v) = %v", tc.err, got)
		}
	}
}

func TestMaskPhone(t *testing.T) {
	t.Parallel()
	if got := MaskPhone("+33612345678"); got != "+33*******78" {
		t.Errorf("MaskPhone = %q", got)
	}
	if got := MaskPhone("123"); got != "***" {
		t.Errorf("MaskPhone court = %q", got)
	}
}
