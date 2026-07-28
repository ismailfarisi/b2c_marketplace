package app

import (
	"context"

	"github.com/marketplace/api/internal/modules/boosts"
	"github.com/marketplace/api/internal/modules/feed"
)

// SponsoredFiller adapts boosts.Service to feed.SponsoredSource.
//
// The adapter is what keeps `feed` out of the ad business. It is guarded by
// internal/app/seam_test.go, and the guarantee that test provides is exactly
// this: the feed cannot acquire an opinion about bidding, pacing or billing,
// because it cannot see the package that has them.
//
// Worth being explicit about the direction of the dependency. The feed hands
// over a *location* and gets back reel ids; it does not hand over a candidate
// set for the ad system to rank. That ordering is what makes "a sponsored reel
// passes the same hard filters" enforceable rather than aspirational — the
// boosts query applies `resolve_reel_coverage` itself, and there is no code
// path in which the feed could ask it not to.
type SponsoredFiller struct{ Boosts *boosts.Service }

func (f SponsoredFiller) Fill(ctx context.Context, req feed.SponsoredRequest) ([]feed.SponsoredItem, error) {
	delivered, err := f.Boosts.Fill(ctx, boosts.Placement{
		Lat: req.Lat, Lng: req.Lng,
		ViewerKey:    req.ViewerKey,
		ViewerID:     req.ViewerID,
		CategoryID:   req.Category,
		Locale:       req.Locale,
		ExcludeReels: req.ExcludeReels,
		Slots:        req.Slots,
	})
	if err != nil {
		return nil, err
	}

	out := make([]feed.SponsoredItem, 0, len(delivered))
	for _, d := range delivered {
		out = append(out, feed.SponsoredItem{ReelID: d.ReelID, DeliveryID: d.ID})
	}
	return out, nil
}
