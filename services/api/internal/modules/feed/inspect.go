package feed

import (
	"context"
	"sort"

	"github.com/google/uuid"

	"github.com/marketplace/api/internal/modules/ranking"
)

// The feed inspector — PLAN-ADMIN-CONSOLE §4.4.
//
// Answers "the feed looked wrong at 14:20" by running the real retrieval for a
// point and a viewer and showing the score *with its terms kept*. Without it the
// only available answers are "the ranker is working as designed" and a
// re-implementation of the ranker in a spreadsheet, and the second one is always
// subtly different from the first.
//
// It lives in this package rather than in `adminview` for the reason §3.2 gives
// about writes and which applies equally to reads that must be *identical*:
// candidate generation is thirty lines of SQL with five union branches and a
// deliberate set of hard filters, and a copy of it in a console package would
// diverge on the first change to either. The inspector calls the same
// `retrieve` the feed calls. If it did not, it would be inspecting a different
// feed and would be worse than nothing — it would be confidently wrong.
//
// Deliberately **not** exposed with the exploration interleave applied. An
// exploration slot is decided by a seeded shuffle of the viewer key and page
// number, so a reel that appeared at position 4 for a viewer will not
// necessarily appear there when an operator looks. Showing the ranked order and
// naming exploration separately is honest; showing a reconstructed page and
// implying it is the page the viewer saw is not.

// InspectedCandidate is one reel with the arithmetic that placed it.
type InspectedCandidate struct {
	ReelID   uuid.UUID
	Caption  string
	Author   string
	Rank     int
	Score    float64
	Terms    ranking.Breakdown
	Features ranking.Features
	// ExploreEligible marks a reel below the lifetime-impression floor. Such a
	// reel can be interleaved into a page regardless of its score, which is the
	// usual explanation for "why is this old thing at position 4" — and it is
	// invisible in the score, because by construction it does not have one worth
	// winning on.
	ExploreEligible bool
}

// Inspection is the whole answer for one point and viewer.
type Inspection struct {
	Profile    string
	Weights    ranking.Weights
	Candidates []InspectedCandidate
	// Retrieved is the count *before* the page limit. A page of ten drawn from
	// eleven candidates and a page of ten drawn from four hundred are completely
	// different situations that look identical in the output.
	Retrieved int
}

// Inspect runs retrieval and scoring for a point, keeping the terms.
func (s *Service) Inspect(ctx context.Context, q Query) (*Inspection, error) {
	if q.Limit <= 0 || q.Limit > 100 {
		q.Limit = 25
	}
	w := s.weights.Weights(ctx)

	candidates, err := s.retrieve(ctx, q, nil, w)
	if err != nil {
		return nil, err
	}

	out := &Inspection{
		Profile:   profileName(s.weights),
		Weights:   w,
		Retrieved: len(candidates),
	}

	inspected := make([]InspectedCandidate, 0, len(candidates))
	for _, c := range candidates {
		inspected = append(inspected, InspectedCandidate{
			ReelID:          c.Item.ID,
			Caption:         c.Item.Caption,
			Author:          c.Item.AuthorName,
			Score:           ranking.Score(c.features, w),
			Terms:           ranking.Explain(c.features, w),
			Features:        c.features,
			ExploreEligible: ranking.Eligible(c.features, w),
		})
	}

	sort.SliceStable(inspected, func(i, j int) bool {
		return inspected[i].Score > inspected[j].Score
	})
	for i := range inspected {
		inspected[i].Rank = i + 1
	}
	if len(inspected) > q.Limit {
		inspected = inspected[:q.Limit]
	}
	out.Candidates = inspected

	return out, nil
}
