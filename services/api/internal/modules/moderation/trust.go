package moderation

import "time"

// Author trust tiers — §3.4.
//
// The cost control, not a nicety. Human-reviewing every reel from a vendor who
// has posted 200 clean ones is waste, and the waste compounds: the queue grows,
// the SLA slips, and the reviewer starts rubber-stamping — at which point the
// queue is worse than no queue, because it looks like moderation while
// providing none.
type Tier string

const (
	TierNew       Tier = "new"
	TierProbation Tier = "probation"
	TierTrusted   Tier = "trusted"
	TierFlagged   Tier = "flagged"
)

// TrustState is the author's standing, as stored in author_trust.
type TrustState struct {
	Tier           Tier
	PublishedCount int
	StrikeCount    int
	LastStrikeAt   *time.Time
}

// StrikeWindow is how long an upheld strike keeps an author out of `trusted`.
const StrikeWindow = 90 * 24 * time.Hour

// ComputeTier derives the tier from the author's record.
//
// Derived rather than stored-and-mutated: a stored tier drifts from the counts
// that justify it the first time a backfill or a manual correction touches one
// without the other, and then nobody can say why a vendor is being reviewed.
func ComputeTier(s TrustState, now time.Time) Tier {
	// Any strike at all, ever, means flagged — the recency window governs
	// promotion back to `trusted`, not demotion. An author who violated policy
	// last week does not get the benefit of the doubt this week.
	if s.StrikeCount > 0 {
		if s.LastStrikeAt != nil && now.Sub(*s.LastStrikeAt) < StrikeWindow {
			return TierFlagged
		}
		// Strike aged out: back to probation, not straight to trusted. Earning
		// full trust again requires clean posts, not merely the passage of time.
		return TierProbation
	}

	switch {
	case s.PublishedCount == 0:
		return TierNew
	case s.PublishedCount <= 5:
		return TierProbation
	default:
		return TierTrusted
	}
}

// SampleRate is the fraction of a trusted author's clean reels that still get
// human review.
//
// Without it, `trusted` is unfalsifiable: nothing ever looks at that content
// again, so an author who changes what they post has no path back to any other
// tier until a customer reports them. Five percent is cheap and keeps the tier
// honest.
const SampleRate = 0.05

// Route decides what happens to a reel given its automated verdict and its
// author's tier.
//
// The asymmetry is deliberate: a tier can make review *stricter* than the
// automated verdict, never looser. A trusted author still gets rejected on a
// reject verdict — trust buys a faster path through clean content, not immunity.
func Route(verdict Verdict, tier Tier, sampled bool) Verdict {
	if verdict == VerdictReject {
		return VerdictReject
	}

	switch tier {
	case TierFlagged:
		// Everything from a flagged author is seen by a human before publish.
		return Stricter(verdict, VerdictReview)

	case TierNew:
		// First-ever post. This is where impersonation and outright spam land,
		// and it is the cheapest possible place to catch them.
		return Stricter(verdict, VerdictReview)

	case TierProbation:
		// Automated flags are honoured; clean passes publish.
		return verdict

	case TierTrusted:
		if verdict == VerdictApprove && sampled {
			return VerdictReview
		}
		return verdict
	}

	// Unknown tier: review. The safe direction for a value we do not recognise
	// is toward a human, never toward publish.
	return Stricter(verdict, VerdictReview)
}

// ReviewSLA is the promise attached to the queue: vendor content decided within
// two hours.
//
// It is written down here because an SLA nobody staffed is the same as no
// moderation, and the first sign of that is this constant being quietly
// unreferenced by any alert.
const ReviewSLA = 2 * time.Hour
