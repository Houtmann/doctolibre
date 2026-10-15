// Package notify envoie les notifications (email, SMS) : drivers, adaptateurs outbox
// et règles d'envoi.
package notify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"doctolibre/internal/config"
	"doctolibre/internal/domain"
	"doctolibre/internal/notify/sms"
	"doctolibre/internal/notify/sms/androidgateway"
	"doctolibre/internal/notify/sms/httpgeneric"
	"doctolibre/internal/notify/sms/logsms"
	"doctolibre/internal/notify/sms/ovh"
	"doctolibre/internal/outbox"
)

// SMS est un message à envoyer (défini dans le package sms pour éviter un cycle avec les drivers).
type SMS = sms.SMS

// SMSSender envoie un SMS (défini dans le package sms).
type SMSSender = sms.Sender

// NewSMSSender construit le driver choisi par DOCTOLIBRE_SMS_DRIVER.
func NewSMSSender(cfg config.SMSConfig, clock domain.Clock, logger *slog.Logger) (SMSSender, error) {
	switch cfg.Driver {
	case "", config.SMSDriverLog:
		return logsms.Sender{Logger: logger}, nil
	case config.SMSDriverHTTP:
		return httpgeneric.New(httpgeneric.Config{
			Method: cfg.HTTP.Method, URL: cfg.HTTP.URL, Headers: cfg.HTTP.Headers, Body: cfg.HTTP.Body,
			BasicUser: cfg.HTTP.BasicUser, BasicPass: cfg.HTTP.BasicPass, Bearer: cfg.HTTP.Bearer,
		})
	case config.SMSDriverAndroid:
		return androidgateway.New(androidgateway.Config{URL: cfg.AndroidURL, User: cfg.AndroidUser, Pass: cfg.AndroidPass})
	case config.SMSDriverOVH:
		return ovh.New(ovh.Config{
			Endpoint: cfg.OVHEndpoint, ServiceName: cfg.OVHService, AppKey: cfg.OVHAppKey,
			AppSecret: cfg.OVHAppSecret, ConsumerKey: cfg.OVHConsumerKey, Clock: clock,
		})
	}
	return nil, fmt.Errorf("notify: driver SMS inconnu %q", cfg.Driver)
}

// ErrSMSOptOut : le patient a refusé les SMS ; le message n'est pas envoyé.
var ErrSMSOptOut = errors.New("notify: patient désinscrit des SMS")

// SMSPayload est le payload JSON attendu des messages outbox kind='sms'.
// Le service qui enfile (T023) rend le texte et le place dans Body.
type SMSPayload struct {
	Body string `json:"body"`
	// OptIn : copie de patient.sms_opt_in au moment de l'enfilement. Absent
	// = pas de contrôle. false => le message est abandonné (hors OTP).
	OptIn *bool `json:"sms_opt_in,omitempty"`
	// OTP : code de vérification, exempté d'opt-out et de plage silencieuse.
	OTP bool `json:"otp,omitempty"`
}

// SMSDispatch adapte un SMSSender en dispatcher outbox pour kind='sms'.
// sender est l'expéditeur par défaut ; transliterate force le GSM-7 (1 segment).
// Erreurs 4xx fournisseur (hors 408/429) et numéros refusés => outbox.Permanent ;
// 429, 5xx et erreurs réseau => retentées.
func SMSDispatch(s SMSSender, sender string, transliterate bool) outbox.DispatcherFunc {
	return func(ctx context.Context, m outbox.Msg) (string, error) {
		var p SMSPayload
		if err := json.Unmarshal(m.Payload, &p); err != nil || p.Body == "" {
			return "", outbox.Permanent(errors.New("notify: payload SMS invalide (champ body obligatoire)"))
		}
		if !p.OTP && p.OptIn != nil && !*p.OptIn {
			return "", outbox.Permanent(ErrSMSOptOut)
		}
		body := p.Body
		if transliterate {
			body = sms.Transliterate(body)
		}
		ref, err := s.Send(ctx, sms.SMS{To: m.Recipient, Body: body, Sender: sender})
		if err != nil && sms.IsPermanent(err) {
			return "", outbox.Permanent(err)
		}
		return ref, err
	}
}

const (
	quietStartHour = 20
	quietEndHour   = 8
)

// SMSRunAt retourne l'instant d'envoi d'un SMS enfilé à now : immédiat pour un
// OTP ou entre 8 h et 20 h (heure du cabinet), sinon 8 h locales suivantes.
// Les jours sont avancés avec AddDate (changements d'heure).
func SMSRunAt(now time.Time, loc *time.Location, isOTP bool) time.Time {
	if isOTP {
		return now
	}
	t := now.In(loc)
	h := t.Hour()
	if h >= quietEndHour && h < quietStartHour {
		return now
	}
	day := t
	if h >= quietStartHour {
		day = t.AddDate(0, 0, 1)
	}
	return time.Date(day.Year(), day.Month(), day.Day(), quietEndHour, 0, 0, 0, loc).UTC()
}
