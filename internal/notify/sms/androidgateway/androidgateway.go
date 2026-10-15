// Package androidgateway envoie via l'app open source « SMS Gateway for
// Android » (capcom6) : mode local (http://IP-du-téléphone:8080) ou cloud
// (https://api.sms-gate.app/3rdparty/v1). Le SMS part avec la SIM du téléphone :
// l'expéditeur n'est pas configurable.
package androidgateway

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"doctolibre/internal/notify/sms"
)

// CloudURL est l'API cloud de l'application.
const CloudURL = "https://api.sms-gate.app/3rdparty/v1"

// Config du driver. URL est la base de l'API (sans /message) ; vide = cloud.
type Config struct {
	URL    string
	User   string
	Pass   string
	Client *http.Client
}

// Sender est le driver.
type Sender struct{ cfg Config }

// New valide la configuration.
func New(cfg Config) (*Sender, error) {
	if cfg.URL == "" {
		cfg.URL = CloudURL
	}
	if cfg.User == "" || cfg.Pass == "" {
		return nil, errors.New("androidgateway: identifiants obligatoires")
	}
	if cfg.Client == nil {
		cfg.Client = sms.NewHTTPClient()
	}
	cfg.URL = strings.TrimRight(cfg.URL, "/")
	return &Sender{cfg: cfg}, nil
}

// Send implémente sms.Sender.
func (s *Sender) Send(ctx context.Context, m sms.SMS) (string, error) {
	payload, err := json.Marshal(map[string]any{"message": m.Body, "phoneNumbers": []string{m.To}})
	if err != nil {
		return "", fmt.Errorf("androidgateway: encodage: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.cfg.URL+"/message", bytes.NewReader(payload))
	if err != nil {
		return "", sms.PermanentError{Err: fmt.Errorf("androidgateway: requête: %w", err)}
	}
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth(s.cfg.User, s.cfg.Pass)
	resp, err := s.cfg.Client.Do(req)
	if err != nil {
		if ue := errors.Unwrap(err); ue != nil {
			err = ue
		}
		return "", fmt.Errorf("androidgateway: appel: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return "", sms.StatusError{Status: resp.StatusCode}
	}
	var out struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(data, &out)
	return out.ID, nil
}
