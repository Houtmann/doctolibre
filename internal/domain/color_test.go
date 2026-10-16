package domain

import "testing"

func TestColorOrDefault(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ in, want string }{
		{"#2563eb", "#2563eb"}, {"#ABCDEF", "#ABCDEF"}, {"", DefaultColor}, {"red", DefaultColor},
		{"#fff", DefaultColor}, {"#12345g", DefaultColor}, {"#1234567", DefaultColor},
		{"url(x)", DefaultColor}, {"#123456;x", DefaultColor}, {"#123456\n", DefaultColor},
	} {
		if got := ColorOrDefault(tc.in); got != tc.want {
			t.Errorf("ColorOrDefault(%q) = %q, want %q", tc.in, got, tc.want)
		}
		if got := ValidColor(tc.in); got != (tc.want == tc.in) {
			t.Errorf("ValidColor(%q) = %v", tc.in, got)
		}
	}
}
