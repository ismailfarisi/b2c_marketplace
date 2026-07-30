// Command worker runs background jobs.
//
// Jobs are Postgres-backed (River) rather than Redis-backed, so a job can be
// enqueued in the same transaction as the state change that requires it. That
// is the same guarantee the outbox gives events, and it removes the entire
// class of "the order committed but the job never ran" bug.
//
// Phase 1 jobs:
//   - outbox relay          publish committed events
//   - capacity sweeper      release expired reservations
//   - idempotency reaper    drop keys past their TTL
//
// Phase 2 adds the media pipeline: probe, transcode, moderate, publish.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/marketplace/api/internal/app"
	"github.com/marketplace/api/internal/config"
	"github.com/marketplace/api/internal/modules/boosts"
	"github.com/marketplace/api/internal/modules/classifieds"
	"github.com/marketplace/api/internal/modules/dispatch"
	"github.com/marketplace/api/internal/modules/invoicing"
	"github.com/marketplace/api/internal/modules/kyc"
	"github.com/marketplace/api/internal/modules/payments"
	"github.com/marketplace/api/internal/modules/payouts"
	"github.com/marketplace/api/internal/modules/subscriptions"
	"github.com/marketplace/api/internal/modules/tax"
	"github.com/marketplace/api/internal/modules/timesignal"
	"github.com/marketplace/api/internal/modules/trust"
	"github.com/marketplace/api/internal/modules/vehicles"
	"github.com/marketplace/api/internal/platform/postgres"
	"github.com/marketplace/api/internal/platform/redisx"
)

var version = "dev"

// Commission, matching cmd/api. Duplicated here rather than read from config
// because it is the same open decision in both places — docs/README.md #4 — and
// two copies that must agree are easier to notice than one that silently
// diverges from a default nobody set.
const (
	commissionBP  = 1_500 // 15%
	platformVATBP = 500   // 5% UAE VAT on the platform's own commission
)

func main() {
	if err := run(); err != nil {
		slog.Error("fatal", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)).
		With("service", "worker", "version", version))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := postgres.Connect(ctx, postgres.Config{
		URL:      cfg.DatabaseURL,
		MaxConns: 10,
		MinConns: 1,
	})
	if err != nil {
		return err
	}
	defer pool.Close()

	rdb, err := redisx.Connect(ctx, cfg.RedisURL)
	if err != nil {
		return err
	}
	if rdb != nil {
		defer rdb.Close()
	}

	// One registry set for the process. This used to build a second one inline
	// for BuildMedia; with a third seam on it that is two registries whose
	// contents merely happen to agree, which is the kind of thing that stays
	// true until the day someone registers a provider in one place.
	registries := app.BuildRegistries(pool)

	media, err := app.BuildMedia(ctx, cfg, pool, rdb, registries.Actions)
	if err != nil {
		return err
	}

	// The media pipeline runs on its own faster loop, in its own goroutine: it
	// has a five-minute end-to-end budget (exit criterion #1) and must not
	// queue behind a 30-second capacity sweep.
	go runMediaLoop(ctx, pool, media, timesignal.New(pool, registries.Subjects))

	// The recurring loop, likewise separate. Its slowest job walks the
	// impressions-scale fulfillment table; putting it in line ahead of the
	// media pipeline would blow the upload-to-feed budget on days when a lot of
	// subscriptions renew.
	go runSubscriptionLoop(ctx, pool, subscriptions.NewService(pool, registries.Verticals,
		subscriptions.Config{
			CommissionBP:    commissionBP,
			PlatformVATBP:   platformVATBP,
			ReserveCapacity: true,
		}))

	// The classifieds loop, likewise separate and for the sharpest reason of
	// the three: its fraud detectors are the only jobs in this process where
	// being late has a victim. Queueing them behind a capacity sweep or a
	// subscription materialisation run would make the tick interval a fiction.
	go runClassifiedsLoop(ctx, classifiedsJobs{
		Listings: vehicles.NewRepository(pool),
		Packages: classifieds.NewService(pool),
		Trust:    trust.NewService(pool),
	})

	// The logistics loop, on the fastest tick of the four. Route planning is the
	// only job in this process whose lateness a customer can see.
	go runLogisticsLoop(ctx, logisticsJobs{
		Dispatch: dispatch.NewService(pool, dispatch.DefaultOptions()),
		Payouts:  payouts.NewService(pool, nil, app.PayoutGate{KYC: kyc.NewService(pool)}),
		Pool:     pool,
	})

	// The Phase 8 loop. Fastest tick in the process, because the delivery OTP
	// on it has to reach a customer before the driver reaches their door.
	go runComplianceLoop(ctx, complianceJobs{
		Notify:   app.BuildNotify(cfg, pool),
		KYC:      kyc.NewService(pool),
		Invoices: invoicing.NewService(pool, tax.NewService(pool), true),
		Pool:     pool,
	})

	// Shared with the capture loop below. One instance, so the commission
	// numbers a capture books are the same ones the API quotes.
	// The same guard the API makes, made again here rather than trusted.
	// This process is the one that actually calls capture, so a sandbox gateway
	// reaching production would settle every delivery against a stub and report
	// revenue that does not exist.
	var provider payments.Provider = payments.SandboxProvider{}
	if cfg.IsProduction() {
		return fmt.Errorf("no production payment provider is configured — " +
			"wire a real gateway before deploying")
	}
	paymentsSvc := payments.NewService(pool, provider, commissionBP, platformVATBP)

	// The Phase 9 loop. Its fast tick releases budget held by impressions that
	// were served and never confirmed — without it a campaign's daily cap fills
	// with holds and it stops serving while reporting zero spend.
	go runBoostLoop(ctx, boosts.NewService(pool, nil))

	// The capture loop. On its own goroutine like the rest, and on a slower
	// tick than any of them: it calls a payment gateway, and the delivery it
	// follows has already happened. Nothing is waiting on this except a number
	// in a report.
	go runCaptureLoop(ctx, pool, paymentsSvc)

	// River is wired here once the first job type exists. Until then the loop
	// below runs the two sweepers that Phase 1 genuinely needs, on a ticker.
	// This is not a permanent design — it is the smallest thing that keeps
	// capacity from leaking while the job framework is still being introduced.
	slog.Info("worker started")

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			slog.Info("worker shutting down")
			return nil
		case <-ticker.C:
			sweep(ctx, pool)
		}
	}
}

func sweep(ctx context.Context, pool *pgxpool.Pool) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	// Expired capacity reservations. Every abandoned checkout holds inventory
	// until this runs; skipping it means vendors show as fully booked with no
	// orders behind it, and nobody notices until they complain.
	released, err := releaseExpiredReservations(ctx, pool)
	if err != nil && !errors.Is(err, context.Canceled) {
		slog.ErrorContext(ctx, "capacity sweep failed", "error", err)
	} else if released > 0 {
		slog.InfoContext(ctx, "released expired reservations", "count", released)
	}

	if _, err := pool.Exec(ctx,
		`DELETE FROM idempotency_keys WHERE expires_at < now()`); err != nil {
		slog.ErrorContext(ctx, "idempotency reap failed", "error", err)
	}
}

func releaseExpiredReservations(ctx context.Context, pool *pgxpool.Pool) (int, error) {
	rows, err := pool.Query(ctx, `
		SELECT id
		  FROM capacity_reservations
		 WHERE state = 'held' AND expires_at < now()
		 ORDER BY expires_at
		 LIMIT 500`)
	if err != nil {
		return 0, err
	}

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}

	released := 0
	for _, id := range ids {
		// release_capacity is transactional per reservation. Doing them one at a
		// time rather than in one big transaction keeps lock hold times short on
		// the hottest table in the system.
		var ok bool
		if err := pool.QueryRow(ctx,
			`SELECT release_capacity($1, 'expired')`, id).Scan(&ok); err != nil {
			slog.WarnContext(ctx, "release failed", "reservation_id", id, "error", err)
			continue
		}
		if ok {
			released++
		}
	}
	return released, nil
}
