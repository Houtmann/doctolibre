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

func TestNavCurrent(t *testing.T) {
	t.Parallel()
	tests := []struct{ path, want string }{
		{"/admin/calendar", "/admin/calendar"},
		{"/admin/appointments/abc", "/admin/calendar"},
		{"/admin/patients/abc", "/admin/patients"},
		{"/admin/patients/abc/care-plans/new", "/admin/patients"},
		{"/admin/appointment-types", "/admin/appointment-types"},
		{"/admin/appointment-typesx", ""},
		{"/admin", ""},
		{"/p/cabinet", ""},
	}
	for _, tt := range tests {
		if got := navCurrent(tt.path); got != tt.want {
			t.Errorf("navCurrent(%q) = %q, attendu %q", tt.path, got, tt.want)
		}
	}
}
