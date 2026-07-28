package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/marketplace/api/internal/modules/boosts"
)

// The Phase 9 jobs.
//
// Three of them, on two intervals, and the fast one is the one that would
// otherwise break the product quietly.
//
// **Reservation expiry, every 30 seconds.** Every served-but-unconfirmed
// impression holds budget. A client that fetches a page and closes the app —
// which is most of them, most of the time — leaves a hold behind. Without a
// sweep those accumulate until a campaign's daily cap is entirely reservations,
// at which point it stops serving while its spend reads zero. That is the
// failure that looks like "the ad system is broken" and is arithmetic.
//
// **Billing, hourly.** Rolls confirmed impressions into ledger journals.
//
// **Partitions, daily.** Same reasoning as everywhere else: a row that lands in
// the DEFAULT partition blocks the later creation of any partition covering its
// range.
const (
	boostSweepTick = 30 * time.Second
	boostBillTick  = time.Hour
)

func runBoostLoop(ctx context.Context, svc *boosts.Service) {
	sweep := time.NewTicker(boostSweepTick)
	defer sweep.Stop()
	bill := time.NewTicker(boostBillTick)
	defer bill.Stop()

	// Immediately at startup. A worker restarted mid-incident is exactly when a
	// backlog of stale reservations exists, and every minute of it is budget
	// that campaigns cannot spend.
	expireBoostReservations(ctx, svc)
	ensureBoostPartitions(ctx, svc)

	for {
		select {
		case <-ctx.Done():
			return
		case <-sweep.C:
			expireBoostReservations(ctx, svc)
		case <-bill.C:
			billBoosts(ctx, svc)
			ensureBoostPartitions(ctx, svc)
		}
	}
}

func expireBoostReservations(ctx context.Context, svc *boosts.Service) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	n, err := svc.ExpireReservations(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "boost reservation sweep failed", "error", err)
		return
	}
	if n > 0 {
		slog.DebugContext(ctx, "released stale boost reservations", "campaigns", n)
	}
}

// billBoosts posts the previous hour.
//
// The previous hour, never the current one. An hour still in progress is still
// receiving confirmations, and billing it would post a charge and then find more
// impressions belonging to a period already closed — which the unique constraint
// on (campaign, period_start) would reject, silently dropping them.
func billBoosts(ctx context.Context, svc *boosts.Service) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	posted, err := svc.Bill(ctx, time.Now())
	if err != nil {
		slog.ErrorContext(ctx, "boost billing failed", "error", err)
		return
	}
	if posted > 0 {
		slog.InfoContext(ctx, "boost charges posted", "campaigns", posted)
	}
}

func ensureBoostPartitions(ctx context.Context, svc *boosts.Service) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	// A week ahead. Daily partitions on a 30-second loop would be cheap to
	// create one at a time; a week's buffer is what survives the worker being
	// down over a weekend.
	if err := svc.EnsurePartitions(ctx, 7); err != nil {
		slog.ErrorContext(ctx, "boost partitions", "error", err)
	}
}
