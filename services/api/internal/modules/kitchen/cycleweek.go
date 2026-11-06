package kitchen

import "time"

// CycleWeek returns which week (1..weeks) of a rotating menu cycle a date falls in.
// anchor is the cycle's week-1 start date; both dates are compared as calendar days.
func CycleWeek(anchor time.Time, weeks int, day time.Time) int {
	if weeks <= 1 {
		return 1
	}
	a := time.Date(anchor.Year(), anchor.Month(), anchor.Day(), 0, 0, 0, 0, time.UTC)
	d := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, time.UTC)
	days := int(d.Sub(a).Hours() / 24)
	w := days / 7
	if days < 0 && days%7 != 0 {
		w--
	}
	w %= weeks
	if w < 0 {
		w += weeks
	}
	return w + 1
}
