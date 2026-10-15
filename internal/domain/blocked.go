// Package domain contient les types métier purs et les erreurs partagées.
package domain

import "time"

// BlockedRange calcule la zone bloquée d'un rendez-vous : le soin
// [start, end) élargi des buffers avant et après. C'est l'unique source de
// vérité pour la colonne appointment.during_blocked (contrainte EXCLUDE) et
// pour le moteur de créneaux (internal/booking). Intervalle semi-ouvert [).
// Les buffers sont des durées absolues (pas de calendrier), donc insensibles au DST.
func BlockedRange(start, end time.Time, before, after time.Duration) (time.Time, time.Time) {
	return start.Add(-before), end.Add(after)
}
