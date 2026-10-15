package domain

import (
	"testing"
	"time"
)

func TestBlockedRange(t *testing.T) {
	t.Parallel()
	s := time.Date(2025, 3, 30, 9, 0, 0, 0, time.UTC)
	e := s.Add(30 * time.Minute)
	bs, be := BlockedRange(s, e, 10*time.Minute, 5*time.Minute)
	if !bs.Equal(s.Add(-10*time.Minute)) || !be.Equal(e.Add(5*time.Minute)) {
		t.Fatalf("got [%v,%v)", bs, be)
	}
	bs, be = BlockedRange(s, e, 0, 0)
	if !bs.Equal(s) || !be.Equal(e) {
		t.Fatalf("sans buffer: got [%v,%v)", bs, be)
	}
}
