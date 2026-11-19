package domain_test

import (
	"testing"
	"time"

	"doctolibre/internal/domain"
)

func TestFormatPhone(t *testing.T) {
	t.Parallel()
	tests := []struct{ in, want string }{
		{"+33612345678", "06 12 34 56 78"},
		{"+33123456789", "01 23 45 67 89"},
		{"+32470123456", "+32470123456"},
		{"", ""},
		{"06 12 34 56 78", "06 12 34 56 78"},
		{"+3361234", "+3361234"},
		{"+33abcdefghi", "+33abcdefghi"},
	}
	for _, tt := range tests {
		if got := domain.FormatPhone(tt.in); got != tt.want {
			t.Errorf("FormatPhone(%q) = %q, attendu %q", tt.in, got, tt.want)
		}
	}
}

func TestChangeAllowed(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC)
	notice := 24 * time.Hour
	tests := []struct {
		name  string
		start time.Time
		want  bool
	}{
		{"loin", now.Add(48 * time.Hour), true},
		{"pile au délai", now.Add(notice), true},
		{"juste en dessous", now.Add(notice - time.Minute), false},
		{"passé", now.Add(-time.Hour), false},
	}
	for _, tt := range tests {
		if got := domain.ChangeAllowed(tt.start, now, notice); got != tt.want {
			t.Errorf("%s : %v, attendu %v", tt.name, got, tt.want)
		}
	}
}
