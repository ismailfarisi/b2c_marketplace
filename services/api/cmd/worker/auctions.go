package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/marketplace/api/internal/modules/auctions"
)

// auctionTick is how often lots are opened and closed.
//
// Fifteen seconds, an order of magnitude tighter than every other sweep in this
// worker — and the difference is the whole reason auctions get their own loop.
//
// Everything else here is eventually correct: a listing that expires ten
// minutes late costs nobody anything. A lot that closes ten minutes late is
// taking bids after the hammer, and every one of those bids is a person
// believing they might win something that is already decided. The anti-snipe
// window is two minutes, so the sweep has to be well inside it or the extension
// it grants is a lie.
const auctionTick = 15 * time.Second

// unsoldNoticeWindow bounds the unsold-notice scan.
//
// An hour: comfortably longer than any plausible outage of a fifteen-second
// sweep, and short enough that the query stays an index range rather than a
// scan of every lot the platform has ever run. The dedupe key stops the
// duplicate messages; nothing but this stops the duplicate work.
const unsoldNoticeWindow = time.Hour

// auctionCommissionBP and auctionVATBP mirror the order path's numbers.
//
// Duplicated from cmd/api rather than shared, matching what the codebase
// already does with these two constants — and that duplication is exactly why
// the comment there says the commission model is still an open decision. When
// it is settled, both move together or the platform earns different margins on
// a meal and a plate.
const (
	auctionCommissionBP = 1_500 // 15%
	auctionVATBP        = 500   // 5% UAE VAT on the platform's own commission
)

func runAuctionLoop(ctx context.Context, svc *auctions.Service) {
	if svc == nil {
		slog.Info("auctions disabled: no service configured")
		return
	}

	// Once at startup. A worker that was down for an hour comes back to lots
	// that should have opened and lots that should have closed, and the closes
	// are the urgent half: until this runs, a finished lot is still accepting
	// bids.
	startupCtx, cancel := context.WithTimeout(ctx, time.Minute)
	sweepAuctions(startupCtx, svc)
	cancel()

	ticker := time.NewTicker(auctionTick)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// A short budget on purpose. If a sweep cannot finish in ten
			// seconds something is badly wrong, and the right response is to
			// let the next tick try rather than to pile ticks on top of a stuck
			// query.
			tickCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			sweepAuctions(tickCtx, svc)
			cancel()
		}
	}
}

func sweepAuctions(ctx context.Context, svc *auctions.Service) {
	now := time.Now()

	// Opening first. A lot whose opens_at and closes_at both fell inside the
	// same downtime should open and then close, in that order, rather than
	// being skipped entirely — which is what closing first would do, since a
	// `scheduled` lot is not eligible to close.
	if opened, err := svc.OpenDue(ctx, now); err != nil {
		slog.ErrorContext(ctx, "auction open sweep failed", "error", err)
	} else if opened > 0 {
		slog.InfoContext(ctx, "auction lots opened", "count", opened)
	}

	won, unsold, err := svc.CloseDue(ctx, now)
	if err != nil {
		slog.ErrorContext(ctx, "auction close sweep failed", "error", err)
		return
	}
	if won > 0 || unsold > 0 {
		slog.InfoContext(ctx, "auction lots closed", "won", won, "unsold", unsold)
	}

	// Settlement and the unsold notice run after the close and outside its
	// urgency. A lot that settles a tick late costs nothing; a lot that closes
	// a tick late is taking bids after the hammer — which is why these are not
	// inside CloseDue.
	if settled, err := svc.SettleClosed(ctx, auctionCommissionBP, auctionVATBP, 100); err != nil {
		slog.ErrorContext(ctx, "auction settlement failed", "error", err)
	} else if settled > 0 {
		slog.InfoContext(ctx, "auction lots settled", "count", settled)
	}

	if _, err := svc.NotifyUnsold(ctx, now.Add(-unsoldNoticeWindow), 100); err != nil {
		slog.ErrorContext(ctx, "auction unsold notice failed", "error", err)
	}
}
