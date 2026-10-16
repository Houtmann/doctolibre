package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"doctolibre/internal/domain"
	"doctolibre/internal/store/sqlcgen"
)

// ErrNoAvailability : aucun créneau libre sur l'horizon de réservation.
var ErrNoAvailability = errors.New("aucun créneau disponible")

// firstSearchWindow : taille des fenêtres de recherche du premier créneau, pour
// ne pas calculer tout l'horizon quand un créneau proche existe.
const firstSearchWindow = 14 * 24 * time.Hour

// FirstSlot est le premier créneau réservable, tous praticiens confondus.
type FirstSlot struct {
	Practitioner sqlcgen.Practitioner
	Type         sqlcgen.AppointmentType
	Start        time.Time
}

// PracticeTypes retourne les motifs réservables en ligne auprès d'au moins un
// praticien actif du cabinet (ordre des motifs du cabinet), et le nombre de
// praticiens qui les proposent.
func (b *Booking) PracticeTypes(ctx context.Context, practiceSlug string) (types []sqlcgen.AppointmentType, practitioners int, err error) {
	pub, err := b.Practice(ctx, practiceSlug)
	if err != nil {
		return nil, 0, err
	}
	offered := map[uuid.UUID]bool{}
	for _, pr := range pub.Practitioners {
		page, err := b.Practitioner(ctx, practiceSlug, pr.Slug)
		if err != nil {
			return nil, 0, err
		}
		if len(page.Types) > 0 {
			practitioners++
		}
		for _, t := range page.Types {
			if !offered[t.ID] {
				offered[t.ID] = true
				types = append(types, t)
			}
		}
	}
	return types, practitioners, nil
}

// FirstAvailable cherche, parmi les praticiens actifs qui proposent le motif en
// ligne, le créneau le plus proche. ErrNoAvailability s'il n'y en a aucun dans
// l'horizon ; domain.ErrNotFound si personne ne propose ce motif.
func (b *Booking) FirstAvailable(ctx context.Context, practiceSlug string, typeID uuid.UUID) (FirstSlot, error) {
	pub, err := b.Practice(ctx, practiceSlug)
	if err != nil {
		return FirstSlot{}, err
	}
	q := sqlcgen.New(b.Pool)
	now := b.Clock.Now()
	horizon := time.Duration(intervalMicros(pub.Practice.BookingHorizon)) * time.Microsecond
	var best FirstSlot
	found, offered := false, false
	for _, pr := range pub.Practitioners {
		page, err := b.Practitioner(ctx, practiceSlug, pr.Slug)
		if err != nil {
			return FirstSlot{}, err
		}
		if !offersType(page.Types, typeID) {
			continue
		}
		offered = true
		sc, err := loadSlotContext(ctx, q, pr.ID, typeID)
		if err != nil {
			return FirstSlot{}, fmt.Errorf("premier créneau: %w", err)
		}
		for from := now; from.Before(now.Add(horizon)); from = from.Add(firstSearchWindow) {
			slots, err := sc.slots(ctx, q, now, from, from.Add(firstSearchWindow))
			if err != nil {
				return FirstSlot{}, err
			}
			if len(slots) == 0 {
				continue
			}
			earliest := slots[0].Start
			for _, s := range slots[1:] {
				if s.Start.Before(earliest) {
					earliest = s.Start
				}
			}
			if !found || earliest.Before(best.Start) {
				best, found = FirstSlot{Practitioner: pr, Type: sc.typ, Start: earliest}, true
			}
			break
		}
	}
	switch {
	case !offered:
		return FirstSlot{}, domain.ErrNotFound
	case !found:
		return FirstSlot{}, ErrNoAvailability
	}
	return best, nil
}

func offersType(types []sqlcgen.AppointmentType, id uuid.UUID) bool {
	for _, t := range types {
		if t.ID == id {
			return true
		}
	}
	return false
}
