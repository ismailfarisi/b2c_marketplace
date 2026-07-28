package money

// Straight-line revenue recognition against a prepaid entitlement.
//
// This arithmetic arrived in Phase 5 as `subscriptions.RecogniseThrough`, and
// Phase 6 found it to be the one piece of that phase which is genuinely
// vertical-neutral. A prepaid meal cycle recognised across its delivery days
// and a prepaid listing package recognised across its 30-day window are the
// same problem with different nouns: money taken up front, earned over a
// countable number of units, and the units run out.
//
// It lives here rather than being imported across modules because a classifieds
// package spending `subscriptions.RecogniseThrough` would encode a dependency
// that does not exist — nothing about a dealer package is a subscription in the
// meals sense. `subscriptions` now delegates to these functions and keeps its
// own wrappers, so Phase 5's call sites and tests are untouched.

// RecogniseThrough returns how much of a prepaid amount should have been
// recognised as revenue after `delivered` of `total` units.
//
// A cumulative floor, not a per-unit rate, and that choice is the entire point.
// A per-unit amount of floor(amount/total) applied `total` times loses up to
// `total - 1` minor units: on 449.99 over 20 units that is 2249 × 20 = 44,980,
// leaving 19 fils permanently in deferred revenue with no event that will ever
// clear it. Multiplied across every customer, that is a balance-sheet account
// which never reaches zero and which nobody can explain.
//
// Taking the cumulative floor and subtracting what is already recognised puts
// the remainder on the last unit, where it belongs.
func RecogniseThrough(amountMinor int64, delivered, total int) int64 {
	if total <= 0 || amountMinor <= 0 || delivered <= 0 {
		return 0
	}
	if delivered >= total {
		return amountMinor
	}
	return amountMinor * int64(delivered) / int64(total)
}

// RecogniseStep is the amount to post now: the cumulative target minus what has
// already been recognised.
//
// Never negative. An entitlement whose unit count shrank — a plan change, a
// correction — would otherwise produce a negative revenue posting, which is a
// refund wearing the wrong event type and lands in the wrong line of every
// report.
func RecogniseStep(amountMinor int64, delivered, total int, alreadyMinor int64) int64 {
	target := RecogniseThrough(amountMinor, delivered, total)
	if target <= alreadyMinor {
		return 0
	}
	return target - alreadyMinor
}

// UnearnedRemainder is what is still owed back if the entitlement ends now.
//
// The counterpart to RecogniseThrough and the number a cancellation refunds.
// Floored at zero: a recognised total above the amount charged is a bug
// upstream, and returning a negative refund would turn that bug into a charge.
func UnearnedRemainder(amountMinor, recognisedMinor int64) int64 {
	if recognisedMinor >= amountMinor {
		return 0
	}
	if recognisedMinor < 0 {
		return amountMinor
	}
	return amountMinor - recognisedMinor
}
