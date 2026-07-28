// Package interests builds per-viewer category affinity.
//
// The arithmetic lives in this file as pure functions, and the database access
// lives next door, for the same reason the ranker was split out of SQL: a
// personalisation rule that cannot be unit-tested is one that gets changed by
// guessing, and the failure mode of a wrong personalisation rule is invisible —
// the feed still returns twenty reels, they are just the wrong twenty.
//
// Two decisions shape everything here.
//
// **Profiles are keyed on viewer_key, not user_id.** Anonymous browsing is a
// supported mode; the feed is the front door and putting a login wall on it
// would be fatal. A profile that only exists for signed-in users personalises
// nothing for exactly the population whose first session decides whether they
// ever return.
//
// **Negative signal counts.** A viewer who has skipped nine laundry reels has
// told you something more useful than one who has watched none, and a model fed
// only positives converges on "this person likes everything" for every person.
package interests

import (
	"math"
	"time"

	"github.com/google/uuid"
)

// HalfLife is how fast a signal stops mattering.
//
// Two weeks. Long enough that a viewer who orders lunch weekly keeps their food
// affinity between orders; short enough that someone who spent a fortnight
// shopping for a car is not still being shown gearbox walkarounds in November.
const HalfLife = 14 * 24 * time.Hour

// Window bounds the rebuild's scan. Four half-lives — beyond that a signal
// contributes under 7% of its original weight and is not worth the rows.
const Window = 60 * 24 * time.Hour

// PerViewerCap bounds how many events one viewer contributes to a rebuild.
//
// Without it a single bot session with 200,000 events makes the batch job's
// cost unbounded, and it is exactly the viewer whose profile matters least.
const PerViewerCap = 500

// Saturation is the decayed weight at which affinity reaches roughly 0.76.
//
// It is what turns an unbounded sum into the [-1, 1] the column is constrained
// to, and it sets how much evidence "strong preference" requires. At 12, a
// viewer needs roughly two orders' worth of clicking, or a dozen watched reels,
// before a category is treated as a real preference rather than a coincidence.
const Saturation = 12.0

// Signal is one recorded interaction, already resolved to a category.
type Signal struct {
	CategoryID uuid.UUID
	Kind       string
	Age        time.Duration
}

// Weights per signal kind.
//
// Ordinal, not measured — nobody has this product's data yet. What they encode
// is a claim about cost to the viewer: a save is an intention to come back, a
// share puts their own reputation behind the reel, and a CTA press is the only
// one of these that costs a page load. A view is nearly free and is weighted
// accordingly.
//
// The reversals are symmetric on purpose. An unlike that decayed at a different
// rate from the like it reverses would leave a residue of preference the viewer
// explicitly withdrew.
var weights = map[string]float64{
	"view_progress": 1,
	"view_start":    0,
	// A skip is the most abundant signal in any feed and the only cheap negative
	// there is. Weighted above a view because skipping is a decision and
	// watching is often just not-yet-skipping.
	"skip": -1.5,

	"like":    3,
	"unlike":  -3,
	"comment": 3,
	"share":   4,
	"save":    5,
	"unsave":  -5,

	// The strongest signal available to this package. Orders are stronger still
	// and are deliberately absent: order_attributions carries no viewer_key, so
	// crediting an order to a viewer would mean guessing at the join. The click
	// that produced the order is counted here instead, which is the same
	// intent one step earlier and is recorded against the right key.
	"action_click": 6,
}

// Weight returns a signal kind's contribution. Unknown kinds contribute
// nothing rather than defaulting to positive: an event type added for
// instrumentation should not silently start steering the feed.
func Weight(kind string) float64 { return weights[kind] }

// Decay is the fraction of a signal's weight that survives to now.
func Decay(age time.Duration, halfLife time.Duration) float64 {
	if halfLife <= 0 {
		return 0
	}
	if age < 0 {
		age = 0
	}
	return math.Exp(-math.Ln2 * float64(age) / float64(halfLife))
}

// Affinity squashes an unbounded decayed sum into [-1, 1].
//
// tanh rather than a hard clamp because the top of the range should be
// approached and not hit: with a clamp, every heavy user of a category has an
// identical affinity of exactly 1.0 and the ranker loses the ability to tell
// "likes food" from "orders food four times a week".
func Affinity(raw float64) float64 {
	if math.IsNaN(raw) {
		return 0
	}
	return math.Tanh(raw / Saturation)
}

// Profile is one viewer's affinity for one category.
type Profile struct {
	CategoryID uuid.UUID
	Affinity   float64
	Positive   int64
	Negative   int64
	LastSignal time.Time
}

// Fold aggregates a viewer's signals into per-category profiles.
//
// now is passed rather than read so the function stays pure and its tests do
// not race the clock.
func Fold(signals []Signal, now time.Time) []Profile {
	type acc struct {
		raw      float64
		pos, neg int64
		youngest time.Duration
		seen     bool
	}
	byCategory := make(map[uuid.UUID]*acc)

	for _, s := range signals {
		if s.CategoryID == uuid.Nil {
			continue
		}
		w := Weight(s.Kind)
		if w == 0 {
			continue
		}

		a := byCategory[s.CategoryID]
		if a == nil {
			a = &acc{youngest: s.Age}
			byCategory[s.CategoryID] = a
		}

		a.raw += w * Decay(s.Age, HalfLife)
		if w > 0 {
			a.pos++
		} else {
			a.neg++
		}
		if !a.seen || s.Age < a.youngest {
			a.youngest, a.seen = s.Age, true
		}
	}

	out := make([]Profile, 0, len(byCategory))
	for id, a := range byCategory {
		out = append(out, Profile{
			CategoryID: id,
			Affinity:   Affinity(a.raw),
			Positive:   a.pos,
			Negative:   a.neg,
			LastSignal: now.Add(-a.youngest),
		})
	}
	return out
}
