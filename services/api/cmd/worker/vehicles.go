package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/marketplace/api/internal/modules/classifieds"
	"github.com/marketplace/api/internal/modules/trust"
	"github.com/marketplace/api/internal/modules/vehicles"
)

// The classifieds jobs.
//
// Six of them on one loop, and the tick is chosen by the fraud detectors rather
// than by the housekeeping. A listing that expires twenty minutes late has
// harmed nobody; a stolen car that stays up for a day has. Everything here is
// idempotent — the detectors dedupe on a partial unique index, the sweepers
// filter on the state they are leaving — so running often costs a handful of
// queries that return no rows.
const classifiedsTick = 10 * time.Minute

// classifiedsJobs bundles the services this loop needs so the tick body reads
// as a list of jobs rather than a parameter list.
type classifiedsJobs struct {
	Listings *vehicles.Repository
	Packages *classifieds.Service
	Trust    *trust.Service
}

// expireListings retires listings past their window.
//
// Not housekeeping. A classifieds site whose listings never expire fills with
// cars that sold months ago; buyers message sellers who have stopped answering,
// and that reads to the next buyer as a dead marketplace rather than as a stale
// listing. Expiry is what makes "everything here is available" true, which is
// the product promise PLAN-VIDEO-FIRST §10 is built on.
func expireListings(ctx context.Context, j classifiedsJobs) {
	n, err := j.Listings.ExpireDue(ctx, 500)
	if err != nil {
		slog.ErrorContext(ctx, "listing expiry failed", "error", err)
		return
	}
	if n > 0 {
		slog.InfoContext(ctx, "expired listings", "count", n)
	}
}

// expireOffers closes negotiations that ran out.
//
// A pending offer with no end is a buyer who believes they have a claim on the
// car and a seller who believes they do not.
func expireOffers(ctx context.Context, j classifiedsJobs) {
	n, err := j.Listings.ExpireOffers(ctx, 500)
	if err != nil {
		slog.ErrorContext(ctx, "offer expiry failed", "error", err)
		return
	}
	if n > 0 {
		slog.InfoContext(ctx, "expired offers", "count", n)
	}
}

// runFraudDetectors is the cross-listing pass.
//
// The checks that need a second row: an odometer that went backwards across two
// listings, one VIN listed by two sellers, a price at half the market. The
// single-listing checks already ran inline at authoring time.
//
// Errors are logged and the loop continues. The value of this set is that it is
// a set — four detectors running while one is broken is much better than none,
// and a detector that fails on a bad plan must not silently disable the other
// four for a week.
func runFraudDetectors(ctx context.Context, j classifiedsJobs) {
	n, err := trust.RunDetectors(ctx, j.Trust, 500)
	if err != nil {
		slog.ErrorContext(ctx, "fraud detection partially failed", "error", err, "raised", n)
	}
	if n > 0 {
		slog.InfoContext(ctx, "raised fraud signals", "count", n)
	}
}

// rescoreTrust folds new evidence, and old evidence, into scores.
//
// The second half is the one that is easy to forget. Scores decay with time and
// nothing writes a row when that happens, so a purely event-driven refresh
// leaves a seller restricted forever by evidence that stopped counting months
// ago — with no event to notice it. trust.RescoreDirty picks up anything not
// recomputed in a day for exactly that reason.
func rescoreTrust(ctx context.Context, j classifiedsJobs) {
	n, err := j.Trust.RescoreDirty(ctx, 500)
	if err != nil {
		slog.ErrorContext(ctx, "trust rescoring failed", "error", err)
		return
	}
	if n > 0 {
		slog.InfoContext(ctx, "rescored trust subjects", "count", n)
	}
}

// recogniseListingRevenue moves earned listing fees out of deferred revenue.
//
// Straight-line across the entitlement window, using the identical arithmetic a
// prepaid meal cycle uses (internal/money). Unlike meals, the unit is a day of
// availability rather than a delivered item — the seller bought thirty days of
// being findable and gets them whether or not they used every credit.
func recogniseListingRevenue(ctx context.Context, j classifiedsJobs) {
	n, err := j.Packages.RecogniseDue(ctx, 500)
	if err != nil {
		slog.ErrorContext(ctx, "listing fee recognition failed", "error", err)
		return
	}
	if n > 0 {
		slog.InfoContext(ctx, "recognised listing fee revenue", "purchases", n)
	}
}

// expirePackages closes out entitlement windows that have ended.
//
// It recognises the remainder before closing. A package that expires with money
// still in deferred revenue leaves it there permanently — the entitlement is
// gone, so no future recognition step will ever clear it, and the account stops
// reconciling by a few fils per purchase forever.
func expirePackages(ctx context.Context, j classifiedsJobs) {
	n, err := j.Packages.ExpireDue(ctx, 500)
	if err != nil {
		slog.ErrorContext(ctx, "package expiry failed", "error", err)
		return
	}
	if n > 0 {
		slog.InfoContext(ctx, "expired listing packages", "count", n)
	}
}

func runClassifiedsLoop(ctx context.Context, j classifiedsJobs) {
	if j.Listings == nil || j.Packages == nil || j.Trust == nil {
		slog.Info("classifieds disabled: no services configured")
		return
	}

	// Once at startup, so a deployment that was down over a weekend is not
	// serving a page of expired listings the moment it comes back.
	startupCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	expireListings(startupCtx, j)
	expireOffers(startupCtx, j)
	cancel()

	ticker := time.NewTicker(classifiedsTick)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			tickCtx, cancel := context.WithTimeout(ctx, 8*time.Minute)

			expireListings(tickCtx, j)
			expireOffers(tickCtx, j)

			// Order matters between these two and only these two: rescoring
			// before detection would score against evidence that is one tick
			// stale, which is how a listing that a detector just flagged stays
			// searchable for another ten minutes.
			runFraudDetectors(tickCtx, j)
			rescoreTrust(tickCtx, j)

			// Money last. It is the job with the least urgency — nobody is
			// harmed by revenue being recognised eight minutes late — and
			// putting it ahead of the fraud pass on a slow tick would be
			// trading safety for bookkeeping.
			expirePackages(tickCtx, j)
			recogniseListingRevenue(tickCtx, j)

			cancel()
		}
	}
}
