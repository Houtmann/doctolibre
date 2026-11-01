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
	Practice     sqlcgen.Practice
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
	var best FirstSlot
	found, offered := false, false
	for _, pr := range pub.Practitioners {
		slot, ok, off, err := b.firstFor(ctx, q, pub, pr, typeID)
		if err != nil {
			return FirstSlot{}, err
		}
		offered = offered || off
		if ok && (!found || slot.Start.Before(best.Start)) {
			best, found = slot, true
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

// FirstAvailableFor est FirstAvailable limité à un praticien (par slug) : même
// calcul, mêmes erreurs (ErrNoAvailability, domain.ErrNotFound si le praticien
// ne propose pas le motif en ligne).
func (b *Booking) FirstAvailableFor(ctx context.Context, practiceSlug, practitionerSlug string, typeID uuid.UUID) (FirstSlot, error) {
	pub, err := b.Practice(ctx, practiceSlug)
	if err != nil {
		return FirstSlot{}, err
	}
	for _, pr := range pub.Practitioners {
		if pr.Slug != practitionerSlug {
			continue
		}
		slot, ok, offered, err := b.firstFor(ctx, sqlcgen.New(b.Pool), pub, pr, typeID)
		switch {
		case err != nil:
			return FirstSlot{}, err
		case !offered:
			return FirstSlot{}, domain.ErrNotFound
		case !ok:
			return FirstSlot{}, ErrNoAvailability
		}
		return slot, nil
	}
	return FirstSlot{}, domain.ErrNotFound
}

// firstFor cherche le premier créneau d'un praticien pour un motif, par fenêtres
// de firstSearchWindow sur l'horizon. offered=false si le praticien ne propose
// pas ce motif en ligne.
func (b *Booking) firstFor(ctx context.Context, q *sqlcgen.Queries, pub PublicPractice, pr sqlcgen.Practitioner, typeID uuid.UUID) (slot FirstSlot, ok, offered bool, err error) {
	page, err := b.Practitioner(ctx, pub.Practice.Slug, pr.Slug)
	if err != nil {
		return FirstSlot{}, false, false, err
	}
	if !offersType(page.Types, typeID) {
		return FirstSlot{}, false, false, nil
	}
	sc, err := loadSlotContext(ctx, q, pr.ID, typeID)
	if err != nil {
		return FirstSlot{}, false, true, fmt.Errorf("premier créneau: %w", err)
	}
	now := b.Clock.Now()
	horizon := time.Duration(intervalMicros(pub.Practice.BookingHorizon)) * time.Microsecond
	for from := now; from.Before(now.Add(horizon)); from = from.Add(firstSearchWindow) {
		slots, err := sc.slots(ctx, q, now, from, from.Add(firstSearchWindow))
		if err != nil {
			return FirstSlot{}, false, true, err
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
		return FirstSlot{Practice: pub.Practice, Practitioner: pr, Type: sc.typ, Start: earliest}, true, true, nil
	}
	return FirstSlot{}, false, true, nil
}

func offersType(types []sqlcgen.AppointmentType, id uuid.UUID) bool {
	for _, t := range types {
		if t.ID == id {
			return true
		}
	}
	return false
}

// SoleSlug retourne le slug du cabinet quand l'instance n'en a qu'un seul
// (ok=false sinon : aucun ou plusieurs).
func (b *Booking) SoleSlug(ctx context.Context) (slug string, ok bool, err error) {
	slugs, err := sqlcgen.New(b.Pool).ListPracticeSlugsLimit2(ctx)
	if err != nil {
		return "", false, fmt.Errorf("liste des cabinets: %w", err)
	}
	if len(slugs) != 1 {
		return "", false, nil
	}
	return slugs[0], true, nil
}
