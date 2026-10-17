package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// env lit m ; DOCTOLIBRE_SECRET_KEY (obligatoire partout) vaut une clé valide sauf
// si m la définit explicitement (même vide).
func env(m map[string]string) func(string) string {
	return func(k string) string {
		if v, ok := m[k]; ok {
			return v
		}
		if k == "DOCTOLIBRE_SECRET_KEY" {
			return strings.Repeat("s", 32)
		}
		return ""
	}
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

func TestLoadFromWorker(t *testing.T) {
	t.Parallel()
	for val, want := range map[string]bool{"": true, "on": true, "off": false, "OFF": false} {
		c, err := LoadFrom(env(map[string]string{"DOCTOLIBRE_WORKER": val}))
		if err != nil || c.WorkerEnabled != want {
			t.Errorf("DOCTOLIBRE_WORKER=%q: %v, %v", val, c.WorkerEnabled, err)
		}
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
		{"worker invalide", map[string]string{"DOCTOLIBRE_WORKER": "maybe"}, "DOCTOLIBRE_WORKER"},
		{"url invalide", map[string]string{"DOCTOLIBRE_BASE_URL": "nope"}, "DOCTOLIBRE_BASE_URL"},
		{"secret trop court", map[string]string{"DOCTOLIBRE_SECRET_KEY": "court"}, "DOCTOLIBRE_SECRET_KEY"},
		{"secret manquant en dev", map[string]string{"DOCTOLIBRE_SECRET_KEY": ""}, "DOCTOLIBRE_SECRET_KEY: obligatoire"},
		{"secret manquant en prod", map[string]string{
			"DOCTOLIBRE_ENV": "prod", "DOCTOLIBRE_SECRET_KEY": "", "DOCTOLIBRE_DATABASE_URL": "postgres://x", "DOCTOLIBRE_BASE_URL": "https://a.fr",
		}, "DOCTOLIBRE_SECRET_KEY: obligatoire"},
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

func TestLoadFromNotifyAllowPrivateHosts(t *testing.T) {
	t.Parallel()
	for val, want := range map[string]bool{"": false, "false": false, "true": true, "TRUE": true, "1": true, "0": false} {
		c, err := LoadFrom(env(map[string]string{"DOCTOLIBRE_NOTIFY_ALLOW_PRIVATE_HOSTS": val}))
		if err != nil || c.NotifyAllowPrivateHosts != want {
			t.Errorf("DOCTOLIBRE_NOTIFY_ALLOW_PRIVATE_HOSTS=%q: %v, %v", val, c.NotifyAllowPrivateHosts, err)
		}
	}
}
