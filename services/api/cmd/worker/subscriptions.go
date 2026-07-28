package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/marketplace/api/internal/modules/subscriptions"
)

// The recurring jobs.
//
// Four of them, and the ordering between two matters: materialisation must run
// after activation, and recognition must run after deliveries complete. The
// rest are independent.
//
// None of these is on a nightly cron despite PLAN.md §4 calling the scheduler "a
// nightly job". A single nightly run has one failure mode that is unacceptable
// here: if it fails, nobody eats tomorrow, and the next attempt is in
// twenty-four hours. Running the same idempotent job every fifteen minutes
// means a transient database blip costs fifteen minutes rather than a day, and
// costs nothing at all when there is no work to do — the query returns no rows.

// materialiseSubscriptions extends the rolling window.
//
// PLAN.md §4: a subscription does not pre-generate a year of fulfillments; a
// job materialises a rolling 14-day window from the plan's rules. Idempotent at
// three levels — the planner describes desired state, the insert is ON CONFLICT
// DO NOTHING, and a unique index makes a double-write impossible — so running
// it often is free.
func materialiseSubscriptions(ctx context.Context, svc *subscriptions.Service) {
	if svc == nil {
		return
	}
	subs, created, err := svc.MaterialiseDue(ctx, 200)
	if err != nil {
		slog.ErrorContext(ctx, "subscription materialisation failed", "error", err)
		return
	}
	if created > 0 {
		slog.InfoContext(ctx, "materialised subscription deliveries",
			"subscriptions", subs, "fulfillments", created)
	}
}

// recogniseSubscriptionRevenue moves earned money out of deferred revenue.
//
// The unit of recognition is a *delivered* meal, which is what makes it earned.
// Recognising on a schedule instead would credit a vendor for a week they were
// closed, and the first anyone would know is a payout that did not match the
// work done.
func recogniseSubscriptionRevenue(ctx context.Context, svc *subscriptions.Service) {
	if svc == nil {
		return
	}
	n, err := svc.RecogniseDelivered(ctx, 500)
	if err != nil {
		slog.ErrorContext(ctx, "revenue recognition failed", "error", err)
		return
	}
	if n > 0 {
		slog.InfoContext(ctx, "recognised subscription revenue", "cycles", n)
	}
}

// activatePaidSubscriptions flips a subscription live once its billing order is
// paid.
//
// Written as a sweep rather than as a payment-webhook side effect, deliberately.
// The webhook is the fast path and it will do this too; this exists because a
// dropped webhook currently means a customer who paid and never got any food,
// which is the single worst failure available in this vertical. A sweep that
// catches it fifteen minutes later is not elegant and it is the difference
// between a delay and a refund.
func activatePaidSubscriptions(ctx context.Context, pool *pgxpool.Pool) {
	tag, err := pool.Exec(ctx, `
		WITH paid AS (
		    SELECT c.id AS cycle_id, c.subscription_id
		      FROM subscription_cycles c
		      JOIN orders o ON o.id = c.order_id
		     WHERE c.status = 'pending'
		       AND o.status IN ('confirmed', 'in_progress', 'completed')
		),
		cycles AS (
		    UPDATE subscription_cycles
		       SET status = 'paid', paid_at = now()
		     WHERE id IN (SELECT cycle_id FROM paid)
		)
		UPDATE subscriptions
		   SET status = 'active'
		 WHERE id IN (SELECT subscription_id FROM paid)
		   AND status IN ('pending_payment', 'suspended')`)
	if err != nil {
		slog.ErrorContext(ctx, "subscription activation sweep failed", "error", err)
		return
	}
	if n := tag.RowsAffected(); n > 0 {
		slog.InfoContext(ctx, "activated paid subscriptions", "count", n)
	}
}

// openRenewalCycles bills the next period before the current one ends.
//
// Three days of lead time, and the number is set by PLAN.md §6's constraint
// rather than by convenience: "Meal customers must know *before* the delivery
// doesn't arrive." A renewal charged on the last day of a cycle leaves the
// dunning schedule no room to run before the first meal of the next one is due.
// Three days is enough for two retries and a notification.
func openRenewalCycles(ctx context.Context, pool *pgxpool.Pool) {
	rows, err := pool.Query(ctx, `
		SELECT c.id, c.subscription_id, c.cycle_no, c.ends_on,
		       c.amount_minor, c.currency, s.branch_id, s.vendor_id,
		       s.user_id, s.service_type, p.cycle_days, b.country, s.timezone
		  FROM subscription_cycles c
		  JOIN subscriptions s ON s.id = c.subscription_id
		  JOIN subscription_plans p ON p.id = s.plan_id
		  JOIN vendor_branches b ON b.id = s.branch_id
		 WHERE c.status = 'paid'
		   AND s.status = 'active'
		   AND s.renewal_mode = 'auto'
		   AND c.ends_on <= CURRENT_DATE + 3
		   -- No successor yet. Without this the sweep re-bills the same
		   -- subscription every fifteen minutes for three days.
		   AND NOT EXISTS (
		       SELECT 1 FROM subscription_cycles n
		        WHERE n.subscription_id = c.subscription_id
		          AND n.cycle_no = c.cycle_no + 1)
		 ORDER BY c.ends_on
		 LIMIT 200`)
	if err != nil {
		slog.ErrorContext(ctx, "renewal query failed", "error", err)
		return
	}

	type renewal struct {
		cycleID, subID, branchID, vendorID, userID uuid.UUID
		cycleNo                                    int
		endsOn                                     time.Time
		amount                                     int64
		currency, serviceType, country, timezone   string
		cycleDays                                  int
	}
	var due []renewal
	for rows.Next() {
		var r renewal
		if err := rows.Scan(&r.cycleID, &r.subID, &r.cycleNo, &r.endsOn,
			&r.amount, &r.currency, &r.branchID, &r.vendorID, &r.userID,
			&r.serviceType, &r.cycleDays, &r.country, &r.timezone); err != nil {
			rows.Close()
			slog.ErrorContext(ctx, "renewal scan failed", "error", err)
			return
		}
		due = append(due, r)
	}
	rows.Close()

	for _, r := range due {
		// One statement per renewal, with the billing order and the cycle
		// created together. A cycle without its order is a period nobody is
		// charged for; an order without its cycle is a charge nothing explains.
		if _, err := pool.Exec(ctx, `
			WITH new_order AS (
			    INSERT INTO orders (
			        order_no, user_id, branch_id, vendor_id, service_type,
			        status, kind, source_subscription_id, currency
			    ) VALUES (
			        generate_order_no($8), $2, $3, $4, $5,
			        'pending_payment', 'subscription_generated', $1, $6
			    )
			    RETURNING id
			)
			INSERT INTO subscription_cycles (
			    subscription_id, cycle_no, starts_on, ends_on, order_id,
			    status, amount_minor, currency, deliverable_days
			)
			SELECT $1, $7, $9::date, $9::date + ($10 - 1),
			       new_order.id, 'pending', $11, $6,
			       -- The recognition divisor, frozen when the cycle opens so a
			       -- mid-cycle pause cannot retroactively change the daily rate
			       -- applied to days already recognised.
			       --
			       -- EXTRACT(DOW) is 0 = Sunday, which is the convention this
			       -- schema uses throughout. ISO's Monday-first numbering here
			       -- would count the wrong days and misprice every cycle.
			       (SELECT count(*)
			          FROM generate_series($9::date, $9::date + ($10 - 1), interval '1 day') d
			         WHERE EXTRACT(DOW FROM d)::int IN (
			                   SELECT DISTINCT weekday
			                     FROM subscription_schedule_rules
			                    WHERE subscription_id = $1))
			  FROM new_order
			ON CONFLICT (subscription_id, cycle_no) DO NOTHING`,
			r.subID, r.userID, r.branchID, r.vendorID, r.serviceType,
			r.currency, r.cycleNo+1, r.country, r.endsOn.AddDate(0, 0, 1),
			r.cycleDays, r.amount,
		); err != nil {
			slog.ErrorContext(ctx, "renewal failed",
				"subscription_id", r.subID, "cycle_no", r.cycleNo+1, "error", err)
			continue
		}
		slog.InfoContext(ctx, "opened renewal cycle",
			"subscription_id", r.subID, "cycle_no", r.cycleNo+1)
	}
}

// runDunning walks the retry schedule on unpaid cycles.
//
// PLAN.md §6: failed renewal → retry schedule → grace period → **suspend
// fulfillments, never delete the subscription**. Everything survives a
// suspension: the rules, the address, the meal history, the cycle record. A
// customer who fixes their card is resumed rather than re-onboarded, and that
// distinction is worth more than every other line in this function.
//
// The charge itself is not attempted here — that is the payments module's job
// and it needs a stored card, which is Phase 8. What this does is advance the
// state machine so that the suspension, the grace period and the notifications
// are real from the day meals ship rather than added after the first customer
// silently stops being fed.
func runDunning(ctx context.Context, pool *pgxpool.Pool) {
	rows, err := pool.Query(ctx, `
		SELECT c.id, c.subscription_id, c.attempts, c.grace_until
		  FROM subscription_cycles c
		  JOIN subscriptions s ON s.id = c.subscription_id
		 WHERE c.status = 'past_due'
		   AND (c.next_attempt_at IS NULL OR c.next_attempt_at <= now())
		   AND s.status <> 'cancelled'
		 ORDER BY c.next_attempt_at NULLS FIRST
		 LIMIT 200`)
	if err != nil {
		slog.ErrorContext(ctx, "dunning query failed", "error", err)
		return
	}

	type overdue struct {
		cycleID, subID uuid.UUID
		attempts       int
		graceUntil     *time.Time
	}
	var list []overdue
	for rows.Next() {
		var o overdue
		if err := rows.Scan(&o.cycleID, &o.subID, &o.attempts, &o.graceUntil); err != nil {
			rows.Close()
			return
		}
		list = append(list, o)
	}
	rows.Close()

	now := time.Now()
	for _, o := range list {
		at, exhausted := subscriptions.NextAttempt(o.attempts, now)

		if !exhausted {
			if _, err := pool.Exec(ctx, `
				UPDATE subscription_cycles
				   SET attempts = attempts + 1, next_attempt_at = $2
				 WHERE id = $1`, o.cycleID, at); err != nil {
				slog.ErrorContext(ctx, "dunning advance failed", "cycle_id", o.cycleID, "error", err)
			}
			continue
		}

		// Out of retries. Grace first, then suspension — a customer whose meals
		// stopped without warning does not come back, and two lunches given away
		// is cheap against a cycle of revenue.
		if o.graceUntil == nil {
			until := subscriptions.GraceUntil(now, time.UTC)
			if _, err := pool.Exec(ctx,
				`UPDATE subscription_cycles SET grace_until = $2 WHERE id = $1`,
				o.cycleID, until); err != nil {
				slog.ErrorContext(ctx, "grace period failed", "cycle_id", o.cycleID, "error", err)
			}
			slog.WarnContext(ctx, "subscription entered grace period",
				"subscription_id", o.subID, "until", until.Format("2006-01-02"))
			continue
		}
		if now.Before(*o.graceUntil) {
			continue
		}

		if _, err := pool.Exec(ctx, `
			UPDATE subscriptions SET status = 'suspended'
			 WHERE id = $1 AND status = 'active'`, o.subID); err != nil {
			slog.ErrorContext(ctx, "suspension failed", "subscription_id", o.subID, "error", err)
			continue
		}
		// Future deliveries only. Meals already delivered stay delivered.
		if _, err := pool.Exec(ctx, `
			UPDATE fulfillments SET status = 'cancelled'
			 WHERE subscription_id = $1
			   AND scheduled_date >= CURRENT_DATE
			   AND status = 'scheduled'`, o.subID); err != nil {
			slog.ErrorContext(ctx, "suspension sweep failed", "subscription_id", o.subID, "error", err)
		}
		slog.WarnContext(ctx, "subscription suspended for non-payment — not deleted",
			"subscription_id", o.subID)
	}
}

// ensureMealSelectionPartitions creates next month's partition ahead of time.
//
// Same reasoning as the impression partitions: rows that land in DEFAULT block
// the later creation of any partition whose range would have covered them.
// Three months of headroom rather than two, because selections are made ahead
// of the rolling fulfillment window rather than behind it.
func ensureMealSelectionPartitions(ctx context.Context, pool *pgxpool.Pool) {
	for _, offset := range []int{0, 1, 2, 3} {
		month := time.Now().UTC().AddDate(0, offset, 0)
		var name string
		if err := pool.QueryRow(ctx,
			`SELECT create_meal_selection_partition($1::date)`,
			month.Format("2006-01-02")).Scan(&name); err != nil {
			slog.ErrorContext(ctx, "meal selection partition creation failed",
				"month", month.Format("2006-01"), "error", err)
			return
		}
	}
}

// subscriptionTick is every fifteen minutes. See the note at the top of this
// file on why this is not a nightly cron.
const subscriptionTick = 15 * time.Minute

func runSubscriptionLoop(ctx context.Context, pool *pgxpool.Pool, svc *subscriptions.Service) {
	if svc == nil {
		slog.Info("subscriptions disabled: no service configured")
		return
	}

	// Once at startup, so a fresh deployment is not a month behind on
	// partitions and a fortnight behind on deliveries.
	startupCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	ensureMealSelectionPartitions(startupCtx, pool)
	activatePaidSubscriptions(startupCtx, pool)
	materialiseSubscriptions(startupCtx, svc)
	cancel()

	ticker := time.NewTicker(subscriptionTick)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			tickCtx, cancel := context.WithTimeout(ctx, 10*time.Minute)

			ensureMealSelectionPartitions(tickCtx, pool)
			// Order matters between these two: a subscription activated by the
			// first call must be materialised by the second in the same tick,
			// or a customer who pays at 09:01 waits until 09:15 for any sign
			// that their food is coming.
			activatePaidSubscriptions(tickCtx, pool)
			materialiseSubscriptions(tickCtx, svc)

			openRenewalCycles(tickCtx, pool)
			runDunning(tickCtx, pool)
			recogniseSubscriptionRevenue(tickCtx, svc)

			cancel()
		}
	}
}
