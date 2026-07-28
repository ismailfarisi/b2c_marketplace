package ranking

import (
	"math"
	"time"
)

// Features is everything the scorer knows about one candidate.
//
// Counts, not rates. Every ratio in this file is derived here and nowhere else,
// which is the same rule migration 000014 applies to storage and for the same
// reason: a rate that travels without its denominator gets compared against a
// rate computed from a different one.
type Features struct {
	// Windowed signal, refreshed by the worker.
	Impressions int64
	Views       int64
	Completions int64
	Clicks      int64
	Likes       int64
	Comments    int64
	Shares      int64
	Saves       int64
	// Orders counts *verified* conversions only — attribution model
	// 'last_click'. Declared and view-through orders are legitimate numbers for
	// a vendor dashboard and the wrong training label for a ranker: a claim the
	// server could not corroborate teaches the model to optimise a fiction.
	Orders int64

	// Lifetime, for exploration eligibility.
	LifetimeImpressions int64

	// Age since publication. Passed in rather than computed from a timestamp so
	// that the scorer stays a pure function and its tests do not need a clock.
	Age time.Duration

	Author AuthorPrior

	// Affinity is the viewer's category affinity, in [-1, 1]. Zero for a viewer
	// with no history, which is the correct neutral.
	Affinity float64
	Followed bool

	// DistanceKM is the viewer-to-vendor distance. Negative means unknown, which
	// is treated as neutral rather than distant — a reel whose origin point we
	// failed to record should not be demoted for a data-collection failure.
	DistanceKM float64
}

// AuthorPrior is the reputation a new reel inherits.
type AuthorPrior struct {
	Published   int64
	Impressions int64
	Clicks      int64
	Orders      int64
	Rejected    int64
	Takedowns   int64
}

// UnknownDistance marks a candidate whose distance could not be computed.
const UnknownDistance = -1.0

// Breakdown is a score with its terms itemised.
//
// It exists because "why is this reel at the top" is asked constantly — by the
// vendor whose reel is not, by whoever is tuning weights, and by anyone
// debugging a feed that looks wrong. Answering it by reading the weights and
// doing arithmetic on a napkin is how ranking teams lose days.
type Breakdown struct {
	Conversion float64
	Intent     float64
	Engagement float64
	Author     float64
	Recency    float64
	Affinity   float64
	Followed   float64
	Proximity  float64
	Total      float64
}

// Score ranks one candidate.
//
// Higher is better; the absolute value means nothing and only the ordering is
// used. The result is always finite — a candidate with no signal at all scores
// on its priors, which is the entire point of having them.
func Score(f Features, w Weights) float64 { return Explain(f, w).Total }

// Explain is Score with the terms kept.
func Explain(f Features, w Weights) Breakdown {
	m := w.PriorEvents

	// Lift over baseline, not the raw rate.
	//
	// Order rates run near 0.2% and engagement rates near 5%. Combining raw
	// rates linearly would let engagement dominate at any weight you chose,
	// and the person turning `conversion` up from 1.0 to 1.5 would watch
	// nothing happen and conclude the signal was worthless. Dividing each
	// smoothed rate by its own baseline puts every term at ~1.0 for an average
	// reel, so a weight of 1.0 genuinely means "as important as the others".
	convLift := lift(f.Orders, f.Impressions, w.PriorOrderRate, m)
	clickLift := lift(f.Clicks, f.Impressions, w.PriorClickRate, m)
	engLift := lift(engagementPoints(f), f.Impressions, w.PriorEngagementRate, m)

	b := Breakdown{
		Conversion: w.Conversion * convLift,
		Intent:     w.Intent * clickLift,
		Engagement: w.Engagement * engLift,
		Author:     w.AuthorQuality * authorScore(f.Author, w),
		Recency:    w.Recency * recency(f.Age, w.RecencyHalfLifeHours),
		Affinity:   w.Affinity * clamp(f.Affinity, -1, 1),
		Proximity:  w.Proximity * proximity(f.DistanceKM, w.ProximityHalfKM),
	}
	if f.Followed {
		b.Followed = w.FollowedAuthor
	}

	b.Total = b.Conversion + b.Intent + b.Engagement + b.Author +
		b.Recency + b.Affinity + b.Followed + b.Proximity
	return b
}

// lift is a Bayesian-smoothed rate divided by its own prior.
//
// The smoothing is what stops the most common ranking failure there is: a reel
// with 3 impressions and 1 click has a 33% click-through rate, an unsmoothed
// ranker reads that as sixteen times better than a genuinely excellent reel,
// and it sits at the top of the feed until someone notices.
//
// priorEvents is a count of *events*, not of impressions, and the conversion to
// impressions happens here: believing the baseline as strongly as `priorEvents`
// observations means priorEvents/prior impressions' worth of belief. For orders
// at a 0.2% baseline that is 2,500 impressions; for engagement at 5% it is 100.
// Stating the prior in impressions instead would give the order rate a
// pseudo-count of 0.4 orders, which one real order overwhelms — and that is
// precisely the reel this function exists to hold back.
//
// So the 3-impression, 1-click reel scores (1 + 5) / (3 + 250) = 2.4% against a
// 2% baseline: barely above average, which is exactly what three impressions
// justify believing.
func lift(events, impressions int64, prior, priorEvents float64) float64 {
	if prior <= 0 || priorEvents <= 0 {
		return 0
	}
	if impressions < 0 {
		impressions = 0
	}
	if events < 0 {
		events = 0
	}
	priorImpressions := priorEvents / prior
	smoothed := (float64(events) + priorEvents) / (float64(impressions) + priorImpressions)
	return smoothed / prior
}

// engagementPoints collapses four interactions into one number.
//
// The weights are ordinal rather than measured, and they encode a claim worth
// stating: a save or a share is a stronger signal than a like, because both
// cost the viewer something. A like is a thumb twitch. A share puts the
// viewer's own reputation behind the reel, and a save is an intention to come
// back — which in a marketplace is intent to buy, deferred.
func engagementPoints(f Features) int64 {
	return f.Likes + 2*f.Comments + 3*f.Shares + 3*f.Saves
}

// recency is exponential decay, 1.0 at publication and 0.5 at the half-life.
//
// Exponential rather than the Phase 2 `1/(1+age)` hyperbola because the tail
// matters: a hyperbola keeps a 60-day-old reel at a twentieth of a fresh one's
// score, which sounds small until the catalogue has ten thousand old reels and
// twenty new ones. Exponential decay puts it at 2^-20 and settles the question.
func recency(age time.Duration, halfLifeHours float64) float64 {
	if halfLifeHours <= 0 {
		return 0
	}
	hours := age.Hours()
	if hours < 0 {
		// Scheduled publication, or clock skew between writer and reader.
		// Treated as brand new rather than as an error: a reel cannot be more
		// recent than now, and the alternative is a negative exponent.
		hours = 0
	}
	return math.Exp(-math.Ln2 * hours / halfLifeHours)
}

// proximity breaks ties between vendors who can all serve the address.
//
// Deliverability is already a hard filter — PLAN-VIDEO-FIRST §3 is explicit
// that a vendor who cannot deliver to you is noise regardless of distance — so
// this term is not doing eligibility work. It is expressing that among two
// vendors who can both reach you, the nearer one probably reaches you sooner.
func proximity(distanceKM, halfKM float64) float64 {
	if distanceKM < 0 || math.IsNaN(distanceKM) {
		// Unknown. Neutral, not distant. Demoting a reel because its origin
		// point failed to record would make a data-collection bug look like a
		// ranking decision.
		return 0.5
	}
	if halfKM <= 0 {
		return 0
	}
	return 1.0 / (1.0 + distanceKM/halfKM)
}

// authorScore is the prior a reel inherits before it has signal of its own.
//
// Without it, the first upload from an account created this morning starts
// level with a reel from a vendor with a year of good content. That is not
// neutrality — it is a subsidy paid by good suppliers to spammers, and it is
// the exact gap content farms are built to exploit.
func authorScore(a AuthorPrior, w Weights) float64 {
	base := lift(a.Orders, a.Impressions, w.PriorOrderRate, w.PriorEvents)

	// Penalties are rates over what the author actually published, so an author
	// with two takedowns out of three reels is punished far harder than one with
	// two out of four hundred.
	published := float64(a.Published)
	if published < 1 {
		published = 1
	}
	base -= w.TakedownPenalty * float64(a.Takedowns) / published
	base -= w.RejectPenalty * float64(a.Rejected) / published

	// Floored rather than allowed to go arbitrarily negative. A prolific
	// bad-faith author should rank last, not swamp every other term in the
	// score and invert the ranking of everything around them.
	return math.Max(base, -1)
}

func clamp(v, lo, hi float64) float64 {
	if math.IsNaN(v) {
		return 0
	}
	return math.Min(math.Max(v, lo), hi)
}
