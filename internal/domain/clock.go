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

// ChangeAllowed dit si un rendez-vous commençant à start peut encore être déplacé ou
// annulé par le patient à l'instant now : il faut au moins minNotice avant le début.
// Règle unique (relecture du lien, contrôle d'annulation, rédaction des messages).
func ChangeAllowed(start, now time.Time, minNotice time.Duration) bool {
	return start.Sub(now) >= minNotice
}
