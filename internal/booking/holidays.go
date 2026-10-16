package booking

import (
	"sort"
	"time"
)

// easter retourne la date du dimanche de Pâques (calendrier grégorien),
// algorithme anonyme de Meeus/Jones/Butcher.
func easter(year int) time.Time {
	a := year % 19
	b, c := year/100, year%100
	d, e := b/4, b%4
	f := (b + 8) / 25
	g := (b - f + 1) / 3
	h := (19*a + b - d - g + 15) % 30
	i, k := c/4, c%4
	l := (32 + 2*e + 2*i - h - k) % 7
	m := (a + 11*h + 22*l) / 451
	month := (h + l - 7*m + 114) / 31
	day := (h+l-7*m+114)%31 + 1
	return time.Date(year, time.Month(month), day, 12, 0, 0, 0, time.UTC)
}

// FrenchHolidays retourne les jours fériés français de l'année, triés. Avec
// alsaceMoselle : en plus le Vendredi saint et le 26 décembre. Dates civiles
// uniquement (AddDate, jamais de durée en heures).
func FrenchHolidays(year int, alsaceMoselle bool) []Date {
	e := easter(year)
	days := []time.Time{
		time.Date(year, time.January, 1, 12, 0, 0, 0, time.UTC),
		e.AddDate(0, 0, 1), // lundi de Pâques
		time.Date(year, time.May, 1, 12, 0, 0, 0, time.UTC),
		time.Date(year, time.May, 8, 12, 0, 0, 0, time.UTC),
		e.AddDate(0, 0, 39), // Ascension
		e.AddDate(0, 0, 50), // lundi de Pentecôte
		time.Date(year, time.July, 14, 12, 0, 0, 0, time.UTC),
		time.Date(year, time.August, 15, 12, 0, 0, 0, time.UTC),
		time.Date(year, time.November, 1, 12, 0, 0, 0, time.UTC),
		time.Date(year, time.November, 11, 12, 0, 0, 0, time.UTC),
		time.Date(year, time.December, 25, 12, 0, 0, 0, time.UTC),
	}
	if alsaceMoselle {
		days = append(days, e.AddDate(0, 0, -2), time.Date(year, time.December, 26, 12, 0, 0, 0, time.UTC))
	}
	sort.Slice(days, func(i, j int) bool { return days[i].Before(days[j]) })
	out := make([]Date, len(days))
	for i, d := range days {
		out[i] = DateOf(d)
	}
	return out
}

// HolidaysBetween retourne les jours fériés dont la date civile (dans loc) est
// comprise entre celles de from et to incluses ; la plage peut couvrir deux années.
func HolidaysBetween(loc *time.Location, from, to time.Time, alsaceMoselle bool) []Date {
	first, last := DateOf(from.In(loc)), DateOf(to.In(loc))
	var out []Date
	for y := first.Year; y <= last.Year; y++ {
		for _, d := range FrenchHolidays(y, alsaceMoselle) {
			if !d.before(first) && !last.before(d) {
				out = append(out, d)
			}
		}
	}
	return out
}

// HolidayClosures traduit les fériés de [from, to] en exceptions Closed : la
// journée entière en heure du cabinet ([00:00, 00:00 du lendemain)) moins les
// ouvertures exceptionnelles explicites (open), soit 0 à n morceaux par jour.
// Closed l'emporte sur Open dans le moteur : l'arbitrage est donc fait ici, et
// seules les ouvertures explicites restent réservables un jour férié.
func HolidayClosures(loc *time.Location, from, to time.Time, alsaceMoselle bool, open []Interval) []Exception {
	var out []Exception
	for _, d := range HolidaysBetween(loc, from, to, alsaceMoselle) {
		s := time.Date(d.Year, d.Month, d.Day, 0, 0, 0, 0, loc)
		for _, iv := range Subtract([]Interval{{Start: s, End: s.AddDate(0, 0, 1)}}, Union(open)) {
			out = append(out, Exception{Start: iv.Start, End: iv.End, Kind: Closed})
		}
	}
	return out
}
