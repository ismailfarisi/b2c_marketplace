package subscriptions

import (
	"time"

	"github.com/marketplace/api/internal/money"
)

// Prepaid billing arithmetic.
//
// PLAN.md §6: "Meal plans in GCC are typically prepaid per cycle, not metered —
// bill the full cycle up front, recognize revenue daily in the ledger."
//
// Every function here is pure and returns integer minor units. The reason is
// not testability alone: subscription billing is the one place in this system
// where the same amount is divided, re-divided and partially reversed over
// weeks, and every one of those operations is an opportunity to lose or invent
// a fils. The invariant these functions exist to hold is that **the sum of
// everything recognised, credited and refunded against a cycle equals what the
// customer was charged, exactly** — not to within a rounding error.

// RecogniseThrough returns how much of a prepaid cycle should have been
// recognised as revenue after `delivered` of `total` delivery days.
//
// Computed as a cumulative floor rather than a per-day rate, and that choice is
// the whole point. A per-day amount of `floor(amount/total)` applied `total`
// times loses up to `total - 1` minor units: on a 449.00 AED cycle over 20 days
// that is 44900/20 = 2245 exactly, but on 449.99 it is 2249 × 20 = 44980,
// leaving 19 fils permanently sitting in deferred revenue with no event that
// will ever clear it. Multiplied across every subscription, that is a balance
// sheet account that never reaches zero and that nobody can explain.
//
// Taking the cumulative floor and subtracting what is already recognised puts
// the remainder on the last day, where it belongs.
//
// Phase 6 moved the arithmetic to internal/money, which is where it belonged
// once a second thing in the system — prepaid listing packages — needed the
// identical calculation. The wrapper stays so that every call site and test
// written against this name is unaffected, and so the *reason* the rule exists
// is documented next to the subscription cycle it was discovered on.
func RecogniseThrough(amountMinor int64, delivered, total int) int64 {
	return money.RecogniseThrough(amountMinor, delivered, total)
}

// RecogniseStep is the amount to post today: the cumulative target minus what
// has already been recognised.
//
// Never negative. A cycle whose deliverable-day count shrank — a plan change
// mid-cycle, or a correction — would otherwise produce a negative revenue
// posting, which is a refund wearing the wrong event type and would show up in
// the wrong line of every report.
func RecogniseStep(amountMinor int64, delivered, total int, alreadyMinor int64) int64 {
	return money.RecogniseStep(amountMinor, delivered, total, alreadyMinor)
}

// PauseCredit is what a customer is owed for delivery days they paused out of.
//
// Priced at the cycle's own daily rate rather than at the plan's list price: a
// customer who bought on a promotion is credited what they paid, not what the
// plan costs today. Getting this backwards is how a discount becomes a way to
// extract cash.
//
// Bounded by what is left unrecognised. A pause cannot credit revenue that has
// already been earned by delivering meals — that would be a refund, which is a
// different event with different accounting and a different approval path.
func PauseCredit(amountMinor int64, pausedDays, totalDays int, recognisedMinor int64) int64 {
	if totalDays <= 0 || pausedDays <= 0 || amountMinor <= 0 {
		return 0
	}
	credit := amountMinor * int64(pausedDays) / int64(totalDays)

	remaining := amountMinor - recognisedMinor
	if remaining < 0 {
		remaining = 0
	}
	if credit > remaining {
		credit = remaining
	}
	return credit
}

// Proration is the money consequence of a mid-cycle plan change.
//
// Positive means the customer owes more; negative means they are owed a credit.
// One signed number rather than two columns, because it is one number in one
// direction and two columns invite one of them being forgotten.
//
// Both sides are priced per remaining delivery day. Charging the new plan's
// full cycle price and crediting the old one's is the obvious alternative and
// it is wrong: it lets a customer upgrade on the last day of a cycle for the
// price of a full month and get four weeks of the better plan for one day of
// it.
func Proration(oldAmountMinor, newAmountMinor int64, remainingDays, totalDays int) int64 {
	if totalDays <= 0 || remainingDays <= 0 {
		return 0
	}
	if remainingDays > totalDays {
		remainingDays = totalDays
	}
	oldRemaining := oldAmountMinor * int64(remainingDays) / int64(totalDays)
	newRemaining := newAmountMinor * int64(remainingDays) / int64(totalDays)
	return newRemaining - oldRemaining
}

// --- dunning ------------------------------------------------------------------

// DunningSchedule is the retry cadence for a failed renewal.
//
// PLAN.md §6: "failed renewal → retry schedule → grace period → suspend
// fulfillments (do not delete the subscription). Meal customers must know
// *before* the delivery doesn't arrive."
//
// The last clause is the constraint that sets these numbers. A meal subscriber
// finds out their card failed when lunch does not arrive, which is the worst
// possible moment and the one that ends the relationship. So the schedule is
// front-loaded: three attempts inside the first two days, while there is still
// time for a notification to reach someone and be acted on before a delivery is
// missed.
var DunningSchedule = []time.Duration{
	4 * time.Hour,      // an expired card is often re-added within the day
	20 * time.Hour,     // next morning, a different bank cut-off
	2 * 24 * time.Hour, // after a weekend has had a chance to pass
	4 * 24 * time.Hour, // last automated try
}

// GracePeriod is how long deliveries continue after the final failed attempt.
//
// Two days of meals given away rather than a subscriber lost. That is a
// deliberate commercial choice and it is cheap: a cycle is weeks of revenue and
// two lunches is not, and a customer whose meals stopped without warning does
// not come back.
const GracePeriod = 2 * 24 * time.Hour

// NextAttempt returns when to retry a failed charge, and whether to give up.
//
// Giving up means suspending fulfillments — never deleting the subscription.
// The rules, the address, the meal history and the cycle record all survive, so
// a customer who fixes their card is resumed rather than re-onboarded.
func NextAttempt(attempts int, now time.Time) (at time.Time, exhausted bool) {
	if attempts < 0 {
		attempts = 0
	}
	if attempts >= len(DunningSchedule) {
		return time.Time{}, true
	}
	return now.Add(DunningSchedule[attempts]), false
}

// GraceUntil is the date through which a past-due subscription keeps
// delivering.
func GraceUntil(finalAttempt time.Time, loc *time.Location) time.Time {
	return dayIn(finalAttempt.Add(GracePeriod), loc)
}

// --- cycles -------------------------------------------------------------------

// Cycle is one billing period.
type Cycle struct {
	No              int
	Starts          time.Time
	Ends            time.Time
	AmountMinor     int64
	Currency        string
	DeliverableDays int
}

// NextCycle computes the billing period following `previous`.
//
// Measured in days rather than calendar months, deliberately. A "monthly" plan
// billed on calendar months costs a five-day-a-week subscriber a different
// amount in February than in March for the same number of meals, and every
// customer notices. Days are the unit the customer actually consumes.
func NextCycle(previous Cycle, cycleDays int, rules []Rule, loc *time.Location, amountMinor int64, currency string) Cycle {
	// The day after the previous cycle ended. Not "start + cycleDays", which
	// would drift by a day every time a cycle was extended by a pause.
	start := dayIn(previous.Ends, loc).AddDate(0, 0, 1)
	end := start.AddDate(0, 0, cycleDays-1)

	return Cycle{
		No:              previous.No + 1,
		Starts:          start,
		Ends:            end,
		AmountMinor:     amountMinor,
		Currency:        currency,
		DeliverableDays: DeliveryDaysBetween(rules, loc, start, end),
	}
}

// FirstCycle opens the billing period a subscription starts in.
func FirstCycle(start time.Time, cycleDays int, rules []Rule, loc *time.Location, amountMinor int64, currency string) Cycle {
	s := dayIn(start, loc)
	end := s.AddDate(0, 0, cycleDays-1)
	return Cycle{
		No:              1,
		Starts:          s,
		Ends:            end,
		AmountMinor:     amountMinor,
		Currency:        currency,
		DeliverableDays: DeliveryDaysBetween(rules, loc, s, end),
	}
}
