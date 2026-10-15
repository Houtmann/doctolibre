package domain

import "time"

// Clock fournit l'heure courante. Le code métier n'appelle jamais time.Now()
// directement : il reçoit un Clock (les tests injectent testutil.FixedClock).
type Clock interface {
	Now() time.Time
}

// SystemClock est le Clock de production.
type SystemClock struct{}

// Now retourne l'heure courante en UTC.
func (SystemClock) Now() time.Time { return time.Now().UTC() }
