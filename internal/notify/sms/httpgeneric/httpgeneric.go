// Package httpgeneric envoie un SMS par un webhook HTTP configurable
// (méthode, URL, en-têtes, corps en text/template, auth basic/bearer).
package httpgeneric

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"text/template"

	"doctolibre/internal/notify/sms"
)

// Config du driver. Body est un text/template avec {{.To}}, {{.Body}},
// {{.Sender}} ; {{json .Body}} produit une chaîne JSON entre guillemets,
// {{urlquery .Body}} échappe pour un formulaire.
type Config struct {
	Method    string // défaut POST
	URL       string
	Headers   map[string]string
	Body      string
	BasicUser string
	BasicPass string
	Bearer    string
	Client    *http.Client
}

// Sender est le driver.
type Sender struct {
	cfg  Config
	body *template.Template
}

// New valide la configuration et compile le template.
func New(cfg Config) (*Sender, error) {
	if cfg.URL == "" {
		return nil, errors.New("httpgeneric: URL obligatoire")
	}
	if cfg.Method == "" {
		cfg.Method = http.MethodPost
	}
	if cfg.Client == nil {
		cfg.Client = sms.NewHTTPClient()
	}
	t, err := template.New("body").Funcs(template.FuncMap{
		"json": func(s string) (string, error) { b, err := json.Marshal(s); return string(b), err },
	}).Parse(cfg.Body)
	if err != nil {
		return nil, fmt.Errorf("httpgeneric: template du corps: %w", err)
	}
	return &Sender{cfg: cfg, body: t}, nil
}

// Send implémente sms.Sender.
func (s *Sender) Send(ctx context.Context, m sms.SMS) (string, error) {
	var buf bytes.Buffer
	if err := s.body.Execute(&buf, m); err != nil {
		return "", sms.PermanentError{Err: fmt.Errorf("httpgeneric: rendu du corps: %w", err)}
	}
	req, err := http.NewRequestWithContext(ctx, s.cfg.Method, s.cfg.URL, &buf)
	if err != nil {
		return "", sms.PermanentError{Err: fmt.Errorf("httpgeneric: requête: %w", err)}
	}
	for k, v := range s.cfg.Headers {
		req.Header.Set(k, v)
	}
	switch {
	case s.cfg.Bearer != "":
		req.Header.Set("Authorization", "Bearer "+s.cfg.Bearer)
	case s.cfg.BasicUser != "":
		req.SetBasicAuth(s.cfg.BasicUser, s.cfg.BasicPass)
	}
	resp, err := s.cfg.Client.Do(req)
	if err != nil {
		// L'erreur *url.Error contient l'URL (parfois un secret en query) : on ne garde que la cause.
		if ue := errors.Unwrap(err); ue != nil {
			err = ue
		}
		return "", fmt.Errorf("httpgeneric: appel: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return "", sms.StatusError{Status: resp.StatusCode}
	}
	return ref(data), nil
}

// ref extrait un identifiant de la réponse JSON (sid, id, messageId…), s'il existe.
func ref(data []byte) string {
	var m map[string]any
	if json.Unmarshal(data, &m) != nil {
		return ""
	}
	for _, k := range []string{"sid", "id", "messageId", "message_id", "uuid"} {
		if v, ok := m[k]; ok {
			return strings.TrimSpace(fmt.Sprint(v))
		}
	}
	return ""
}
