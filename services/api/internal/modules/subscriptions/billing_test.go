package subscriptions

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestRecognitionSumsExactlyToTheAmountCharged(t *testing.T) {
	// The invariant the whole file exists to hold. A per-day rate of
	// floor(amount/days) applied `days` times leaves up to days-1 minor units
	// permanently in deferred revenue with no event that will ever clear them —
	// a liability account that never reaches zero and that nobody can explain.
	//
	// The awkward amounts are the point: 44999 over 20 days is 2249.95 a day.
	for _, amount := range []int64{44_999, 100_000, 1, 7, 123_457, 99_999_991} {
		for _, days := range []int{1, 3, 7, 20, 22, 26, 30} {
			var recognised int64
			for d := 1; d <= days; d++ {
				step := RecogniseStep(amount, d, days, recognised)
				if step < 0 {
					t.Fatalf("amount=%d days=%d: negative step on day %d", amount, days, d)
				}
				recognised += step
			}
			if recognised != amount {
				t.Fatalf("amount=%d days=%d: recognised %d, want %d",
					amount, days, recognised, amount)
			}
		}
	}
}

func TestRecognitionNeverOvershoots(t *testing.T) {
	// The daily job runs more than once a day when it retries. Recognising past
	// the amount charged would credit a vendor for meals nobody paid for, and
	// the CHECK on subscription_cycles.recognised_minor would reject the write
	// — turning a rounding decision into an outage.
	const amount, days = 44_900, 20
	var recognised int64
	for d := 1; d <= days*2; d++ {
		delivered := d
		if delivered > days {
			delivered = days
		}
		recognised += RecogniseStep(amount, delivered, days, recognised)
	}
	if recognised != amount {
		t.Fatalf("re-running the recogniser produced %d, want %d", recognised, amount)
	}
}

func TestRecogniseStepIsZeroWhenNothingNewWasDelivered(t *testing.T) {
	const amount, days = 44_900, 20
	already := RecogniseThrough(amount, 5, days)
	if got := RecogniseStep(amount, 5, days, already); got != 0 {
		t.Fatalf("re-recognising the same day posted %d", got)
	}
}

func TestRecogniseStepCannotGoNegative(t *testing.T) {
	// A plan change mid-cycle can shrink the deliverable-day count. A negative
	// posting here would be a refund wearing the wrong event type, landing in
	// the wrong line of every report.
	const amount = 44_900
	already := RecogniseThrough(amount, 18, 20)
	if got := RecogniseStep(amount, 10, 12, already); got < 0 {
		t.Fatalf("a shrinking cycle produced a negative recognition: %d", got)
	}
}

func TestNothingIsRecognisedBeforeTheFirstDelivery(t *testing.T) {
	// A vendor's payable must not move the moment a customer signs up. A payout
	// run the next day would pay them for four weeks of meals they have not
	// cooked, and recovering that from a vendor who then cancels is a
	// conversation nobody wins.
	if got := RecogniseThrough(44_900, 0, 20); got != 0 {
		t.Fatalf("recognised %d before any delivery", got)
	}
}

func TestPauseCreditIsPricedAtWhatTheCustomerPaid(t *testing.T) {
	// A customer who bought on a promotion is credited what they paid, not what
	// the plan costs today. Backwards, and a discount becomes a way to extract
	// cash.
	const discounted, days = 20_000, 20
	if got := PauseCredit(discounted, 5, days, 0); got != 5_000 {
		t.Fatalf("credit = %d, want 5000 (a quarter of what was actually paid)", got)
	}
}

func TestPauseCreditCannotReachEarnedRevenue(t *testing.T) {
	// Crediting revenue already earned by delivering meals is a refund — a
	// different event, with a different approval path, that has to reverse the
	// vendor's payable rather than the platform's deferred balance.
	const amount, days = 44_900, 20
	recognised := RecogniseThrough(amount, 18, days) // only two days left unearned

	credit := PauseCredit(amount, 10, days, recognised)
	if credit > amount-recognised {
		t.Fatalf("credit %d exceeds the unearned remainder %d", credit, amount-recognised)
	}
}

func TestPauseCreditOfAFullyDeliveredCycleIsZero(t *testing.T) {
	const amount, days = 44_900, 20
	if got := PauseCredit(amount, 5, days, amount); got != 0 {
		t.Fatalf("credited %d against a fully-recognised cycle", got)
	}
}

func TestProrationIsPricedPerRemainingDay(t *testing.T) {
	// Charging the new plan's full cycle price and crediting the old one's lets
	// a customer upgrade on the last day of a cycle and get four weeks of the
	// better plan for one day of it.
	const oldPlan, newPlan, total = 40_000, 60_000, 20

	full := Proration(oldPlan, newPlan, total, total)
	if full != 20_000 {
		t.Fatalf("upgrading on day one costs %d, want the full difference 20000", full)
	}

	lastDay := Proration(oldPlan, newPlan, 1, total)
	if lastDay != 1_000 {
		t.Fatalf("upgrading with one day left costs %d, want 1000", lastDay)
	}
}

func TestDowngradingProducesACredit(t *testing.T) {
	if got := Proration(60_000, 40_000, 10, 20); got >= 0 {
		t.Fatalf("a downgrade produced %d, want a negative (credit) figure", got)
	}
}

func TestProrationOfAFinishedCycleIsZero(t *testing.T) {
	if got := Proration(40_000, 60_000, 0, 20); got != 0 {
		t.Fatalf("prorating a cycle with no days left produced %d", got)
	}
}

// --- dunning ------------------------------------------------------------------

func TestDunningRetriesFastEnoughToWarnBeforeAMissedMeal(t *testing.T) {
	// PLAN.md §6: "Meal customers must know *before* the delivery doesn't
	// arrive." A subscriber who finds out their card failed when lunch does not
	// turn up is the worst possible discovery moment, and the one that ends the
	// relationship.
	now := time.Date(2026, 8, 3, 9, 0, 0, 0, dubai)

	first, exhausted := NextAttempt(0, now)
	if exhausted {
		t.Fatal("gave up before the first retry")
	}
	if first.Sub(now) > 6*time.Hour {
		t.Fatalf("the first retry is %v away — too late to warn anyone", first.Sub(now))
	}

	second, _ := NextAttempt(1, now)
	if second.Sub(now) > 24*time.Hour {
		t.Fatalf("the second retry is %v away, past the next delivery", second.Sub(now))
	}
}

func TestDunningEventuallyGivesUp(t *testing.T) {
	now := time.Now()
	if _, exhausted := NextAttempt(len(DunningSchedule), now); !exhausted {
		t.Fatal("the retry schedule never terminates")
	}
	if _, exhausted := NextAttempt(len(DunningSchedule)-1, now); exhausted {
		t.Fatal("gave up one attempt early")
	}
}

func TestDunningBacksOff(t *testing.T) {
	now := time.Now()
	var previous time.Duration
	for i := range DunningSchedule {
		at, _ := NextAttempt(i, now)
		delay := at.Sub(now)
		if delay <= previous {
			t.Fatalf("attempt %d retries after %v, no later than attempt %d's %v",
				i, delay, i-1, previous)
		}
		previous = delay
	}
}

func TestGracePeriodKeepsDeliveringAfterTheLastAttempt(t *testing.T) {
	// Two lunches given away rather than a subscriber lost. A cycle is weeks of
	// revenue; two meals is not, and a customer whose food stopped without
	// warning does not come back.
	final := time.Date(2026, 8, 3, 9, 0, 0, 0, dubai)
	until := GraceUntil(final, dubai)

	if !until.After(final) {
		t.Fatal("the grace period ends before the final attempt")
	}
	if until.Sub(final) > 3*24*time.Hour {
		t.Fatalf("grace of %v is long enough to give away a week of meals", until.Sub(final))
	}
}

// --- cycles -------------------------------------------------------------------

func TestCyclesAreMeasuredInDaysNotCalendarMonths(t *testing.T) {
	// A "monthly" plan billed on calendar months costs a five-day-a-week
	// subscriber a different amount in February than in March for the same
	// number of meals, and every customer notices.
	first := FirstCycle(date(2026, 2, 1), 28, weekdayLunch(), dubai, 44_900, "AED")
	second := NextCycle(first, 28, weekdayLunch(), dubai, 44_900, "AED")

	if d := first.Ends.Sub(first.Starts).Hours() / 24; d != 27 {
		t.Fatalf("a 28-day cycle spans %v days end-to-end, want 27", d)
	}
	if !second.Starts.Equal(first.Ends.AddDate(0, 0, 1)) {
		t.Fatalf("cycle 2 starts %s, want the day after cycle 1 ended (%s)",
			second.Starts.Format("2006-01-02"), first.Ends.AddDate(0, 0, 1).Format("2006-01-02"))
	}
	if second.No != 2 {
		t.Fatalf("cycle number = %d, want 2", second.No)
	}
}

func TestCyclesDoNotDriftWhenAPauseExtendsThem(t *testing.T) {
	// NextCycle starts the day after the previous ended, not "start +
	// cycleDays". A pause that extends a cycle would otherwise leave a one-day
	// gap or overlap, compounding on every renewal.
	first := FirstCycle(date(2026, 8, 2), 28, weekdayLunch(), dubai, 44_900, "AED")
	extended := first
	extended.Ends = first.Ends.AddDate(0, 0, 5) // five paused days pushed it out

	next := NextCycle(extended, 28, weekdayLunch(), dubai, 44_900, "AED")
	if !next.Starts.Equal(extended.Ends.AddDate(0, 0, 1)) {
		t.Fatalf("cycle 2 starts %s, leaving a gap after the extended cycle 1 (%s)",
			next.Starts.Format("2006-01-02"), extended.Ends.Format("2006-01-02"))
	}
}

func TestACycleKnowsItsDeliverableDays(t *testing.T) {
	// The recognition divisor, frozen when the cycle opens so a mid-cycle pause
	// cannot retroactively change the daily rate applied to days already
	// recognised.
	c := FirstCycle(date(2026, 8, 2), 28, weekdayLunch(), dubai, 44_900, "AED")
	if c.DeliverableDays != 20 {
		t.Fatalf("deliverable days = %d, want 20 for four Sunday-to-Thursday weeks",
			c.DeliverableDays)
	}
}

func TestACycleWithNoDeliveriesRecognisesNothingRatherThanDividingByZero(t *testing.T) {
	// A subscription whose schedule rules were all removed. The arithmetic must
	// refuse rather than panic on the nightly job.
	_ = uuid.New()
	if got := RecogniseStep(44_900, 3, 0, 0); got != 0 {
		t.Fatalf("recognised %d against a cycle with zero delivery days", got)
	}
	if got := PauseCredit(44_900, 3, 0, 0); got != 0 {
		t.Fatalf("credited %d against a cycle with zero delivery days", got)
	}
}
