// Package config charge la configuration depuis l'environnement.
package config

import (
	"bufio"
	"errors"
	"fmt"
	"net/netip"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// Environnements supportés.
const (
	EnvDev  = "dev"
	EnvProd = "prod"
)

const (
	minSecretLen       = 32
	minMetricsTokenLen = 16
)

// Config est la configuration de l'application.
type Config struct {
	HTTPAddr    string
	DatabaseURL string
	BaseURL     string
	SecretKey   string
	Env         string
	Timezone    string
	// MetricsToken active /metrics (jeton Bearer) ; vide = endpoint désactivé.
	MetricsToken string
	// TrustedProxies : CIDR des reverse proxies dont X-Forwarded-For est cru.
	TrustedProxies []netip.Prefix
	// AutoMigrate applique les migrations au démarrage de `serve` (défaut : true).
	AutoMigrate bool
}

// IsProd indique si l'application tourne en production.
func (c Config) IsProd() bool { return c.Env == EnvProd }

// Load lit l'environnement du process ; en dev, un fichier .env du répertoire
// courant sert de repli (les variables réelles ont priorité).
func Load() (Config, error) {
	getenv := os.Getenv
	if getenv("DOCTOLIBRE_ENV") == "" || getenv("DOCTOLIBRE_ENV") == EnvDev {
		if m, err := parseEnvFile(".env"); err == nil {
			getenv = func(k string) string {
				if v := os.Getenv(k); v != "" {
					return v
				}
				return m[k]
			}
		}
	}
	return LoadFrom(getenv)
}

// LoadFrom construit la configuration à partir d'une fonction de lecture
// (os.Getenv en production, une map dans les tests).
func LoadFrom(getenv func(string) string) (Config, error) {
	get := func(key, def string) string {
		if v := strings.TrimSpace(getenv(key)); v != "" {
			return v
		}
		return def
	}
	var errs []error
	proxies, err := ParseCIDRs(getenv("DOCTOLIBRE_TRUSTED_PROXIES"))
	if err != nil {
		errs = append(errs, fmt.Errorf("DOCTOLIBRE_TRUSTED_PROXIES: %w", err))
	}
	autoMigrate := true
	if v := strings.TrimSpace(getenv("DOCTOLIBRE_AUTO_MIGRATE")); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			errs = append(errs, fmt.Errorf("DOCTOLIBRE_AUTO_MIGRATE: %q invalide (true|false)", v))
		}
		autoMigrate = b
	}
	c := Config{
		TrustedProxies: proxies,
		AutoMigrate:    autoMigrate,
		HTTPAddr:       get("DOCTOLIBRE_HTTP_ADDR", ":8080"),
		DatabaseURL:    get("DOCTOLIBRE_DATABASE_URL", ""),
		BaseURL:        get("DOCTOLIBRE_BASE_URL", ""),
		SecretKey:      get("DOCTOLIBRE_SECRET_KEY", ""),
		Env:            get("DOCTOLIBRE_ENV", EnvDev),
		Timezone:       get("DOCTOLIBRE_TIMEZONE", "Europe/Paris"),

		MetricsToken: get("DOCTOLIBRE_METRICS_TOKEN", ""),
	}
	if c.BaseURL == "" && c.Env == EnvDev {
		c.BaseURL = "http://localhost:8080"
	}
	if err := c.Validate(); err != nil {
		errs = append(errs, err)
	}
	if len(errs) > 0 {
		return Config{}, errors.Join(errs...)
	}
	return c, nil
}

// Validate vérifie la cohérence de la configuration.
func (c Config) Validate() error {
	var errs []error
	if c.Env != EnvDev && c.Env != EnvProd {
		errs = append(errs, fmt.Errorf("DOCTOLIBRE_ENV: %q invalide (dev|prod)", c.Env))
	}
	if _, err := time.LoadLocation(c.Timezone); err != nil {
		errs = append(errs, fmt.Errorf("DOCTOLIBRE_TIMEZONE: %q invalide: %w", c.Timezone, err))
	}
	if c.BaseURL != "" {
		if u, err := url.Parse(c.BaseURL); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			errs = append(errs, fmt.Errorf("DOCTOLIBRE_BASE_URL: %q n'est pas une URL http(s) valide", c.BaseURL))
		}
	}
	if c.SecretKey != "" && len(c.SecretKey) < minSecretLen {
		errs = append(errs, fmt.Errorf("DOCTOLIBRE_SECRET_KEY: %d octets, %d minimum", len(c.SecretKey), minSecretLen))
	}
	if c.MetricsToken != "" && len(c.MetricsToken) < minMetricsTokenLen {
		errs = append(errs, fmt.Errorf("DOCTOLIBRE_METRICS_TOKEN: %d octets, %d minimum", len(c.MetricsToken), minMetricsTokenLen))
	}
	if c.Env == EnvProd {
		if c.SecretKey == "" {
			errs = append(errs, errors.New("DOCTOLIBRE_SECRET_KEY: obligatoire en prod"))
		}
		if c.DatabaseURL == "" {
			errs = append(errs, errors.New("DOCTOLIBRE_DATABASE_URL: obligatoire en prod"))
		}
		if c.BaseURL == "" {
			errs = append(errs, errors.New("DOCTOLIBRE_BASE_URL: obligatoire en prod"))
		}
	}
	return errors.Join(errs...)
}

// parseEnvFile lit un fichier KEY=VALUE (commentaires #, guillemets optionnels).
func parseEnvFile(path string) (map[string]string, error) {
	f, err := os.Open(path) //nolint:gosec // chemin fixe, fichier de dev
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	out := map[string]string{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		v = strings.TrimSpace(v)
		if len(v) >= 2 && (v[0] == '"' || v[0] == '\'') && v[len(v)-1] == v[0] {
			v = v[1 : len(v)-1]
		}
		out[strings.TrimSpace(k)] = v
	}
	return out, sc.Err()
}

// ParseCIDRs lit une liste de CIDR (ou d'IP seules) séparés par des virgules ou
// des espaces ; vide -> nil.
func ParseCIDRs(s string) ([]netip.Prefix, error) {
	var out []netip.Prefix
	for _, f := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ' ' || r == '\t' || r == '\n' }) {
		if p, err := netip.ParsePrefix(f); err == nil {
			out = append(out, p.Masked())
			continue
		}
		a, err := netip.ParseAddr(f)
		if err != nil {
			return nil, fmt.Errorf("%q n'est ni un CIDR ni une IP", f)
		}
		out = append(out, netip.PrefixFrom(a, a.BitLen()))
	}
	return out, nil
}
