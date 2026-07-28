// Package attribution decides which reel, if any, produced an order.
//
// This is the measurement the whole video bet rests on. PLAN-VIDEO-FIRST §8:
// "Instrument conversion honestly before building ranking" — and §9 puts the
// reason plainly, that egress is paid for on every view whether it converts or
// not. Phase 4 trains a ranking model on these numbers. A number that flatters
// the feed does not merely mislead a dashboard; it teaches the model to
// optimise for a fiction, and the error compounds from there.
//
// So the design goal here is not "attribute as much as possible". It is
// "attribute defensibly, and make the shape of the doubt visible". Three
// choices follow from that, and each costs the feed credit it might otherwise
// have claimed:
//
//   - A client's claim is not evidence. The order body may name a reel; the
//     server independently checks whether it saw a click. When they disagree,
//     the disagreement is recorded rather than resolved in the feed's favour.
//   - View-through is never folded into click-through. Counting "saw the reel,
//     ordered later, never pressed anything" as conversion is the single
//     easiest way to make a feed look like it works.
//   - One order attributes to at most one reel. Enforced by a primary key, not
//     by convention, because the alternative sums to more revenue than the
//     business made.
package attribution

import (
	"time"

	"github.com/google/uuid"
)

// Model is the rule that produced an attribution. Stored on every row, because
// the rules will change and a historical number is only interpretable under the
// rule that actually produced it.
type Model string

const (
	// ModelLastClick — the server recorded a CTA press by this viewer inside
	// the window. The only model that is evidence rather than inference.
	ModelLastClick Model = "last_click"

	// ModelDeclared — the client named a reel and the server saw no click.
	// Kept because it is a real signal about client behaviour (deep links,
	// share links, a click the server dropped), and reported separately
	// because it is unverified.
	ModelDeclared Model = "declared"

	// ModelViewThrough — the viewer was served the reel and ordered from the
	// same target without ever pressing the CTA. The weakest claim, and the one
	// most often quoted as if it were the strongest.
	ModelViewThrough Model = "view_through"

	// ModelNone — organic. Browse, search, a repeat order, a phone call.
	ModelNone Model = "none"
)

// ClickWindow is how long a click can precede an order and still be credited.
//
// Twenty-four hours. The reasoning, since a window is always arbitrary and the
// arbitrariness should at least be argued: laundry and meals are same-day or
// next-day decisions, so a viewer who saw a reel on Monday and books on Friday
// has almost certainly been re-reached by something else. A 7-day window would
// roughly double attributed conversions without doubling anything real, and
// that inflated number is precisely what Phase 4 would learn from.
//
// This is a starting point to validate against the observed latency
// distribution, which is why every attribution stores its own latency: if the
// mass sits under an hour, the window is far too generous and should tighten.
const ClickWindow = 24 * time.Hour

// ViewThroughWindow is deliberately shorter than ClickWindow.
//
// A click is a stated intent that survives a night's sleep. A view is not: the
// longer the gap, the more likely the order would have happened regardless, and
// view-through credit is exactly the credit that should be hardest to earn.
const ViewThroughWindow = 4 * time.Hour

// Click is a recorded CTA press.
type Click struct {
	ID         int64
	ActionID   uuid.UUID
	ReelID     uuid.UUID
	TargetType string
	TargetID   uuid.UUID
	Surface    string
	ClickedAt  time.Time
	WatchedMS  int
}

// Impression is a reel having been served to this viewer.
type Impression struct {
	ReelID     uuid.UUID
	TargetType string
	TargetID   uuid.UUID
	Surface    string
	ServedAt   time.Time
}

// Order is what is being attributed. Deliberately minimal: attribution has no
// business knowing about lines, quotes or capacity.
type Order struct {
	ID       uuid.UUID
	PlacedAt time.Time
	GMVMinor int64
	Currency string
	BranchID uuid.UUID
	VendorID uuid.UUID
	Targets  []Target
	// DeclaredReelID is what the client claimed. A claim, not evidence.
	DeclaredReelID   *uuid.UUID
	DeclaredActionID *uuid.UUID
}

// Target is one thing the order actually bought, for matching against what a
// reel pointed at.
type Target struct {
	Type string
	ID   uuid.UUID
}

// Evidence is everything the resolver may consider.
type Evidence struct {
	// Clicks by this viewer, most recent first.
	Clicks []Click
	// Impressions served to this viewer, most recent first.
	Impressions []Impression
	// AuthoredByOrderedVendor reports, per reel ID, whether the reel's author
	// is the vendor being ordered from. Always true in Phase 2's vendor-only
	// world; the field exists because Phase 6 adds user-authored reels, and a
	// reel by a third party pointing at a vendor is a different claim.
	AuthoredByOrderedVendor map[uuid.UUID]bool
}

// Result is the decision, plus the reasoning behind it.
type Result struct {
	Model    Model
	ReelID   *uuid.UUID
	ActionID *uuid.UUID
	ClickID  *int64
	Latency  time.Duration
	OffFeed  bool
	GMVMinor int64
	Currency string
	// Reason is stored on nothing and shown to nobody by default. It exists so
	// that when a vendor disputes a number — and they will, because the number
	// determines what they think the platform is worth — there is an answer
	// better than "the query said so".
	Reason string
}

// Resolve decides.
//
// Precedence: last-click, then declared, then view-through, then none. Each
// step is strictly weaker evidence than the one above it, and the first that
// matches wins — so a verifiable click always beats an unverifiable claim, and
// an unverifiable claim always beats an inference.
func Resolve(o Order, e Evidence, now time.Time) Result {
	res := Result{
		Model:    ModelNone,
		GMVMinor: o.GMVMinor,
		Currency: o.Currency,
		Reason:   "no click or impression matched this order within the window",
	}

	// --- 1. last click ------------------------------------------------------
	//
	// Requires all three: inside the window, pointing at something this order
	// actually bought, and before the order was placed. The middle condition is
	// the one that is easy to omit and expensive to omit — without it, a viewer
	// who clicks a laundry CTA and then orders a completely different vendor's
	// meal plan credits the laundry reel.
	for _, c := range o.clicksInWindow(e.Clicks, ClickWindow, now) {
		if !o.matchesTarget(c.TargetType, c.TargetID) {
			continue
		}
		res.Model = ModelLastClick
		res.ReelID = &c.ReelID
		res.ActionID = &c.ActionID
		id := c.ID
		res.ClickID = &id
		res.Latency = o.PlacedAt.Sub(c.ClickedAt)
		res.OffFeed = isOffFeed(c.Surface)
		res.Reason = "server-recorded CTA press on this target within the click window"
		return res
	}

	// --- 2. declared --------------------------------------------------------
	//
	// The client says a reel sent them and the server has no click to show for
	// it. Legitimate causes exist — a share link opened outside the app, a
	// click event lost on a flaky network — so it is recorded rather than
	// discarded. It is never merged into last_click, because the whole point of
	// separating them is that one is checkable and one is not.
	if o.DeclaredReelID != nil {
		res.Model = ModelDeclared
		res.ReelID = o.DeclaredReelID
		res.ActionID = o.DeclaredActionID
		// Off-feed by assumption: the ordinary in-feed path produces a click,
		// so a declaration with no click most likely arrived by a shared link.
		// Assuming the flattering alternative would put un-clicked orders into
		// the feed's headline number.
		res.OffFeed = true
		res.Reason = "client declared a source reel; no server-recorded click corroborates it"
		return res
	}

	// --- 3. view-through ----------------------------------------------------
	//
	// The viewer was shown a reel for something they then bought, and never
	// pressed the CTA. This is the weakest claim in the system and it is
	// reported on its own line for that reason: a feed whose value rests
	// entirely on view-through has not been shown to work.
	for _, imp := range e.Impressions {
		if imp.ServedAt.After(o.PlacedAt) {
			continue
		}
		if o.PlacedAt.Sub(imp.ServedAt) > ViewThroughWindow {
			continue
		}
		if !o.matchesTarget(imp.TargetType, imp.TargetID) {
			continue
		}
		res.Model = ModelViewThrough
		reelID := imp.ReelID
		res.ReelID = &reelID
		res.Latency = o.PlacedAt.Sub(imp.ServedAt)
		res.OffFeed = isOffFeed(imp.Surface)
		res.Reason = "reel for this target was served to the viewer, but no CTA was pressed"
		return res
	}

	return res
}

// clicksInWindow filters to clicks that could plausibly have caused this order,
// newest first.
func (o Order) clicksInWindow(clicks []Click, window time.Duration, _ time.Time) []Click {
	out := make([]Click, 0, len(clicks))
	for _, c := range clicks {
		// A click after the order did not cause it. This is not hypothetical:
		// a client that batches events can deliver a click whose timestamp
		// lands after the order it followed.
		if c.ClickedAt.After(o.PlacedAt) {
			continue
		}
		if o.PlacedAt.Sub(c.ClickedAt) > window {
			continue
		}
		out = append(out, c)
	}
	return out
}

// matchesTarget reports whether a CTA's destination is something this order
// actually bought.
//
// Branch and vendor targets match at the vendor level: a `follow` or `open_menu`
// CTA points at a branch rather than an offering, and an order from that branch
// is the outcome it was driving toward.
func (o Order) matchesTarget(targetType string, targetID uuid.UUID) bool {
	switch targetType {
	case "vendor_branch":
		return targetID == o.BranchID
	case "vendor":
		return targetID == o.VendorID
	case "url":
		// An external link cannot be corroborated against anything in this
		// system. Refusing to match it is the honest answer; claiming it would
		// credit the feed for traffic it cannot see the outcome of.
		return false
	}

	for _, t := range o.Targets {
		if t.Type == targetType && t.ID == targetID {
			return true
		}
	}
	return false
}

// isOffFeed separates feed-driven traffic from everything else.
//
// A vendor pushing their own reel to a WhatsApp group converts at rates the
// feed will never approach. That is a real and useful marketing channel; it is
// not evidence that the feed works, and letting it into the headline number is
// how a team convinces itself to keep funding a feed nobody uses.
func isOffFeed(surface string) bool {
	switch surface {
	case "feed", "search":
		return false
	default:
		return true
	}
}

// Verifiable reports whether a model rests on something the server observed.
//
// The distinction the funnel is built around: verifiable models can be quoted
// as conversion; the others are reported beside it with their names attached.
func (m Model) Verifiable() bool { return m == ModelLastClick }

// Attributed reports whether any reel got credit at all, under any model.
func (m Model) Attributed() bool { return m != ModelNone }
