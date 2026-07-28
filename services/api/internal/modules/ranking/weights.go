// Package ranking scores feed candidates.
//
// Everything in this package is a pure function of a Features struct and a
// Weights struct. That is the whole design, and it is deliberate: the Phase 2
// feed scored in SQL, which was correct while the score was `1/(1+age)` and
// stops being correct the moment the score has eight terms, Bayesian smoothing
// and an author prior. A scoring expression buried in a 60-line query is one
// nobody can unit-test, and an untestable ranker is one that gets tuned by
// guessing.
//
// So the feed became two stages — retrieval in SQL, ranking in Go. Retrieval
// answers "what is eligible", which is a database question. Ranking answers
// "what is best", which is a modelling question, and it lives here where a test
// can assert that a reel with three impressions does not outrank one with five
// thousand.
package ranking

import (
	"encoding/json"
	"fmt"
	"math"
)

// Weights is one ranking configuration.
//
// Every field is a named number with a stated unit, because the alternative —
// a []float64 indexed by position — produces the specific bug where someone
// inserts a signal in the middle and every weight after it silently means
// something else.
type Weights struct {
	// ── the four score terms that come from behaviour ────────────────────────
	//
	// Each multiplies a *lift over baseline*, not a raw rate. Order rates run
	// near 0.2% and engagement rates near 5%; a linear combination of raw rates
	// would let engagement dominate at any weight you chose, and you would spend
	// a week wondering why turning conversion up did nothing.

	// Conversion is verified orders per impression. It carries the largest
	// weight by design — PLAN-VIDEO-FIRST §3 sets the north star as transactions
	// per weekly active user and names the failure mode explicitly: watch time
	// is the easier number to move, and a team that optimises it ends up with an
	// entertainment product that does not sell anything.
	Conversion float64 `json:"conversion"`
	// Intent is CTA presses per impression. Denser than orders by roughly two
	// orders of magnitude, which is why it is here at all: at launch almost
	// every reel has zero orders and conversion alone would carry no signal.
	Intent float64 `json:"intent"`
	// Engagement is the weighted like/comment/share/save rate.
	Engagement float64 `json:"engagement"`
	// AuthorQuality is the prior a new reel inherits from its author.
	AuthorQuality float64 `json:"author_quality"`

	// ── the terms that come from context ─────────────────────────────────────

	// Recency is exponential decay on age, in [0, 1].
	Recency float64 `json:"recency"`
	// Affinity multiplies the viewer's category affinity, which ranges [-1, 1].
	// The negative half matters: a viewer who has skipped nine laundry reels has
	// told you more than one who has watched none.
	Affinity float64 `json:"affinity"`
	// FollowedAuthor is a flat bonus. A follow is the most explicit signal a
	// viewer ever sends and it should not have to compete on click-through.
	FollowedAuthor float64 `json:"followed_author"`
	// Proximity rewards nearness *within* the eligible set. Deliverability is
	// already a hard filter; this only breaks ties between vendors who can all
	// serve the address.
	Proximity float64 `json:"proximity"`

	// ── shape parameters ─────────────────────────────────────────────────────

	RecencyHalfLifeHours float64 `json:"recency_half_life_hours"`
	ProximityHalfKM      float64 `json:"proximity_half_km"`

	// PriorEvents is the Bayesian prior strength, expressed in **events** rather
	// than in impressions.
	//
	// This distinction is the difference between smoothing that works and
	// smoothing that looks like it does. Stated as impressions — "believe the
	// baseline as strongly as 200 impressions" — the pseudo-count for a rare
	// event is 200 × 0.002 = 0.4 orders, so a single real order swamps the prior
	// and a reel with one order in three impressions ranks above a reel with
	// fifteen in five thousand. The scale of the smoothing has to follow the
	// rarity of the event, and stating it in events is what makes it do so
	// automatically: at 5, the order prior is worth 2,500 impressions and the
	// engagement prior 100, which is exactly the asymmetry the evidence
	// requires.
	PriorEvents float64 `json:"prior_events"`

	// The baselines. These are also the divisors that turn each rate into a
	// lift, so getting them roughly right matters more than getting them
	// precisely right: a term whose baseline is 10× off is a term whose weight
	// means 10× what it says.
	PriorClickRate      float64 `json:"prior_click_rate"`
	PriorOrderRate      float64 `json:"prior_order_rate"`
	PriorEngagementRate float64 `json:"prior_engagement_rate"`

	// Author penalties, applied as rates over that author's published count.
	// Takedowns are punished harder than rejections: a rejection means the gate
	// worked, a takedown means something got past it and was seen.
	TakedownPenalty float64 `json:"takedown_penalty"`
	RejectPenalty   float64 `json:"reject_penalty"`

	// ── exploration ──────────────────────────────────────────────────────────

	// ExplorationShare is the fraction of every page reserved for reels that
	// have not had a fair hearing. PLAN-VIDEO-FIRST §3 puts it at 10–15% and
	// states the consequence of zero plainly: new vendors never get impressions
	// and the supply side churns. That is not a ranking inefficiency, it is the
	// marketplace dying from the supply end while every dashboard looks healthy.
	ExplorationShare float64 `json:"exploration_share"`
	// ExplorationImpressionFloor is what "has not had a fair hearing" means,
	// counted over the reel's lifetime rather than the feature window. A rolling
	// window would make a reel eligible for exploration again every time the
	// window rolled past its launch, which is a subsidy with no end.
	ExplorationImpressionFloor int64 `json:"exploration_impression_floor"`
}

// DefaultWeights is what ranks the feed when the database has no active
// profile, or has one that fails validation.
//
// It exists so that a missing config row degrades ranking to "last known good"
// rather than taking the front door down. Ranking that is slightly stale is a
// product that works; ranking that refuses to load is a blank screen.
//
// These values match the `phase-4-heuristic` row seeded in migration 000014.
// They are heuristics and they are guesses — honest ones, but nobody has served
// a page with them yet. PLAN-VIDEO-FIRST §11 is blunt that heuristic ranking
// buys about six months.
func DefaultWeights() Weights {
	return Weights{
		Conversion:    1.00,
		Intent:        0.60,
		Engagement:    0.45,
		AuthorQuality: 0.25,

		Recency:        0.35,
		Affinity:       0.40,
		FollowedAuthor: 0.30,
		Proximity:      0.15,

		RecencyHalfLifeHours: 72,
		ProximityHalfKM:      5,

		PriorEvents: 5,

		PriorClickRate:      0.02,
		PriorOrderRate:      0.002,
		PriorEngagementRate: 0.05,

		TakedownPenalty: 2.0,
		RejectPenalty:   0.5,

		ExplorationShare:           0.15,
		ExplorationImpressionFloor: 500,
	}
}

// Validate rejects a configuration that would produce nonsense.
//
// Called on everything loaded from the database, because ranking_profiles is
// edited by hand by whoever is tuning this week, and a typo'd zero in
// smoothing_impressions turns the feed over to whichever reel got lucky on its
// third impression. A bad profile must be refused loudly, not applied quietly.
func (w Weights) Validate() error {
	var problems []string

	finite := func(name string, v float64) {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			problems = append(problems, name+" is not a finite number")
		}
	}
	positive := func(name string, v float64) {
		finite(name, v)
		if v <= 0 {
			problems = append(problems, fmt.Sprintf("%s must be > 0, got %v", name, v))
		}
	}
	rate := func(name string, v float64) {
		finite(name, v)
		if v <= 0 || v >= 1 {
			problems = append(problems, fmt.Sprintf("%s must be a rate in (0,1), got %v", name, v))
		}
	}

	for name, v := range map[string]float64{
		"conversion": w.Conversion, "intent": w.Intent, "engagement": w.Engagement,
		"author_quality": w.AuthorQuality, "recency": w.Recency,
		"affinity": w.Affinity, "followed_author": w.FollowedAuthor,
		"proximity": w.Proximity, "takedown_penalty": w.TakedownPenalty,
		"reject_penalty": w.RejectPenalty,
	} {
		finite(name, v)
	}

	positive("recency_half_life_hours", w.RecencyHalfLifeHours)
	positive("proximity_half_km", w.ProximityHalfKM)

	// Below 1 the "prior" is weaker than a single observation, which is the same
	// as having no prior at all — and no prior is the failure this whole
	// mechanism exists to prevent.
	if math.IsNaN(w.PriorEvents) || w.PriorEvents < 1 {
		problems = append(problems,
			fmt.Sprintf("prior_events must be >= 1, got %v", w.PriorEvents))
	}

	rate("prior_click_rate", w.PriorClickRate)
	rate("prior_order_rate", w.PriorOrderRate)
	rate("prior_engagement_rate", w.PriorEngagementRate)

	// The upper bound is not arbitrary. Past half the page, "exploration" is no
	// longer a slice reserved for new supply — it is the ranking, and the
	// ranking is the thing being explored around.
	if math.IsNaN(w.ExplorationShare) || w.ExplorationShare < 0 || w.ExplorationShare > 0.5 {
		problems = append(problems,
			fmt.Sprintf("exploration_share must be in [0, 0.5], got %v", w.ExplorationShare))
	}
	if w.ExplorationImpressionFloor < 0 {
		problems = append(problems, "exploration_impression_floor must be >= 0")
	}

	if len(problems) > 0 {
		return fmt.Errorf("ranking: invalid weights: %v", problems)
	}
	return nil
}

// ParseWeights decodes a stored profile.
//
// Missing keys inherit the compiled-in default rather than zeroing. A profile
// written before a new signal existed must keep working after the deploy that
// adds it — the alternative is that shipping a new term silently sets its
// weight to zero everywhere, which is a ranking change nobody made and nobody
// reviewed.
func ParseWeights(raw []byte) (Weights, error) {
	w := DefaultWeights()
	if len(raw) == 0 {
		return w, nil
	}
	if err := json.Unmarshal(raw, &w); err != nil {
		return DefaultWeights(), fmt.Errorf("ranking: parse weights: %w", err)
	}
	if err := w.Validate(); err != nil {
		return DefaultWeights(), err
	}
	return w, nil
}
