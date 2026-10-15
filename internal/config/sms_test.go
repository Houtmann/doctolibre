package config

import (
	"strings"
	"testing"
)

func TestSMSDefaults(t *testing.T) {
	t.Parallel()
	c, err := LoadFrom(env(nil))
	if err != nil {
		t.Fatal(err)
	}
	if c.SMS.Driver != "log" || !c.SMS.Transliterate {
		t.Errorf("défauts SMS = %+v", c.SMS)
	}
}

func TestSMSDrivers(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		env  map[string]string
		err  string // sous-chaîne attendue ; vide = OK
	}{
		{"http ok", map[string]string{
			"DOCTOLIBRE_SMS_DRIVER": "http", "DOCTOLIBRE_SMS_HTTP_URL": "https://x", "DOCTOLIBRE_SMS_HTTP_BODY": "{{.Body}}",
			"DOCTOLIBRE_SMS_HTTP_HEADERS": `{"Content-Type":"application/json"}`,
		}, ""},
		{"http sans url", map[string]string{"DOCTOLIBRE_SMS_DRIVER": "http"}, "DOCTOLIBRE_SMS_HTTP_URL"},
		{"http en-têtes invalides", map[string]string{"DOCTOLIBRE_SMS_DRIVER": "http", "DOCTOLIBRE_SMS_HTTP_HEADERS": "a: b"}, "DOCTOLIBRE_SMS_HTTP_HEADERS"},
		{"android sans identifiants", map[string]string{"DOCTOLIBRE_SMS_DRIVER": "android"}, "DOCTOLIBRE_SMS_ANDROID_USER"},
		{"ovh incomplet", map[string]string{"DOCTOLIBRE_SMS_DRIVER": "ovh"}, "DOCTOLIBRE_SMS_OVH_SERVICE"},
		{"inconnu", map[string]string{"DOCTOLIBRE_SMS_DRIVER": "pigeon"}, "DOCTOLIBRE_SMS_DRIVER"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := LoadFrom(env(tc.env))
			switch {
			case tc.err == "" && err != nil:
				t.Fatalf("erreur inattendue : %v", err)
			case tc.err != "" && (err == nil || !strings.Contains(err.Error(), tc.err)):
				t.Fatalf("erreur = %v, attendu %q", err, tc.err)
			}
		})
	}
}
