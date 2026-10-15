package service

import (
	"testing"
	"time"
)

func TestDayBeforeAtAcrossDST(t *testing.T) {
	t.Parallel()
	paris, err := time.LoadLocation("Europe/Paris")
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name  string
		start time.Time
		hour  int
		want  time.Time
	}{
		{"jour normal", time.Date(2026, 10, 20, 9, 0, 0, 0, paris), 18, time.Date(2026, 10, 19, 16, 0, 0, 0, time.UTC)},
		{"lendemain du passage à l'heure d'été (jour de 23 h)", time.Date(2026, 3, 30, 9, 0, 0, 0, paris), 18, time.Date(2026, 3, 29, 16, 0, 0, 0, time.UTC)},
		{"lendemain du retour à l'heure d'hiver (jour de 25 h)", time.Date(2026, 10, 26, 9, 0, 0, 0, paris), 18, time.Date(2026, 10, 25, 17, 0, 0, 0, time.UTC)},
		{"rendez-vous le jour du changement", time.Date(2026, 10, 25, 9, 0, 0, 0, paris), 18, time.Date(2026, 10, 24, 16, 0, 0, 0, time.UTC)},
		{"heure inexistante (2 h 30 le 29 mars) : décalée, jamais d'erreur", time.Date(2026, 3, 30, 9, 0, 0, 0, paris), 2, time.Date(2026, 3, 29, 1, 0, 0, 0, time.UTC)},
		{"start fourni en UTC", time.Date(2026, 10, 20, 7, 0, 0, 0, time.UTC), 18, time.Date(2026, 10, 19, 16, 0, 0, 0, time.UTC)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := dayBeforeAt(tt.start, tt.hour, paris); !got.Equal(tt.want) {
				t.Errorf("dayBeforeAt = %v, attendu %v", got.UTC(), tt.want)
			}
		})
	}
}

func TestReminderDue(t *testing.T) {
	t.Parallel()
	send := time.Date(2026, 10, 19, 16, 0, 0, 0, time.UTC)
	tests := []struct {
		name       string
		now, touch time.Time
		want       bool
	}{
		{"pas encore l'heure", send.Add(-time.Minute), send.Add(-48 * time.Hour), false},
		{"à l'heure", send, send.Add(-48 * time.Hour), true},
		{"rattrapage après une panne", send.Add(3 * time.Hour), send.Add(-48 * time.Hour), true},
		{"réservé après l'heure d'envoi : confirmation toute fraîche", send.Add(time.Hour), send.Add(30 * time.Minute), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := reminderDue(send, tt.now, tt.touch); got != tt.want {
				t.Errorf("reminderDue = %v, attendu %v", got, tt.want)
			}
		})
	}
}
