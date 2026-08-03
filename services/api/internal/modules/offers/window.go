// Package offers is a vendor's time-boxed sale: a price that is true between
// two times of day and not otherwise.
//
// Named for the mechanic rather than the hour. The design called these "midnight
// offers" because that is the case everyone pictures — a kitchen clearing stock
// at 23:00 — but the same thing is a 14:00 lull filler or a Friday brunch
// window, and a package called `midnight` would be wrong the first time a
// restaurant ran one at noon.
//
// This is the first user of subject.ModeWindow, which has been in the seam
// unexercised since it was written: a window counts *up* and fills, where a
// countdown drains. The distinction is why they are two modes and not one — see
// the note on subject.Mode.
package offers

import (
	"time"
)

// Window is a recurring rule — "22:00 to 02:00, every day" — as stored.
//
// Times of day and not instants. The vendor is stating a standing fact about
// their week, and materialising tonight's pair would need rewriting every night
// and would be an hour wrong twice a year anywhere that shifts its clocks.
type Window struct {
	// StartLocal and EndLocal are minutes past midnight in the branch's zone.
	// An end at or before the start means the window crosses midnight.
	StartLocal int
	EndLocal   int
	// Days is ISO 8601 — 1 Monday … 7 Sunday, matching Postgres's `isodow`.
	// Empty means every day.
	Days []int
	// RunsFrom and RunsUntil bound a sale that is not standing. Zero means
	// unbounded on that side.
	RunsFrom  time.Time
	RunsUntil time.Time
}

// Occurrence is one resolved run of the window.
type Occurrence struct {
	Start time.Time
	End   time.Time
	// Date is the calendar day the occurrence is filed under — its *start* day,
	// even when the window runs past midnight into the next one. A claim made at
	// 00:30 during a 22:00–02:00 window belongs to the night that began
	// yesterday, and filing it under today would let one person claim twice.
	Date time.Time
	// Live is true when `now` falls inside it.
	Live bool
}

// CrossesMidnight reports whether the window runs into the next day.
func (w Window) CrossesMidnight() bool { return w.EndLocal <= w.StartLocal }

// Duration is how long one occurrence lasts.
func (w Window) Duration() time.Duration {
	minutes := w.EndLocal - w.StartLocal
	if minutes <= 0 {
		minutes += 24 * 60
	}
	return time.Duration(minutes) * time.Minute
}

// Resolve returns the occurrence that is live now, or the next one to start.
//
// The two are returned by the same function on purpose. A reel about an offer
// has to render whether the offer is running or not — "1h 20m of 4h" while it
// is on, "starts 22:00" before — and asking the caller to make two calls and
// decide between them is how one of the two branches ends up untested.
//
// ok=false means the offer has no future occurrence at all: its calendar range
// has passed, or it runs on no days.
func (w Window) Resolve(now time.Time) (Occurrence, bool) {
	// Yesterday first, because a window that crosses midnight may still be
	// running from the previous day — at 00:30 the live occurrence of a
	// 22:00–02:00 offer started yesterday, and looking only at today would
	// report it as not yet begun.
	for offset := -1; offset <= searchDays; offset++ {
		day := startOfDay(now).AddDate(0, 0, offset)
		occ, ok := w.occurrenceOn(day)
		if !ok {
			continue
		}
		// Ended already — keep looking forward.
		if !occ.End.After(now) {
			continue
		}
		occ.Live = !occ.Start.After(now)
		return occ, true
	}
	return Occurrence{}, false
}

// searchDays bounds the forward scan.
//
// Eight, so a window that runs on exactly one weekday is always found from any
// starting day. A wider scan would keep looking for an offer whose calendar
// range ended, which is a row that should be deactivated rather than searched
// for.
const searchDays = 8

// occurrenceOn builds the occurrence starting on a given local day, if the rule
// runs that day.
func (w Window) occurrenceOn(day time.Time) (Occurrence, bool) {
	if !w.runsOn(day) {
		return Occurrence{}, false
	}
	start := day.Add(time.Duration(w.StartLocal) * time.Minute)
	return Occurrence{
		Start: start,
		End:   start.Add(w.Duration()),
		Date:  day,
	}, true
}

// runsOn tests the day-of-week set and the calendar bounds.
//
// Both are tested against the *start* day. A 22:00–02:00 offer that runs on
// Fridays runs on Friday night and finishes on Saturday morning; asking whether
// Saturday is in the set would end it four hours early every week.
func (w Window) runsOn(day time.Time) bool {
	if !w.RunsFrom.IsZero() && day.Before(startOfDay(w.RunsFrom)) {
		return false
	}
	if !w.RunsUntil.IsZero() && day.After(startOfDay(w.RunsUntil)) {
		return false
	}
	if len(w.Days) == 0 {
		return true
	}
	iso := isoWeekday(day)
	for _, d := range w.Days {
		if d == iso {
			return true
		}
	}
	return false
}

// isoWeekday converts Go's Sunday-is-zero to ISO's Monday-is-one, which is what
// the column stores and what Postgres's `isodow` returns.
func isoWeekday(t time.Time) int {
	if t.Weekday() == time.Sunday {
		return 7
	}
	return int(t.Weekday())
}

// startOfDay truncates in the value's own location.
//
// Not `Truncate(24h)`, which works on the absolute instant and therefore lands
// at a different wall-clock time in every zone that is not a whole number of
// hours from UTC — and gets the day boundary wrong across a DST shift.
func startOfDay(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}

// MinutesFromTime converts a wall clock reading into the stored representation.
func MinutesFromTime(hour, minute int) int { return hour*60 + minute }
