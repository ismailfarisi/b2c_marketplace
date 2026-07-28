// Package classifieds is the money side of the vehicles vertical.
//
// It is the first thing in this system the *platform itself* sells. Every other
// payment moves money from a customer to a vendor with the platform taking a
// commission; a listing fee moves money from a seller to the platform and no
// third party is involved. That difference is why `orders` did not fit (it
// requires a vendor, a branch and a service type), why `payments` had to be
// widened in migration 000016, and why nothing in here ever touches
// vendor_payable.
//
// SCOPE-EXTENSIONS §3 put it as "your entire money module does not apply". That
// was close but not exact, and the exact version is worth having: the *ledger*
// applies unchanged, the *commission* model does not, and `orders` turned out
// to be a commerce table rather than a billing one.
package classifieds

import (
	"time"

	"github.com/google/uuid"

	"github.com/marketplace/api/internal/money"
)

// Entitlement is a purchased package with quota left on it.
type Entitlement struct {
	ID              uuid.UUID
	ListingQuota    int
	ListingsUsed    int
	BumpQuota       int
	BumpsUsed       int
	FeaturedDays    int
	DurationDays    int
	AmountMinor     int64
	RecognisedMinor int64
	StartsAt        time.Time
	EndsAt          time.Time
}

func (e Entitlement) listingsLeft() int { return e.ListingQuota - e.ListingsUsed }
func (e Entitlement) bumpsLeft() int    { return e.BumpQuota - e.BumpsUsed }

func (e Entitlement) live(now time.Time) bool {
	return !now.Before(e.StartsAt) && now.Before(e.EndsAt)
}

// SelectForListing picks which purchase pays for the next listing.
//
// Earliest expiry first, among those with quota left. This is the only choice
// that does not quietly cost the seller money: spend the credits that are about
// to lapse, keep the ones that will still be there tomorrow. Picking the
// largest remaining balance, or the most recent purchase, both burn a
// long-dated credit while a short-dated one expires unused — and the seller
// finds out at renewal, from a number that does not match what they bought.
//
// Ties break on id so the choice is deterministic. Without that, two concurrent
// publishes could pick different purchases from the same set and the quota
// arithmetic would depend on map iteration order.
func SelectForListing(es []Entitlement, now time.Time) (Entitlement, bool) {
	return pick(es, now, func(e Entitlement) bool { return e.listingsLeft() > 0 })
}

// SelectForBump picks which purchase pays for the next bump. Same rule.
func SelectForBump(es []Entitlement, now time.Time) (Entitlement, bool) {
	return pick(es, now, func(e Entitlement) bool { return e.bumpsLeft() > 0 })
}

func pick(es []Entitlement, now time.Time, usable func(Entitlement) bool) (Entitlement, bool) {
	var best Entitlement
	found := false
	for _, e := range es {
		if !e.live(now) || !usable(e) {
			continue
		}
		switch {
		case !found:
			best, found = e, true
		case e.EndsAt.Before(best.EndsAt):
			best = e
		case e.EndsAt.Equal(best.EndsAt) && e.ID.String() < best.ID.String():
			best = e
		}
	}
	return best, found
}

// ─────────────────────────────────────────────────────────────────────────────
// Recognition
// ─────────────────────────────────────────────────────────────────────────────

// ElapsedUnits is how much of a purchase's window has passed, in whole days.
//
// Days, not hours, and clamped to the window at both ends. A purchase is sold
// as "30 days of listing" and recognising it by the hour would produce a
// different number in every report depending on when the job last ran, for no
// gain — the entitlement is not consumed by the hour either.
func ElapsedUnits(e Entitlement, now time.Time) int {
	if e.DurationDays <= 0 {
		return 0
	}
	if now.Before(e.StartsAt) {
		return 0
	}
	days := int(now.Sub(e.StartsAt).Hours() / 24)
	if days > e.DurationDays {
		days = e.DurationDays
	}
	if days < 0 {
		days = 0
	}
	return days
}

// RecogniseNow returns how much revenue to post for this purchase right now.
//
// Straight-line across the entitlement window using the same cumulative-floor
// arithmetic as a prepaid meal cycle. Identical problem, different noun: money
// taken up front, earned over a countable number of units, and the units run
// out. See internal/money/recognition.go for why the floor is cumulative.
func RecogniseNow(e Entitlement, now time.Time) int64 {
	return money.RecogniseStep(e.AmountMinor, ElapsedUnits(e, now), e.DurationDays, e.RecognisedMinor)
}

// UnearnedMinor is what a refund would owe. Straight from the ledger's point of
// view: the balance still sitting in deferred revenue for this purchase.
func UnearnedMinor(e Entitlement) int64 {
	return money.UnearnedRemainder(e.AmountMinor, e.RecognisedMinor)
}

// FeaturedUntil is when a promotion bought from this purchase should end.
//
// Bounded by the purchase's own window. A 7-day feature bought on day 28 of a
// 30-day package ends with the package, not five days after it — selling reach
// that outlives the entitlement it came from is how a promotions table starts
// disagreeing with the money that paid for it.
func FeaturedUntil(e Entitlement, from time.Time) time.Time {
	end := from.AddDate(0, 0, e.FeaturedDays)
	if end.After(e.EndsAt) {
		return e.EndsAt
	}
	return end
}
