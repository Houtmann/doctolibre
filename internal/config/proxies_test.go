package config

import (
	"strings"
	"testing"
)

func TestTrustedProxiesAndAutoMigrate(t *testing.T) {
	t.Parallel()
	c, err := LoadFrom(env(map[string]string{"DOCTOLIBRE_TRUSTED_PROXIES": "172.16.0.0/12, 10.0.0.1 ::1"}))
	if err != nil {
		t.Fatal(err)
	}
	if len(c.TrustedProxies) != 3 || c.TrustedProxies[1].String() != "10.0.0.1/32" || c.TrustedProxies[2].String() != "::1/128" {
		t.Fatalf("proxies inattendus: %v", c.TrustedProxies)
	}
	if !c.AutoMigrate {
		t.Fatal("AutoMigrate doit valoir true par défaut")
	}
	c, err = LoadFrom(env(map[string]string{"DOCTOLIBRE_AUTO_MIGRATE": "false"}))
	if err != nil || c.AutoMigrate {
		t.Fatalf("err=%v c=%+v", err, c)
	}
	for _, bad := range []map[string]string{
		{"DOCTOLIBRE_TRUSTED_PROXIES": "pas-une-ip"},
		{"DOCTOLIBRE_TRUSTED_PROXIES": "10.0.0.0/99"},
		{"DOCTOLIBRE_AUTO_MIGRATE": "peut-être"},
	} {
		if _, err := LoadFrom(env(bad)); err == nil || !strings.Contains(err.Error(), "DOCTOLIBRE_") {
			t.Fatalf("%v: erreur attendue, obtenu %v", bad, err)
		}
	}
}
