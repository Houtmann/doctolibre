package auth

import (
	"strings"
	"testing"
)

func TestHashVerify(t *testing.T) {
	t.Parallel()
	h, err := Hash("correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(h, "$argon2id$v=19$m=65536,t=1,p=2$") {
		t.Fatalf("format PHC inattendu: %q", h)
	}
	h2, _ := Hash("correct horse battery")
	if h == h2 {
		t.Error("deux hashes identiques : sel non aléatoire")
	}
	tests := []struct {
		name, pw string
		want     bool
	}{
		{"bon", "correct horse battery", true},
		{"mauvais", "correct horse batterY", false},
		{"vide", "", false},
	}
	for _, tt := range tests {
		got, err := Verify(tt.pw, h)
		if err != nil || got != tt.want {
			t.Errorf("%s: got %v err %v, want %v", tt.name, got, err, tt.want)
		}
	}
}

func TestVerifyMalformed(t *testing.T) {
	t.Parallel()
	for _, h := range []string{
		"", "plaintext", "$argon2i$v=19$m=65536,t=1,p=2$c2VsLXNlbA$a2V5",
		"$argon2id$v=18$m=65536,t=1,p=2$c2VsLXNlbA$a2V5",
		"$argon2id$v=19$m=0,t=1,p=2$c2VsLXNlbA$a2V5",
		"$argon2id$v=19$m=99999999,t=1,p=2$c2VsLXNlbA$a2V5",
		"$argon2id$v=19$m=65536,t=1$c2VsLXNlbA$a2V5",
		"$argon2id$v=19$m=65536,t=1,p=2$!!$a2V5",
		"$argon2id$v=19$m=65536,t=1,p=2$c2VsLXNlbA$",
	} {
		if ok, err := Verify("x", h); ok || err == nil {
			t.Errorf("Verify(%q) = %v, %v ; erreur attendue", h, ok, err)
		}
	}
}

func TestSafeNext(t *testing.T) {
	t.Parallel()
	tests := []struct{ in, want string }{
		{"", "/admin"},
		{"/admin/agenda?day=2026-10-15", "/admin/agenda?day=2026-10-15"},
		{"/admin", "/admin"},
		{"//evil.example", "/admin"},
		{"https://evil.example/x", "/admin"},
		{"http://evil.example", "/admin"},
		{"/\\evil.example", "/admin"},
		{"\\\\evil.example", "/admin"},
		{"admin", "/admin"},
		{"javascript:alert(1)", "/admin"},
		{"/ok\r\nSet-Cookie: x=1", "/admin"},
		{"/%0d%0a", "/%0d%0a"}, // encodé : inoffensif, http.Redirect l'émet tel quel
		{"/a b", "/a b"},
	}
	for _, tt := range tests {
		if got := SafeNext(tt.in); got != tt.want {
			t.Errorf("SafeNext(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestSlugify(t *testing.T) {
	t.Parallel()
	tests := []struct{ in, want string }{
		{"Cabinet test", "cabinet-test"},
		{"  Cabinet Kiné d'Étoile !! ", "cabinet-kine-d-etoile"},
		{"???", "cabinet"},
	}
	for _, tt := range tests {
		if got := Slugify(tt.in); got != tt.want {
			t.Errorf("Slugify(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
