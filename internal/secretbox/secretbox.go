// Package secretbox chiffre de petits secrets stockés en base (identifiants de
// fournisseurs email/SMS) : AES-256-GCM, clé dérivée de DOCTOLIBRE_SECRET_KEY.
package secretbox

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
)

// label sépare cette dérivation de celles des autres usages de la clé secrète.
const label = "doctolibre/notify-transport/v1"

// ErrDecrypt : le chiffré est illisible (clé changée, données altérées, ou
// copiées vers un autre contexte). Le message ne contient aucune donnée.
var ErrDecrypt = errors.New("secretbox: déchiffrement impossible")

// Box chiffre et déchiffre avec une clé dérivée.
type Box struct{ aead cipher.AEAD }

// New dérive la clé AES-256 de secret (HMAC-SHA256 avec un libellé fixe).
func New(secret string) (Box, error) {
	if secret == "" {
		return Box{}, errors.New("secretbox: clé secrète vide")
	}
	m := hmac.New(sha256.New, []byte(secret))
	m.Write([]byte(label))
	block, err := aes.NewCipher(m.Sum(nil))
	if err != nil {
		return Box{}, fmt.Errorf("secretbox: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return Box{}, fmt.Errorf("secretbox: %w", err)
	}
	return Box{aead: aead}, nil
}

// Seal chiffre plain ; aad (données associées, non secrètes) doit être
// identique à l'ouverture. Le nonce aléatoire est préfixé au résultat.
func (b Box) Seal(plain, aad []byte) ([]byte, error) {
	if b.aead == nil {
		return nil, errors.New("secretbox: non initialisé")
	}
	nonce := make([]byte, b.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("secretbox: nonce: %w", err)
	}
	return b.aead.Seal(nonce, nonce, plain, aad), nil
}

// Open déchiffre le résultat de Seal.
func (b Box) Open(sealed, aad []byte) ([]byte, error) {
	if b.aead == nil {
		return nil, ErrDecrypt
	}
	n := b.aead.NonceSize()
	if len(sealed) < n+b.aead.Overhead() {
		return nil, ErrDecrypt
	}
	plain, err := b.aead.Open(nil, sealed[:n], sealed[n:], aad)
	if err != nil {
		return nil, ErrDecrypt
	}
	return plain, nil
}
