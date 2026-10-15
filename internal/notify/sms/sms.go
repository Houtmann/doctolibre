// Package sms contient le contrat commun des fournisseurs SMS (type SMS,
// interface Sender, erreurs, segments GSM-7/UCS-2). Les drivers vivent dans
// les sous-packages ; le choix du driver est fait par notify.NewSMSSender.
package sms

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"
)

// SMS est un message à envoyer. To est au format E.164 (+33612345678).
type SMS struct {
	To     string
	Body   string
	Sender string // expéditeur alphanumérique ; ignoré par certains drivers
}

// Sender envoie un SMS et retourne la référence du fournisseur (peut être vide).
type Sender interface {
	Send(ctx context.Context, m SMS) (ref string, err error)
}

// StatusError : le fournisseur a répondu avec un statut HTTP non 2xx.
// Le corps de la réponse n'est volontairement pas conservé (il peut contenir
// le numéro du destinataire).
type StatusError struct {
	Status int
}

func (e StatusError) Error() string {
	return fmt.Sprintf("sms: le fournisseur a répondu HTTP %d", e.Status)
}

// PermanentError marque une erreur non retentable (ex. numéro invalide).
type PermanentError struct{ Err error }

func (e PermanentError) Error() string { return e.Err.Error() }
func (e PermanentError) Unwrap() error { return e.Err }

// IsPermanent indique si retenter est inutile : erreur marquée permanente, ou
// réponse 4xx hors 408 et 429. Erreurs réseau et 5xx sont temporaires.
func IsPermanent(err error) bool {
	var p PermanentError
	if errors.As(err, &p) {
		return true
	}
	var s StatusError
	if errors.As(err, &s) {
		return s.Status >= 400 && s.Status < 500 && s.Status != http.StatusTooManyRequests && s.Status != http.StatusRequestTimeout
	}
	return false
}

// DefaultTimeout est le délai des appels HTTP des drivers.
const DefaultTimeout = 20 * time.Second

// NewHTTPClient retourne un client HTTP avec délai.
func NewHTTPClient() *http.Client { return &http.Client{Timeout: DefaultTimeout} }

// MaskPhone masque un numéro pour les logs : "+33612345678" -> "+33*******78".
func MaskPhone(p string) string {
	r := []rune(p)
	if len(r) <= 5 {
		return "***"
	}
	for i := 3; i < len(r)-2; i++ {
		r[i] = '*'
	}
	return string(r)
}
