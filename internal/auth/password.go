// Package auth gère l'authentification du personnel : mots de passe, sessions, middleware.
package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"golang.org/x/crypto/argon2"
)

// Paramètres argon2id (RFC 9106, profil « mémoire réduite » adapté à un petit VPS) :
// 64 MiB, 1 passe, 2 voies de parallélisme, sel 16 octets, clé 32 octets.
const (
	argonMemoryKiB = 64 * 1024
	argonTime      = 1
	argonThreads   = 2
	argonSaltLen   = 16
	argonKeyLen    = 32

	// Bornes acceptées à la vérification (protège d'une valeur aberrante en base).
	maxArgonMemoryKiB = 1024 * 1024
	maxArgonTime      = 10
	maxArgonThreads   = 16
)

var errBadHash = errors.New("hash de mot de passe invalide")

// Hash retourne le mot de passe haché au format PHC :
// $argon2id$v=19$m=65536,t=1,p=2$<sel b64>$<clé b64>.
func Hash(password string) (string, error) {
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("sel: %w", err)
	}
	return encode(password, salt, argonMemoryKiB, argonTime, argonThreads), nil
}

func encode(password string, salt []byte, mem, t uint32, p uint8) string {
	key := argon2.IDKey([]byte(password), salt, t, mem, p, argonKeyLen)
	b64 := base64.RawStdEncoding
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, mem, t, p, b64.EncodeToString(salt), b64.EncodeToString(key))
}

// Verify compare le mot de passe au hash PHC, en temps constant sur la clé dérivée.
// Une erreur signifie que le hash est inexploitable (pas que le mot de passe est faux).
func Verify(password, encoded string) (bool, error) {
	parts := strings.Split(encoded, "$")
	// ["", "argon2id", "v=19", "m=..,t=..,p=..", salt, key]
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" {
		return false, errBadHash
	}
	if parts[2] != "v="+strconv.Itoa(argon2.Version) {
		return false, errBadHash
	}
	params := strings.Split(parts[3], ",")
	if len(params) != 3 {
		return false, errBadHash
	}
	var vals [3]uint32
	for i, name := range []string{"m=", "t=", "p="} {
		v, ok := strings.CutPrefix(params[i], name)
		if !ok {
			return false, errBadHash
		}
		n, err := strconv.ParseUint(v, 10, 32)
		if err != nil {
			return false, errBadHash
		}
		vals[i] = uint32(n)
	}
	mem, t, p := vals[0], vals[1], vals[2]
	if mem == 0 || mem > maxArgonMemoryKiB || t == 0 || t > maxArgonTime || p == 0 || p > maxArgonThreads {
		return false, errBadHash
	}
	b64 := base64.RawStdEncoding
	salt, err := b64.DecodeString(parts[4])
	if err != nil || len(salt) == 0 {
		return false, errBadHash
	}
	want, err := b64.DecodeString(parts[5])
	if err != nil || len(want) == 0 || len(want) > 128 {
		return false, errBadHash
	}
	got := argon2.IDKey([]byte(password), salt, t, mem, uint8(p), uint32(len(want))) //nolint:gosec // p borné à maxArgonThreads, len(want) ≤ 128
	return subtle.ConstantTimeCompare(got, want) == 1, nil
}

var (
	dummyOnce sync.Once
	dummyHash string
)

// dummyVerify dépense le même coût qu'une vérification réelle (utilisateur inconnu),
// pour que le temps de réponse ne révèle pas l'existence d'un compte.
func dummyVerify(password string) {
	dummyOnce.Do(func() {
		dummyHash, _ = Hash("dummy-password-for-timing")
	})
	_, _ = Verify(password, dummyHash)
}
