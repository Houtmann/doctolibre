package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestLoadFromDefaults(t *testing.T) {
	t.Parallel()
	c, err := LoadFrom(env(nil))
	if err != nil {
		t.Fatal(err)
	}
	if c.HTTPAddr != ":8080" || c.Env != EnvDev || c.Timezone != "Europe/Paris" || c.BaseURL != "http://localhost:8080" {
		t.Fatalf("défauts inattendus: %+v", c)
	}
}

func TestLoadFromErrors(t *testing.T) {
	t.Parallel()
	good := strings.Repeat("s", 32)
	tests := []struct {
		name string
		env  map[string]string
		want string
	}{
		{"env invalide", map[string]string{"DOCTOLIBRE_ENV": "staging"}, "DOCTOLIBRE_ENV"},
		{"fuseau invalide", map[string]string{"DOCTOLIBRE_TIMEZONE": "Mars/Base"}, "DOCTOLIBRE_TIMEZONE"},
		{"url invalide", map[string]string{"DOCTOLIBRE_BASE_URL": "nope"}, "DOCTOLIBRE_BASE_URL"},
		{"secret trop court", map[string]string{"DOCTOLIBRE_SECRET_KEY": "court"}, "DOCTOLIBRE_SECRET_KEY"},
		{"secret manquant en prod", map[string]string{
			"DOCTOLIBRE_ENV": "prod", "DOCTOLIBRE_DATABASE_URL": "postgres://x", "DOCTOLIBRE_BASE_URL": "https://a.fr",
		}, "obligatoire en prod"},
		{"base de données manquante en prod", map[string]string{
			"DOCTOLIBRE_ENV": "prod", "DOCTOLIBRE_SECRET_KEY": good, "DOCTOLIBRE_BASE_URL": "https://a.fr",
		}, "DOCTOLIBRE_DATABASE_URL"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := LoadFrom(env(tt.env))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("erreur attendue contenant %q, obtenu %v", tt.want, err)
			}
		})
	}
}

func TestLoadFromProdOK(t *testing.T) {
	t.Parallel()
	c, err := LoadFrom(env(map[string]string{
		"DOCTOLIBRE_ENV": "prod", "DOCTOLIBRE_SECRET_KEY": strings.Repeat("s", 32),
		"DOCTOLIBRE_DATABASE_URL": "postgres://x", "DOCTOLIBRE_BASE_URL": "https://a.fr",
	}))
	if err != nil || !c.IsProd() {
		t.Fatalf("err=%v c=%+v", err, c)
	}
}

func TestParseEnvFile(t *testing.T) {
	t.Parallel()
	p := filepath.Join(t.TempDir(), ".env")
	content := "# commentaire\n\nA=1\nexport B=\"deux mots\"\nC='x=y'\nignoré\n"
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	m, err := parseEnvFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if m["A"] != "1" || m["B"] != "deux mots" || m["C"] != "x=y" || len(m) != 3 {
		t.Fatalf("map inattendue: %v", m)
	}
}
