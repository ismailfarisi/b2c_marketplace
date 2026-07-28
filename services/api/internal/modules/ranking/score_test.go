package ranking

import (
	"math"
	"testing"
	"time"
)

func fresh(f Features) Features {
	if f.Age == 0 {
		f.Age = time.Hour
	}
	if f.DistanceKM == 0 {
		f.DistanceKM = UnknownDistance
	}
	return f
}

func TestSmoothingStopsAThreeImpressionReelWinning(t *testing.T) {
	// The single most common ranking failure there is. A reel with 3 impressions
	// and 1 click has a 33% click-through rate and no information; an unsmoothed
	// ranker reads that as sixteen times better than an excellent reel and pins
	// it to the top of the feed until a human notices.
	w := DefaultWeights()

	lucky := fresh(Features{Impressions: 3, Clicks: 1, Orders: 1})
	proven := fresh(Features{
		Impressions: 5000, Views: 3200, Clicks: 150, Orders: 15,
		Likes: 250, Comments: 30, Shares: 10, Saves: 40,
	})

	if Score(lucky, w) >= Score(proven, w) {
		t.Fatalf("a 3-impression reel outranked a proven one: lucky=%.4f proven=%.4f",
			Score(lucky, w), Score(proven, w))
	}
}

func TestConversionOutranksEngagement(t *testing.T) {
	// PLAN-VIDEO-FIRST §3 sets the north star as transactions per WAU and names
	// the failure mode: watch time is the easier number to move, and a team that
	// optimises it ends up with an entertainment product that does not sell
	// anything. If this test ever fails, that has happened.
	w := DefaultWeights()

	viral := fresh(Features{
		Impressions: 10_000, Views: 9_000, Likes: 900, Comments: 200, Shares: 150, Saves: 100,
		Clicks: 200, Orders: 0,
	})
	seller := fresh(Features{
		Impressions: 10_000, Views: 4_000, Likes: 60, Comments: 5, Shares: 2, Saves: 40,
		Clicks: 400, Orders: 60,
	})

	if Score(seller, w) <= Score(viral, w) {
		t.Fatalf("an entertaining reel that sells nothing outranked one that sells: "+
			"viral=%.4f seller=%.4f", Score(viral, w), Score(seller, w))
	}
}

func TestOrdersCountOnlyVerifiedConversions(t *testing.T) {
	// Not a behavioural assertion about this function — Features.Orders is
	// populated by the feature refresh — but a guard on the contract. If the
	// refresh query ever starts counting declared or view-through attributions,
	// this comment is where the reader finds out it should not.
	//
	// What is asserted: orders move the score, so the field is load-bearing and
	// filling it with the wrong number is not harmless.
	w := DefaultWeights()
	base := fresh(Features{Impressions: 5000, Clicks: 100})
	with := base
	with.Orders = 25

	if Score(with, w) <= Score(base, w) {
		t.Fatal("orders did not move the score")
	}
}

func TestRecencyHalvesAtTheHalfLife(t *testing.T) {
	w := DefaultWeights()
	half := time.Duration(w.RecencyHalfLifeHours) * time.Hour

	atZero := recency(0, w.RecencyHalfLifeHours)
	atHalf := recency(half, w.RecencyHalfLifeHours)

	if math.Abs(atZero-1.0) > 1e-9 {
		t.Errorf("recency at publication = %v, want 1", atZero)
	}
	if math.Abs(atHalf-0.5) > 1e-9 {
		t.Errorf("recency at the half-life = %v, want 0.5", atHalf)
	}
	if recency(10*half, w.RecencyHalfLifeHours) >= 0.002 {
		t.Error("a ten-half-life-old reel is still competitive; decay is not exponential")
	}
}

func TestFutureAgeIsTreatedAsNew(t *testing.T) {
	// Scheduled publication and clock skew between writer and reader both
	// produce a negative age. The alternative to clamping is a negative
	// exponent, which ranks a skewed reel above everything ever published.
	w := DefaultWeights()
	if got := recency(-5*time.Hour, w.RecencyHalfLifeHours); got != 1.0 {
		t.Fatalf("recency for a future timestamp = %v, want 1", got)
	}
}

func TestNegativeAffinityDemotes(t *testing.T) {
	// A viewer who has skipped nine laundry reels has told you more than one who
	// has watched none. A ranker that only reads positive signal cannot act on
	// that, and every profile converges to "likes everything".
	w := DefaultWeights()
	base := fresh(Features{Impressions: 1000, Clicks: 20, Orders: 2})

	liked, disliked, neutral := base, base, base
	liked.Affinity = 0.8
	disliked.Affinity = -0.8

	if !(Score(liked, w) > Score(neutral, w) && Score(neutral, w) > Score(disliked, w)) {
		t.Fatalf("affinity is not ordered: liked=%.4f neutral=%.4f disliked=%.4f",
			Score(liked, w), Score(neutral, w), Score(disliked, w))
	}
}

func TestAffinityIsClamped(t *testing.T) {
	// The column is CHECK-constrained to [-1, 1], but the scorer is also called
	// with computed values, and one uncapped affinity would swamp every other
	// term in the score.
	w := DefaultWeights()
	base := fresh(Features{Impressions: 1000, Clicks: 20})

	huge, one := base, base
	huge.Affinity = 500
	one.Affinity = 1

	if Score(huge, w) != Score(one, w) {
		t.Fatalf("affinity was not clamped: %v vs %v", Score(huge, w), Score(one, w))
	}
}

func TestTakedownsPenaliseTheAuthorPrior(t *testing.T) {
	w := DefaultWeights()
	clean := fresh(Features{Impressions: 500, Clicks: 10, Author: AuthorPrior{
		Published: 40, Impressions: 20_000, Clicks: 400, Orders: 40,
	}})
	dirty := clean
	dirty.Author.Takedowns = 8

	if Score(dirty, w) >= Score(clean, w) {
		t.Fatal("an author with takedowns ranked no worse than one without")
	}

	// And the penalty is a rate, not a count: two takedowns out of three reels
	// is a different author from two out of four hundred.
	small := AuthorPrior{Published: 3, Impressions: 1500, Clicks: 30, Orders: 3, Takedowns: 2}
	large := AuthorPrior{Published: 400, Impressions: 200_000, Clicks: 4000, Orders: 400, Takedowns: 2}
	if authorScore(small, w) >= authorScore(large, w) {
		t.Fatal("takedowns are being counted absolutely rather than as a rate")
	}
}

func TestAuthorPenaltyIsFloored(t *testing.T) {
	// A prolific bad-faith author should rank last, not go so negative that the
	// author term swamps everything else and inverts the ranking around them.
	w := DefaultWeights()
	awful := AuthorPrior{Published: 2, Takedowns: 50, Rejected: 200}
	if got := authorScore(awful, w); got < -1 {
		t.Fatalf("author score = %v, want it floored at -1", got)
	}
}

func TestNewAuthorDoesNotStartLevelWithAProvenOne(t *testing.T) {
	// Without the author prior, the first upload from an account created this
	// morning starts level with a reel from a vendor with a year of good
	// content. That is not neutrality — it is a subsidy paid by good suppliers
	// to spammers.
	w := DefaultWeights()
	newcomer := fresh(Features{Author: AuthorPrior{Published: 1}})
	established := fresh(Features{Author: AuthorPrior{
		Published: 200, Impressions: 100_000, Clicks: 3000, Orders: 400,
	}})

	if Score(established, w) <= Score(newcomer, w) {
		t.Fatal("an established author's new reel did not outrank an unknown author's")
	}
}

func TestUnknownDistanceIsNeutralNotPenalised(t *testing.T) {
	// A reel whose origin point failed to record must not be demoted for a
	// data-collection failure — that makes a pipeline bug look like a ranking
	// decision, which is how they stay unfixed for months.
	w := DefaultWeights()
	near := proximity(0.5, w.ProximityHalfKM)
	far := proximity(50, w.ProximityHalfKM)
	unknown := proximity(UnknownDistance, w.ProximityHalfKM)

	if !(unknown > far && unknown < near) {
		t.Fatalf("unknown distance is not neutral: near=%.3f unknown=%.3f far=%.3f",
			near, unknown, far)
	}
}

func TestFollowIsAFlatBonusNotACompetition(t *testing.T) {
	// A follow is the most explicit signal a viewer ever sends. It should not
	// have to win on click-through against a stranger's viral reel.
	w := DefaultWeights()
	base := fresh(Features{Impressions: 1000, Clicks: 20})
	followed := base
	followed.Followed = true

	if diff := Score(followed, w) - Score(base, w); math.Abs(diff-w.FollowedAuthor) > 1e-9 {
		t.Fatalf("follow bonus = %v, want %v", diff, w.FollowedAuthor)
	}
}

func TestAReelWithNoSignalScoresFinitely(t *testing.T) {
	// Every reel starts here. A NaN or an Inf at this point does not produce a
	// bad ranking, it produces a sort that panics or silently reorders — and
	// this is the state of every reel in its first hour.
	w := DefaultWeights()
	s := Score(Features{Age: 0, DistanceKM: UnknownDistance}, w)
	if math.IsNaN(s) || math.IsInf(s, 0) {
		t.Fatalf("a brand-new reel scored %v", s)
	}
}

func TestNegativeCountsCannotProduceNonsense(t *testing.T) {
	// Counters are flushed from Redis, where an unlike can drive a count below
	// zero between a decrement and the next reconciliation.
	w := DefaultWeights()
	s := Score(fresh(Features{Impressions: -10, Clicks: -3, Likes: -1}), w)
	if math.IsNaN(s) || math.IsInf(s, 0) {
		t.Fatalf("negative counters produced %v", s)
	}
}

func TestBreakdownSumsToTheScore(t *testing.T) {
	// The breakdown is what answers "why is this reel at the top". If it does
	// not add up to the score, it answers it wrongly and confidently.
	w := DefaultWeights()
	f := fresh(Features{
		Impressions: 4000, Views: 2000, Clicks: 90, Orders: 8,
		Likes: 200, Comments: 30, Shares: 12, Saves: 25,
		Affinity: 0.4, Followed: true, DistanceKM: 2.5,
		Author: AuthorPrior{Published: 30, Impressions: 9000, Clicks: 180, Orders: 20},
	})

	b := Explain(f, w)
	sum := b.Conversion + b.Intent + b.Engagement + b.Author +
		b.Recency + b.Affinity + b.Followed + b.Proximity
	if math.Abs(sum-b.Total) > 1e-9 {
		t.Fatalf("breakdown sums to %v, total is %v", sum, b.Total)
	}
	if math.Abs(b.Total-Score(f, w)) > 1e-9 {
		t.Fatal("Explain and Score disagree")
	}
}

func TestLiftIsAboutOneForAnAverageReel(t *testing.T) {
	// The property that makes the weights mean what they say. Order rates run
	// near 0.2% and engagement rates near 5%; if lift were not normalised, a
	// weight of 1.0 on conversion and 0.45 on engagement would still let
	// engagement dominate by 25×, and whoever turned conversion up would watch
	// nothing happen.
	w := DefaultWeights()
	const impressions = 100_000

	conv := lift(int64(w.PriorOrderRate*impressions), impressions, w.PriorOrderRate, w.PriorEvents)
	eng := lift(int64(w.PriorEngagementRate*impressions), impressions, w.PriorEngagementRate, w.PriorEvents)

	for name, got := range map[string]float64{"conversion": conv, "engagement": eng} {
		if math.Abs(got-1.0) > 0.02 {
			t.Errorf("%s lift for an average reel = %.4f, want ~1.0", name, got)
		}
	}
}
