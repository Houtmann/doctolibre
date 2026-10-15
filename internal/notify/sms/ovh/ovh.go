// Package ovh envoie via l'API OVH SMS (POST /sms/{service}/jobs) avec la
// signature OVH : "$1$" + sha1hex(AS+CK+METHOD+URL+BODY+TSTAMP), joints par "+".
package ovh

import (
	"bytes"
	"context"
	"crypto/sha1" //nolint:gosec // imposé par le schéma de signature OVH
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"doctolibre/internal/domain"
	"doctolibre/internal/notify/sms"
)

// DefaultEndpoint : API OVH Europe.
const DefaultEndpoint = "https://eu.api.ovh.com/1.0"

// Config du driver.
type Config struct {
	Endpoint    string // défaut DefaultEndpoint
	ServiceName string // sms-xx123456-1
	AppKey      string
	AppSecret   string
	ConsumerKey string
	Clock       domain.Clock
	Client      *http.Client
}

// Sender est le driver.
type Sender struct{ cfg Config }

// New valide la configuration.
func New(cfg Config) (*Sender, error) {
	if cfg.ServiceName == "" || cfg.AppKey == "" || cfg.AppSecret == "" || cfg.ConsumerKey == "" {
		return nil, errors.New("ovh: service, clé d'application, secret et consumer key obligatoires")
	}
	if cfg.Clock == nil {
		return nil, errors.New("ovh: horloge obligatoire")
	}
	if cfg.Endpoint == "" {
		cfg.Endpoint = DefaultEndpoint
	}
	if cfg.Client == nil {
		cfg.Client = sms.NewHTTPClient()
	}
	cfg.Endpoint = strings.TrimRight(cfg.Endpoint, "/")
	return &Sender{cfg: cfg}, nil
}

// Signature calcule l'en-tête X-Ovh-Signature.
func Signature(appSecret, consumerKey, method, fullURL, body string, tstamp int64) string {
	raw := strings.Join([]string{appSecret, consumerKey, method, fullURL, body, strconv.FormatInt(tstamp, 10)}, "+")
	sum := sha1.Sum([]byte(raw)) //nolint:gosec
	return "$1$" + hex.EncodeToString(sum[:])
}

// Send implémente sms.Sender.
func (s *Sender) Send(ctx context.Context, m sms.SMS) (string, error) {
	job := map[string]any{
		"message": m.Body, "receivers": []string{m.To},
		"noStopClause": true, // SMS transactionnel : pas de mention STOP ajoutée
		"priority":     "high",
	}
	if m.Sender != "" {
		job["sender"] = m.Sender
	} else {
		job["senderForResponse"] = true
	}
	payload, err := json.Marshal(job)
	if err != nil {
		return "", fmt.Errorf("ovh: encodage: %w", err)
	}
	fullURL := s.cfg.Endpoint + "/sms/" + url.PathEscape(s.cfg.ServiceName) + "/jobs"
	ts := s.cfg.Clock.Now().Unix()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, fullURL, bytes.NewReader(payload))
	if err != nil {
		return "", sms.PermanentError{Err: fmt.Errorf("ovh: requête: %w", err)}
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Ovh-Application", s.cfg.AppKey)
	req.Header.Set("X-Ovh-Consumer", s.cfg.ConsumerKey)
	req.Header.Set("X-Ovh-Timestamp", strconv.FormatInt(ts, 10))
	req.Header.Set("X-Ovh-Signature", Signature(s.cfg.AppSecret, s.cfg.ConsumerKey, http.MethodPost, fullURL, string(payload), ts))
	resp, err := s.cfg.Client.Do(req)
	if err != nil {
		if ue := errors.Unwrap(err); ue != nil {
			err = ue
		}
		return "", fmt.Errorf("ovh: appel: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return "", sms.StatusError{Status: resp.StatusCode}
	}
	var out struct {
		IDs              []json.Number `json:"ids"`
		InvalidReceivers []string      `json:"invalidReceivers"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return "", fmt.Errorf("ovh: réponse illisible: %w", err)
	}
	if len(out.InvalidReceivers) > 0 {
		return "", sms.PermanentError{Err: errors.New("ovh: numéro refusé")}
	}
	if len(out.IDs) > 0 {
		return out.IDs[0].String(), nil
	}
	return "", nil
}
