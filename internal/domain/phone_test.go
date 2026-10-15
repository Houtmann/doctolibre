package domain_test

import (
	"errors"
	"testing"

	"doctolibre/internal/domain"
)

func TestNormalizePhone(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in, want string
		bad      bool
	}{
		{in: "06 12 34 56 78", want: "+33612345678"},
		{in: "0612345678", want: "+33612345678"},
		{in: "06.12.34.56.78", want: "+33612345678"},
		{in: "06-12-34-56-78", want: "+33612345678"},
		{in: "+33 6 12 34 56 78", want: "+33612345678"},
		{in: "+33 (0)6 12 34 56 78", want: "+33612345678"},
		{in: "0033612345678", want: "+33612345678"},
		{in: "  01 23 45 67 89  ", want: "+33123456789"},
		{in: "+32 470 12 34 56", want: "+32470123456"},
		{in: "+44 7911 123456", want: "+447911123456"},
		{in: "", bad: true},
		{in: "abc", bad: true},
		{in: "06 12 34 56", bad: true},
		{in: "06 12 34 56 789", bad: true},
		{in: "00123", bad: true},
		{in: "33612345678", bad: true},
		{in: "+33 6 12 34 56", bad: true},
		{in: "+0612345678", bad: true},
		{in: "+1234567", bad: true},
		{in: "+1234567890123456", bad: true},
		{in: "06 12 34 56 78 +", bad: true},
		{in: "06;12345678", bad: true},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			t.Parallel()
			got, err := domain.NormalizePhone(tt.in)
			if tt.bad {
				if !errors.Is(err, domain.ErrInvalidPhone) {
					t.Fatalf("NormalizePhone(%q) = %q, %v ; erreur attendue", tt.in, got, err)
				}
				return
			}
			if err != nil || got != tt.want {
				t.Fatalf("NormalizePhone(%q) = %q, %v ; attendu %q", tt.in, got, err, tt.want)
			}
		})
	}
}
