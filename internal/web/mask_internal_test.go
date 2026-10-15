package web

import "testing"

func TestMaskRecipient(t *testing.T) {
	t.Parallel()
	tests := []struct{ in, want string }{
		{"+33612345678", "+33•••••••78"},
		{"0612345678", "06••••••78"},
		{"jean@exemple.fr", "j•••@exemple.fr"},
		{"é@exemple.fr", "•••@exemple.fr"},
		{"élodie@exemple.fr", "é•••@exemple.fr"},
		{"12345", "•••"},
		{"+3361", "•••"},
		{"", "•••"},
		{"@x.fr", "•••@x.fr"},
	}
	for _, tt := range tests {
		if got := maskRecipient(tt.in); got != tt.want {
			t.Errorf("maskRecipient(%q) = %q, attendu %q", tt.in, got, tt.want)
		}
	}
}
