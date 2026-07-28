package classifieds

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

var base = time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC)

func ent(id string, quota, used, days int, endsIn time.Duration) Entitlement {
	return Entitlement{
		ID:           uuid.MustParse(id),
		ListingQuota: quota, ListingsUsed: used,
		BumpQuota: quota, BumpsUsed: used,
		DurationDays: days,
		AmountMinor:  int64(days) * 1000,
		StartsAt:     base.Add(-24 * time.Hour),
		EndsAt:       base.Add(endsIn),
	}
}

const (
	idA = "11111111-1111-1111-1111-111111111111"
	idB = "22222222-2222-2222-2222-222222222222"
	idC = "33333333-3333-3333-3333-333333333333"
)

func TestSpendsTheCreditThatExpiresSoonest(t *testing.T) {
	// The only rule that does not quietly cost the seller money. Spending the
	// long-dated credit while the short-dated one lapses is a loss the seller
	// discovers at renewal, from a number that does not match what they bought.
	es := []Entitlement{
		ent(idA, 5, 0, 30, 20*24*time.Hour),
		ent(idB, 5, 0, 30, 2*24*time.Hour), // about to lapse
		ent(idC, 5, 0, 30, 60*24*time.Hour),
	}
	got, ok := SelectForListing(es, base)
	if !ok || got.ID.String() != idB {
		t.Fatalf("picked %v, want the one expiring in two days", got.ID)
	}
}

func TestExhaustedAndExpiredCreditsAreSkipped(t *testing.T) {
	es := []Entitlement{
		// Expires soonest but has nothing left.
		ent(idA, 5, 5, 30, time.Hour),
		// Already expired.
		func() Entitlement { e := ent(idB, 5, 0, 30, -time.Hour); return e }(),
		ent(idC, 5, 2, 30, 40*24*time.Hour),
	}
	got, ok := SelectForListing(es, base)
	if !ok || got.ID.String() != idC {
		t.Fatalf("picked %v, want the only usable credit", got.ID)
	}
}

func TestNotYetStartedCreditsAreSkipped(t *testing.T) {
	e := ent(idA, 5, 0, 30, 40*24*time.Hour)
	e.StartsAt = base.Add(24 * time.Hour)
	if _, ok := SelectForListing([]Entitlement{e}, base); ok {
		t.Fatal("a purchase whose window has not opened must not be spendable")
	}
}

func TestNoUsableCreditIsAnAnswerNotAnError(t *testing.T) {
	if _, ok := SelectForListing(nil, base); ok {
		t.Fatal("an empty wallet must report false")
	}
}

func TestSelectionIsDeterministic(t *testing.T) {
	// Two credits expiring at the same instant. Without a tiebreak, two
	// concurrent publishes can pick different purchases from the same set and
	// the quota arithmetic starts depending on iteration order.
	es := []Entitlement{
		ent(idB, 5, 0, 30, 10*24*time.Hour),
		ent(idA, 5, 0, 30, 10*24*time.Hour),
	}
	first, _ := SelectForListing(es, base)
	es[0], es[1] = es[1], es[0]
	second, _ := SelectForListing(es, base)
	if first.ID != second.ID {
		t.Fatalf("selection depends on input order: %v then %v", first.ID, second.ID)
	}
}

func TestBumpsAndListingsAreSeparateQuotas(t *testing.T) {
	e := ent(idA, 3, 0, 30, 10*24*time.Hour)
	e.ListingsUsed = 3 // listings exhausted, bumps untouched
	if _, ok := SelectForListing([]Entitlement{e}, base); ok {
		t.Fatal("listing quota is exhausted")
	}
	if _, ok := SelectForBump([]Entitlement{e}, base); !ok {
		t.Fatal("bumps must not be consumed by listings")
	}
}

// ── recognition ──────────────────────────────────────────────────────────────

func TestRecognitionIsStraightLineAndLosesNothing(t *testing.T) {
	// The Phase 5 property, re-asserted for a different noun. Recognising a
	// package day by day must sum to exactly the amount charged — no fils left
	// stranded in deferred revenue, which is an account that would then never
	// reach zero.
	for _, amount := range []int64{14900, 99900, 349900, 1, 7, 100003} {
		for _, days := range []int{1, 7, 30, 31, 365} {
			e := Entitlement{
				AmountMinor:  amount,
				DurationDays: days,
				StartsAt:     base,
				EndsAt:       base.AddDate(0, 0, days),
			}
			var posted int64
			for d := 0; d <= days; d++ {
				e.RecognisedMinor = posted
				posted += RecogniseNow(e, base.AddDate(0, 0, d))
			}
			if posted != amount {
				t.Fatalf("amount %d over %d days recognised %d", amount, days, posted)
			}
		}
	}
}

func TestRecognitionNeverRunsAhead(t *testing.T) {
	e := Entitlement{AmountMinor: 30000, DurationDays: 30, StartsAt: base, EndsAt: base.AddDate(0, 0, 30)}
	// Halfway through, half the money. Recognising faster than the entitlement
	// is consumed books revenue for reach that has not been delivered.
	e.RecognisedMinor = 0
	got := RecogniseNow(e, base.AddDate(0, 0, 15))
	if got != 15000 {
		t.Fatalf("recognised %d at the halfway point, want 15000", got)
	}
}

func TestRecognitionStopsAtTheEndOfTheWindow(t *testing.T) {
	e := Entitlement{AmountMinor: 30000, DurationDays: 30, StartsAt: base,
		EndsAt: base.AddDate(0, 0, 30), RecognisedMinor: 30000}
	if got := RecogniseNow(e, base.AddDate(0, 0, 400)); got != 0 {
		t.Fatalf("recognised %d past the end of a fully recognised window", got)
	}
}

func TestRecognitionIsNeverNegative(t *testing.T) {
	// A correction that shortened the window would otherwise produce a negative
	// revenue posting — a refund wearing the wrong event type.
	e := Entitlement{AmountMinor: 30000, DurationDays: 30, StartsAt: base,
		EndsAt: base.AddDate(0, 0, 30), RecognisedMinor: 25000}
	if got := RecogniseNow(e, base.AddDate(0, 0, 3)); got != 0 {
		t.Fatalf("got %d, want 0", got)
	}
}

func TestUnearnedIsWhatARefundOwes(t *testing.T) {
	e := Entitlement{AmountMinor: 30000, RecognisedMinor: 11000}
	if got := UnearnedMinor(e); got != 19000 {
		t.Fatalf("got %d, want 19000", got)
	}
	e.RecognisedMinor = 40000 // an upstream bug
	if got := UnearnedMinor(e); got != 0 {
		t.Fatalf("got %d — an over-recognised purchase must not refund a negative", got)
	}
}

// ── promotions ───────────────────────────────────────────────────────────────

func TestAFeatureCannotOutliveThePackageThatPaidForIt(t *testing.T) {
	// Seven featured days bought on day 28 of a 30-day package end with the
	// package. Selling reach that outlives its entitlement is how the
	// promotions table starts disagreeing with the money.
	e := Entitlement{FeaturedDays: 7, StartsAt: base, EndsAt: base.AddDate(0, 0, 30)}
	got := FeaturedUntil(e, base.AddDate(0, 0, 28))
	if !got.Equal(e.EndsAt) {
		t.Fatalf("feature ends %s, package ends %s", got, e.EndsAt)
	}
}

func TestAFeatureBoughtEarlyRunsItsFullLength(t *testing.T) {
	e := Entitlement{FeaturedDays: 7, StartsAt: base, EndsAt: base.AddDate(0, 0, 30)}
	want := base.AddDate(0, 0, 7)
	if got := FeaturedUntil(e, base); !got.Equal(want) {
		t.Fatalf("got %s, want %s", got, want)
	}
}

func TestElapsedUnitsIsClampedAtBothEnds(t *testing.T) {
	e := Entitlement{DurationDays: 30, StartsAt: base, EndsAt: base.AddDate(0, 0, 30)}
	if got := ElapsedUnits(e, base.Add(-100*time.Hour)); got != 0 {
		t.Fatalf("before the window: got %d, want 0", got)
	}
	if got := ElapsedUnits(e, base.AddDate(0, 0, 900)); got != 30 {
		t.Fatalf("past the window: got %d, want 30", got)
	}
}
