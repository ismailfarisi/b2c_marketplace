package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/marketplace/api/internal/modules/dispatch"
	"github.com/marketplace/api/internal/modules/payouts"
)

// The logistics jobs.
//
// A faster tick than the other loops, and for a reason that is not performance:
// route planning is the only job in this process whose *lateness is visible to a
// customer*. A subscription materialised twenty minutes late harms nobody; an
// order that arrives at 17:55 for an 18:00 window and is not on a route until
// 18:05 has already missed it.
const logisticsTick = 5 * time.Minute

type logisticsJobs struct {
	Dispatch *dispatch.Service
	Payouts  *payouts.Service
	Pool     *pgxpool.Pool
}

// planRoutes sequences whatever arrived since the last run.
//
// Driven by the fulfillment table rather than by a list of branches, so a branch
// that onboarded this morning is planned this afternoon without anyone editing a
// config. Idempotent by construction — it only reads `route_id IS NULL` — which
// is what makes a five-minute tick free rather than five times the work.
//
// Two days are planned, not one. A branch whose 09:00 window opens tomorrow
// needs its route to exist before the driver's shift starts, and a planner that
// only ever looks at today produces the first route of the morning at 00:05
// while everyone is asleep.
func planRoutes(ctx context.Context, j logisticsJobs) {
	today := truncateDay(time.Now().UTC())

	for _, date := range []time.Time{today, today.AddDate(0, 0, 1)} {
		branches, err := j.Dispatch.BranchesWithWork(ctx, date, 200)
		if err != nil {
			slog.ErrorContext(ctx, "route planning: find branches failed", "error", err)
			return
		}
		for _, branchID := range branches {
			res, err := j.Dispatch.BuildRoutes(ctx, branchID, date)
			if err != nil {
				// One branch failing must not stop the rest. A bad service area
				// or a missing timezone is a per-branch data problem, and the
				// other forty branches have deliveries to make.
				slog.ErrorContext(ctx, "route planning failed",
					"branch_id", branchID, "date", date, "error", err)
				continue
			}
			if res.RoutesCreated > 0 {
				slog.InfoContext(ctx, "planned routes",
					"branch_id", branchID, "date", date,
					"routes", res.RoutesCreated, "stops", res.StopsRouted,
					// Logged at plan time, which is the only moment anyone can
					// still do something about it.
					"already_late", res.LateStops)
			}
		}
	}
}

// releaseStaleRoutes hands back work nobody is doing.
//
// A route assigned two hours ago that the driver never started is not assigned
// in any useful sense — the phone is off, the shift never began, the driver went
// home. Leaving it assigned means the planner skips that work forever, because
// the fulfillments already carry a route_id and look handled.
//
// This is the failure mode that makes a dispatch system quietly lose orders, and
// it is invisible until a customer calls.
func releaseStaleRoutes(ctx context.Context, j logisticsJobs) {
	rows, err := j.Pool.Query(ctx, `
		SELECT id FROM delivery_routes
		 WHERE status = 'assigned'
		   AND updated_at < now() - interval '2 hours'
		   AND started_at IS NULL
		 LIMIT 100`)
	if err != nil {
		slog.ErrorContext(ctx, "stale route sweep failed", "error", err)
		return
	}
	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			slog.ErrorContext(ctx, "stale route scan failed", "error", err)
			return
		}
		ids = append(ids, id)
	}
	rows.Close()

	for _, routeID := range ids {
		if err := j.Dispatch.Release(ctx, routeID, "assigned but never started"); err != nil {
			slog.ErrorContext(ctx, "stale route release failed", "route_id", routeID, "error", err)
			continue
		}
		slog.WarnContext(ctx, "released a stale route", "route_id", routeID)
	}
}

// ensureDriverPingPartitions keeps a week of partitions ahead.
//
// Same shape as the meal-selection partitions in the subscription loop. A
// partitioned table with no partition for tomorrow does not fail loudly — the
// DEFAULT partition swallows the rows and the table stops being partitioned in
// any useful sense, which is discovered months later when retention is needed.
func ensureDriverPingPartitions(ctx context.Context, j logisticsJobs) {
	if _, err := j.Pool.Exec(ctx, `SELECT ensure_driver_ping_partitions(7)`); err != nil {
		slog.ErrorContext(ctx, "driver ping partition maintenance failed", "error", err)
	}
}

// reapDriverPings drops location history past its retention.
//
// This is location data about identifiable people. Keeping it forever is a
// liability rather than an asset, and the operational value of a ping is spent
// within days — after a delivery is settled nobody looks at it again except in a
// dispute, which is what fulfillment_events and proof_of_delivery are for.
//
// Thirty days, dropped a partition at a time. A DELETE over a month of pings
// would vacuum for an hour; a DETACH is instant.
const pingRetentionDays = 30

func reapDriverPings(ctx context.Context, j logisticsJobs) {
	_, err := j.Pool.Exec(ctx, `
		DO $$
		DECLARE
			part record;
			cutoff date := current_date - $1::int;
		BEGIN
			FOR part IN
				SELECT c.relname
				  FROM pg_class c
				  JOIN pg_inherits i ON i.inhrelid = c.oid
				  JOIN pg_class p ON p.oid = i.inhparent
				 WHERE p.relname = 'driver_location_pings'
				   AND c.relname ~ '^driver_location_pings_[0-9]{8}$'
				   AND to_date(right(c.relname, 8), 'YYYYMMDD') < cutoff
			LOOP
				EXECUTE format('DROP TABLE IF EXISTS %I', part.relname);
			END LOOP;
		END $$;`, pingRetentionDays)
	if err != nil {
		slog.ErrorContext(ctx, "driver ping retention failed", "error", err)
	}
}

// generatePayouts closes the previous week.
//
// Never the current one — a statement covering a period that is still accruing
// changes after the vendor was shown it. Regenerating a *draft* is normal and
// expected: a refund landing on Tuesday for a Saturday order legitimately
// changes last week's figures until the statement is issued.
func generatePayouts(ctx context.Context, j logisticsJobs) {
	period := payouts.PreviousWeek(time.Now(), j.Payouts.Location())

	n, err := j.Payouts.Generate(ctx, period)
	if err != nil {
		slog.ErrorContext(ctx, "payout generation failed",
			"period_start", period.Start.Format("2006-01-02"), "error", err)
		return
	}
	if n > 0 {
		slog.InfoContext(ctx, "generated payout statements",
			"period_start", period.Start.Format("2006-01-02"),
			"period_end", period.End.Format("2006-01-02"), "statements", n)
	}
}

func runLogisticsLoop(ctx context.Context, j logisticsJobs) {
	if j.Dispatch == nil || j.Payouts == nil || j.Pool == nil {
		slog.Info("logistics disabled: no services configured")
		return
	}

	startupCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	ensureDriverPingPartitions(startupCtx, j)
	planRoutes(startupCtx, j)
	cancel()

	ticker := time.NewTicker(logisticsTick)
	defer ticker.Stop()

	// Payouts run far less often than routes and are gated on a counter rather
	// than a second ticker: one loop with one shutdown path is easier to reason
	// about than two, and "every 12th tick" is exactly as precise as this job
	// needs to be.
	const payoutEvery = 12
	tick := 0

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			tick++
			tickCtx, cancel := context.WithTimeout(ctx, 4*time.Minute)

			ensureDriverPingPartitions(tickCtx, j)
			planRoutes(tickCtx, j)
			releaseStaleRoutes(tickCtx, j)

			if tick%payoutEvery == 0 {
				generatePayouts(tickCtx, j)
				reapDriverPings(tickCtx, j)
			}

			cancel()
		}
	}
}

func truncateDay(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}
