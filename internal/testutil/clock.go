package testutil

import (
	"sync"
	"time"

	"doctolibre/internal/domain"
)

// FixedClock est un domain.Clock figé, avançable à la main. Sûr en concurrence.
type FixedClock struct {
	mu sync.Mutex
	t  time.Time
}

var _ domain.Clock = (*FixedClock)(nil)

// NewFixedClock retourne une horloge figée à t.
func NewFixedClock(t time.Time) *FixedClock { return &FixedClock{t: t} }

// Now retourne l'instant courant de l'horloge.
func (c *FixedClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

// Advance avance l'horloge de d.
func (c *FixedClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}
