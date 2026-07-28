// Package subscriptions is the recurrence engine.
//
// It is deliberately not part of the meals vertical, and that placement is the
// main finding of Phase 5. PLAN.md §5 P2 framed meals as the architecture's
// exam — "if adding meals requires changing commerce or scheduling internals
// rather than adding strategies, stop and fix the abstraction." A meal box
// ordered for tomorrow passed that exam with zero engine changes. A meal
// *subscription* did not, and not because meals are special: nothing in the
// engine modelled **recurrence**, and a weekly laundry pickup would have hit
// exactly the same wall.
//
// So recurrence turned out to be a second axis rather than a vertical concern,
// and this package is engine code that happens to be needed first by food.
// Nothing below this line mentions a meal.
//
// The scheduler itself is a pure function. PLAN.md §4 describes a nightly job
// that "materializes a rolling 14-day window of fulfillments from the plan's
// rules", and every hard part of that is a date-arithmetic decision — pauses,
// skips, cutoffs, timezones, re-runs. Those belong somewhere a test can reach
// them without a database and without waiting a day.
package subscriptions

import (
	"sort"
	"time"

	"github.com/google/uuid"
)

// Horizon is how far ahead fulfillments are created.
//
// Fourteen days, from PLAN.md §4. The number is a balance between two failure
// modes and it is worth naming both: too short and a vendor cannot see enough
// of next week to buy ingredients; too long and every pause, skip or plan
// change has to reach back and unpick work that was already scheduled, which is
// the thing that makes subscription systems intractable.
const Horizon = 14 * 24 * time.Hour

// Weekday numbering is 0 = Sunday, matching branch_hours and menu_cycle_days.
//
// The GCC working week starts Sunday. Go's time.Weekday agrees (Sunday == 0),
// but ISO-8601 does not, and mixing the two produces meals delivered on the
// wrong day — a bug that is invisible in code review and extremely visible to
// the customer.
type Weekday int

// Rule is one recurring delivery: this weekday, this slot, this window, this
// address.
type Rule struct {
	Weekday   Weekday
	Slot      string
	WindowID  uuid.UUID
	AddressID uuid.NullUUID
}

// Pause is a date range the customer is away. Inclusive of both ends —
// "away from the 3rd to the 7th" includes the 7th, and the alternative reading
// costs one delivered meal to an empty flat every single time.
type Pause struct {
	ID       uuid.UUID
	From     time.Time
	To       time.Time
	Mode     string
	Inactive bool
}

// Skip suppresses one date. An empty Slot suppresses every slot on that date.
type Skip struct {
	Date time.Time
	Slot string
}

// Subscription is the slice the scheduler needs. Narrow on purpose: a scheduler
// that can see the customer's payment method will eventually consult it.
type Subscription struct {
	ID        uuid.UUID
	Start     time.Time
	End       time.Time // zero for open-ended
	Location  *time.Location
	AddressID uuid.UUID
	// Suspended stops new materialisation without cancelling anything. This is
	// the dunning grace period's effect: PLAN.md §6 requires that a failed
	// renewal suspends fulfillments and never deletes the subscription.
	Suspended bool
}

// Occurrence is one delivery the scheduler says should exist.
type Occurrence struct {
	Date      time.Time
	Slot      string
	WindowID  uuid.UUID
	AddressID uuid.UUID
	// Sequence orders the slots within one day, 1-based. It is what makes the
	// (subscription, date, sequence) uniqueness in migration 000015 work, and it
	// is deliberately not the slot name — dispatch must not learn what
	// "breakfast" is.
	Sequence int
}

// Plan computes the deliveries that should exist in [from, to].
//
// Pure, and idempotent by construction: it describes the desired state of a
// window rather than the change to make. The caller diffs it against what
// exists. That is what lets the nightly job, a manual retry, and a partial
// failure all call the same function without coordinating — and it is why the
// unique index on (subscription, date, sequence) is a safety net rather than
// the mechanism.
//
// `from` is normally today. The caller never passes a past date, but if it
// does, past dates are still excluded: PLAN.md §4 is explicit that pauses and
// skips "only touch unmaterialized or future-materialized fulfillments — never
// past ones", and the cleanest way to guarantee that is for the planner to be
// incapable of describing the past.
func Plan(sub Subscription, rules []Rule, pauses []Pause, skips []Skip, from, to, now time.Time) []Occurrence {
	if len(rules) == 0 || sub.Suspended {
		return nil
	}

	loc := sub.Location
	if loc == nil {
		// UTC would be wrong rather than merely unset: materialising "tomorrow"
		// from a UTC clock at 21:00 in Dubai yields today's date, and the
		// customer gets a delivery they did not order and misses one they did.
		loc = time.UTC
	}

	today := dayIn(now, loc)
	start := latest(dayIn(from, loc), dayIn(sub.Start, loc), today)
	end := dayIn(to, loc)
	if !sub.End.IsZero() {
		end = earliest(end, dayIn(sub.End, loc))
	}
	if end.Before(start) {
		return nil
	}

	// Index the rules by weekday so the day loop is a map lookup rather than a
	// scan over every rule for every day in the window.
	byWeekday := make(map[Weekday][]Rule, 7)
	for _, r := range rules {
		byWeekday[r.Weekday] = append(byWeekday[r.Weekday], r)
	}
	for wd := range byWeekday {
		// Stable slot ordering, so a given day always produces the same
		// sequence numbers. Without this, two runs of the scheduler could assign
		// sequence 1 to breakfast and then to lunch, and the unique index would
		// let both land as separate fulfillments.
		sort.Slice(byWeekday[wd], func(i, j int) bool {
			return slotOrder(byWeekday[wd][i].Slot) < slotOrder(byWeekday[wd][j].Slot)
		})
	}

	var out []Occurrence
	for d := start; !d.After(end); d = d.AddDate(0, 0, 1) {
		wd := Weekday(d.Weekday())
		dayRules := byWeekday[wd]
		if len(dayRules) == 0 {
			continue
		}
		if pausedOn(pauses, d) {
			continue
		}

		seq := 0
		for _, r := range dayRules {
			seq++
			if skippedOn(skips, d, r.Slot) {
				// The sequence number is still consumed. Renumbering around a
				// skip would give Tuesday's lunch sequence 1 this week and 2
				// next week, and the unique index would then happily create a
				// duplicate for the same meal.
				continue
			}

			address := sub.AddressID
			if r.AddressID.Valid {
				address = r.AddressID.UUID
			}
			out = append(out, Occurrence{
				Date:      d,
				Slot:      r.Slot,
				WindowID:  r.WindowID,
				AddressID: address,
				Sequence:  seq,
			})
		}
	}
	return out
}

// CanModify reports whether a delivery on this date is still changeable.
//
// The cutoff is real money rather than a policy preference: past it the kitchen
// has bought the ingredients and often cooked them, so a skip is waste the
// vendor absorbs. Expressed in hours before midnight of the service date, in
// the branch's timezone.
//
// Deliberately conservative at the boundary — exactly-at-cutoff is refused. A
// customer who is one second late and gets their skip accepted, then complains
// when they are charged, is a support ticket; one who is refused sees a clear
// message.
func CanModify(serviceDate time.Time, cutoffHours int, loc *time.Location, now time.Time) bool {
	if loc == nil {
		loc = time.UTC
	}
	day := dayIn(serviceDate, loc)
	deadline := day.Add(-time.Duration(cutoffHours) * time.Hour)
	return now.In(loc).Before(deadline)
}

// PausedDays counts the delivery days a pause actually suppresses.
//
// Not the calendar length of the pause. A customer on a Sunday-to-Thursday plan
// who pauses over a weekend has lost nothing, and charging them two pause-days
// against their allowance — or crediting them for two meals they were never
// going to get — is wrong in both directions.
func PausedDays(sub Subscription, rules []Rule, p Pause, now time.Time) int {
	if len(rules) == 0 {
		return 0
	}
	loc := sub.Location
	if loc == nil {
		loc = time.UTC
	}

	weekdays := make(map[Weekday]bool, 7)
	for _, r := range rules {
		weekdays[Weekday(r.Weekday)] = true
	}

	// Only future days count. A pause backdated over meals already delivered
	// does not un-deliver them, and crediting for them would be a refund
	// dressed up as a pause.
	today := dayIn(now, loc)
	from := latest(dayIn(p.From, loc), today)
	to := dayIn(p.To, loc)

	days := 0
	for d := from; !d.After(to); d = d.AddDate(0, 0, 1) {
		if weekdays[Weekday(d.Weekday())] {
			days++
		}
	}
	return days
}

// DeliveryDaysBetween counts scheduled delivery days in a range, ignoring
// pauses and skips.
//
// This is the divisor for prepaid revenue recognition, and it is frozen when a
// cycle opens for exactly that reason: a mid-cycle pause must not retroactively
// change the daily rate applied to days already recognised.
func DeliveryDaysBetween(rules []Rule, loc *time.Location, from, to time.Time) int {
	if len(rules) == 0 {
		return 0
	}
	if loc == nil {
		loc = time.UTC
	}
	weekdays := make(map[Weekday]bool, 7)
	for _, r := range rules {
		weekdays[Weekday(r.Weekday)] = true
	}

	days := 0
	for d := dayIn(from, loc); !d.After(dayIn(to, loc)); d = d.AddDate(0, 0, 1) {
		if weekdays[Weekday(d.Weekday())] {
			days++
		}
	}
	return days
}

// --- helpers -----------------------------------------------------------------

func pausedOn(pauses []Pause, d time.Time) bool {
	for _, p := range pauses {
		if p.Inactive {
			continue
		}
		// Inclusive of both ends. "Away from the 3rd to the 7th" includes the
		// 7th; reading it exclusively delivers one meal to an empty flat every
		// single time.
		if !d.Before(dayIn(p.From, d.Location())) && !d.After(dayIn(p.To, d.Location())) {
			return true
		}
	}
	return false
}

func skippedOn(skips []Skip, d time.Time, slot string) bool {
	for _, s := range skips {
		if !dayIn(s.Date, d.Location()).Equal(d) {
			continue
		}
		// An empty slot skips the whole day. A whole-day skip and a slot-
		// specific one can both exist; the whole-day one wins, which is why
		// this returns on the first match rather than requiring an exact one.
		if s.Slot == "" || s.Slot == slot {
			return true
		}
	}
	return false
}

// dayIn truncates a timestamp to midnight in a location.
//
// The single most important function in this file. Every date in a subscription
// is a *local* date: a delivery on the 5th is on the 5th in Dubai regardless of
// what UTC thinks, and truncating in UTC shifts every evening timestamp back a
// day for the whole Gulf.
func dayIn(t time.Time, loc *time.Location) time.Time {
	if loc == nil {
		loc = time.UTC
	}
	lt := t.In(loc)
	return time.Date(lt.Year(), lt.Month(), lt.Day(), 0, 0, 0, 0, loc)
}

func latest(ts ...time.Time) time.Time {
	out := ts[0]
	for _, t := range ts[1:] {
		if t.After(out) {
			out = t
		}
	}
	return out
}

func earliest(ts ...time.Time) time.Time {
	out := ts[0]
	for _, t := range ts[1:] {
		if t.Before(out) {
			out = t
		}
	}
	return out
}

// slotOrder gives meal slots a stable chronological ordering.
//
// This is the one place in the package that knows slot names exist, and it
// knows only their order — not what they contain. A slot the engine has never
// heard of sorts last rather than being rejected, so a vertical can add one
// without a change here.
//
// suhoor and iftar sit where they fall in the day, not at the end. During
// Ramadan they *are* the schedule (PLAN.md §12 — meal subscriptions invert),
// and a sort that treats them as exotic additions produces a delivery sheet in
// the wrong order for a month.
func slotOrder(slot string) int {
	switch slot {
	case "suhoor":
		return 0
	case "breakfast":
		return 1
	case "lunch":
		return 2
	case "snack":
		return 3
	case "iftar":
		return 4
	case "dinner":
		return 5
	default:
		return 99
	}
}
