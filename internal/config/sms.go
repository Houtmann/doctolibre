package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

// Drivers SMS.
const (
	SMSDriverLog     = "log"
	SMSDriverHTTP    = "http"
	SMSDriverAndroid = "android"
	SMSDriverOVH     = "ovh"
)

// SMSConfig regroupe la configuration des SMS (DOCTOLIBRE_SMS_*).
type SMSConfig struct {
	Driver         string // log (défaut) | http | android | ovh
	Sender         string // expéditeur alphanumérique (selon driver)
	Transliterate  bool   // ê->e, œ->oe… pour rester en GSM-7 (défaut true)
	HTTP           SMSHTTPConfig
	AndroidURL     string // vide = cloud
	AndroidUser    string
	AndroidPass    string
	OVHService     string
	OVHAppKey      string
	OVHAppSecret   string
	OVHConsumerKey string
	OVHEndpoint    string // vide = https://eu.api.ovh.com/1.0
}

// SMSHTTPConfig : driver « http » (webhook générique).
type SMSHTTPConfig struct {
	Method    string
	URL       string
	Headers   map[string]string
	Body      string
	BasicUser string
	BasicPass string
	Bearer    string
}

func loadSMS(get func(key, def string) string) (SMSConfig, error) {
	c := SMSConfig{
		Driver:         strings.ToLower(get("DOCTOLIBRE_SMS_DRIVER", SMSDriverLog)),
		Sender:         get("DOCTOLIBRE_SMS_SENDER", ""),
		Transliterate:  !isFalse(get("DOCTOLIBRE_SMS_TRANSLITERATE", "true")),
		AndroidURL:     get("DOCTOLIBRE_SMS_ANDROID_URL", ""),
		AndroidUser:    get("DOCTOLIBRE_SMS_ANDROID_USER", ""),
		AndroidPass:    get("DOCTOLIBRE_SMS_ANDROID_PASSWORD", ""),
		OVHService:     get("DOCTOLIBRE_SMS_OVH_SERVICE", ""),
		OVHAppKey:      get("DOCTOLIBRE_SMS_OVH_APP_KEY", ""),
		OVHAppSecret:   get("DOCTOLIBRE_SMS_OVH_APP_SECRET", ""),
		OVHConsumerKey: get("DOCTOLIBRE_SMS_OVH_CONSUMER_KEY", ""),
		OVHEndpoint:    get("DOCTOLIBRE_SMS_OVH_ENDPOINT", ""),
		HTTP: SMSHTTPConfig{
			Method:    strings.ToUpper(get("DOCTOLIBRE_SMS_HTTP_METHOD", http.MethodPost)),
			URL:       get("DOCTOLIBRE_SMS_HTTP_URL", ""),
			Body:      get("DOCTOLIBRE_SMS_HTTP_BODY", ""),
			BasicUser: get("DOCTOLIBRE_SMS_HTTP_BASIC_USER", ""),
			BasicPass: get("DOCTOLIBRE_SMS_HTTP_BASIC_PASSWORD", ""),
			Bearer:    get("DOCTOLIBRE_SMS_HTTP_BEARER", ""),
		},
	}
	if h := get("DOCTOLIBRE_SMS_HTTP_HEADERS", ""); h != "" {
		if err := json.Unmarshal([]byte(h), &c.HTTP.Headers); err != nil {
			return c, fmt.Errorf("DOCTOLIBRE_SMS_HTTP_HEADERS: objet JSON attendu ({\"En-tête\":\"valeur\"}): %w", err)
		}
	}
	return c, nil
}

func isFalse(s string) bool {
	switch strings.ToLower(s) {
	case "false", "0", "no", "non", "off":
		return true
	}
	return false
}

// Validate vérifie les variables requises par le driver choisi.
func (c SMSConfig) Validate() error {
	var errs []error
	need := func(name, v string) {
		if v == "" {
			errs = append(errs, fmt.Errorf("%s: obligatoire avec DOCTOLIBRE_SMS_DRIVER=%s", name, c.Driver))
		}
	}
	switch c.Driver {
	case "", SMSDriverLog:
	case SMSDriverHTTP:
		need("DOCTOLIBRE_SMS_HTTP_URL", c.HTTP.URL)
		need("DOCTOLIBRE_SMS_HTTP_BODY", c.HTTP.Body)
	case SMSDriverAndroid:
		need("DOCTOLIBRE_SMS_ANDROID_USER", c.AndroidUser)
		need("DOCTOLIBRE_SMS_ANDROID_PASSWORD", c.AndroidPass)
	case SMSDriverOVH:
		need("DOCTOLIBRE_SMS_OVH_SERVICE", c.OVHService)
		need("DOCTOLIBRE_SMS_OVH_APP_KEY", c.OVHAppKey)
		need("DOCTOLIBRE_SMS_OVH_APP_SECRET", c.OVHAppSecret)
		need("DOCTOLIBRE_SMS_OVH_CONSUMER_KEY", c.OVHConsumerKey)
	default:
		errs = append(errs, fmt.Errorf("DOCTOLIBRE_SMS_DRIVER: %q invalide (log|http|android|ovh)", c.Driver))
	}
	return errors.Join(errs...)
}
