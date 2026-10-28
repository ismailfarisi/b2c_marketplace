package subscriptions

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/marketplace/api/internal/modules/ledger"
	"github.com/marketplace/api/internal/money"
	"github.com/marketplace/api/internal/platform/postgres"
)

// The rolling window.
//
// PLAN.md §4: "A subscription does not pre-generate a year of fulfillments. A
// nightly job materializes a rolling 14-day window of fulfillments from the
// plan's rules."
//
// The alternative — generating everything up front — fails on the second thing
// that happens to a subscription. A pause has to unpick a year of rows; a plan
// change has to rewrite them; a schedule edit has to diff them. Every one of
// those is tractable over fourteen days and intractable over three hundred and
// sixty-five, and the table is twenty-five times smaller for free.

// MaterialiseOne brings one subscription's fulfillments up to the horizon.
//
// Idempotent at three levels, deliberately: Plan describes desired state rather
// than a change, the insert is ON CONFLICT DO NOTHING, and the unique index on
// (subscription, date, sequence) makes a double-write impossible even if the
// first two were wrong. The nightly job, a manual retry, and a resume after
// payment all call this without coordinating.
func (s *Service) MaterialiseOne(ctx context.Context, subID uuid.UUID, now time.Time) (int, error) {
	created := 0

	err := postgres.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		live, err := s.repo.LoadLive(ctx, tx, subID)
		if err != nil {
			return err
		}
		if live.Suspended || live.Status == "paused" {
			return nil
		}

		from := now
		if live.Materialised != nil {
			// Resume from the day after the horizon rather than from today: the
			// days in between already have fulfillments, and re-planning them
			// only to have every insert conflict is work with no outcome.
			next := dayIn(*live.Materialised, live.Location).AddDate(0, 0, 1)
			if next.After(from) {
				from = next
			}
		}
		to := dayIn(now, live.Location).Add(Horizon)

		occurrences := Plan(live.Subscription, live.Rules, live.Pauses, live.Skips, from, to, now)
		if len(occurrences) == 0 {
			// Still advance the horizon. A subscription whose window contains no
			// delivery days — a Ramadan pause, a plan between cycles — would
			// otherwise be re-planned from scratch on every run forever.
			return s.repo.SetMaterialisedThrough(ctx, tx, subID, to)
		}

		addressIDs := make([]uuid.UUID, 0, len(occurrences))
		seen := map[uuid.UUID]bool{}
		for _, o := range occurrences {
			if !seen[o.AddressID] {
				seen[o.AddressID] = true
				addressIDs = append(addressIDs, o.AddressID)
			}
		}
		addresses, err := s.repo.LoadAddresses(ctx, tx, addressIDs)
		if err != nil {
			return err
		}

		vertical, err := s.verticals.Get(liveServiceType(ctx, tx, subID))
		if err != nil {
			return fmt.Errorf("subscriptions: %w", err)
		}

		for _, occ := range occurrences {
			addr, ok := addresses[occ.AddressID]
			if !ok {
				// The customer deleted the address a rule pointed at. Skipping
				// the day and logging is better than failing the whole batch:
				// one undeliverable day is a support call, a failed nightly job
				// is every subscriber's whole fortnight.
				slog.WarnContext(ctx, "subscription delivery has no address, skipping",
					"subscription_id", subID, "date", occ.Date.Format("2006-01-02"))
				continue
			}

			reservation, err := s.reserve(ctx, tx, live, vertical.Capacity.ResourceKey(), occ)
			if err != nil {
				return err
			}

			id, err := s.repo.InsertFulfillment(ctx, tx, subID, live.BranchID, occ, reservation, addr)
			if err != nil {
				return err
			}
			if id != uuid.Nil {
				created++
			}
		}

		return s.repo.SetMaterialisedThrough(ctx, tx, subID, to)
	})

	return created, err
}

// reserve draws capacity for one delivery day.
//
// Returns a null reservation rather than failing when the kitchen is full, and
// that is the deliberate difference from the order path. A one-off order that
// cannot get capacity must fail — the customer is standing there and can pick
// another slot. A subscription delivery that cannot get capacity is a meal the
// customer has *already paid for*, and refusing to create it would silently
// drop a day from a plan somebody bought.
//
// So the fulfillment is created either way and the vendor sees it on their
// board over their stated limit. That is an operational problem a human can
// solve on the day; the alternative is a customer who paid for twenty meals and
// received nineteen with no record of the missing one.
func (s *Service) reserve(ctx context.Context, tx pgx.Tx, live *Live, resourceKey string, occ Occurrence) (uuid.NullUUID, error) {
	if !s.reservationsHeld {
		return uuid.NullUUID{}, nil
	}

	var bucketID uuid.UUID
	err := tx.QueryRow(ctx, `
		SELECT id FROM capacity_buckets
		 WHERE branch_id = $1 AND resource_key = $2 AND bucket_date = $3 AND window_id = $4`,
		live.BranchID, resourceKey, occ.Date, occ.WindowID).Scan(&bucketID)
	if err != nil {
		// No bucket means the branch never opened that window. The delivery is
		// still created; the vendor will see it.
		return uuid.NullUUID{}, nil
	}

	var reservationID *uuid.UUID
	// A long TTL because this is not a checkout hold — the meal is paid for and
	// the reservation stands until it is delivered. The sweeper's 15-minute
	// expiry would otherwise release a fortnight of a kitchen's capacity
	// overnight.
	if err := tx.QueryRow(ctx,
		`SELECT reserve_capacity($1, NULL, 1, make_interval(days => 30))`,
		bucketID).Scan(&reservationID); err != nil {
		slog.WarnContext(ctx, "subscription capacity reservation failed, creating the delivery anyway",
			"subscription_id", live.ID, "date", occ.Date.Format("2006-01-02"), "error", err)
		return uuid.NullUUID{}, nil
	}
	if reservationID == nil {
		slog.WarnContext(ctx, "kitchen over capacity for a paid subscription delivery",
			"subscription_id", live.ID, "branch_id", live.BranchID,
			"date", occ.Date.Format("2006-01-02"))
		return uuid.NullUUID{}, nil
	}
	return uuid.NullUUID{UUID: *reservationID, Valid: true}, nil
}

// MaterialiseDue runs the rolling window for every subscription that needs it.
//
// Ordered by how stale the horizon is, so a subscription that has fallen
// furthest behind is caught up first. A LIMIT without an ordering recomputes
// the same rows forever and leaves the tail permanently unmaterialised.
func (s *Service) MaterialiseDue(ctx context.Context, batch int) (subs, created int, err error) {
	if batch <= 0 {
		batch = 200
	}

	rows, qerr := s.pool.Query(ctx, `
		SELECT id
		  FROM subscriptions
		 WHERE status = 'active'
		   AND (materialised_through IS NULL
		        OR materialised_through < CURRENT_DATE + make_interval(secs => $1))
		 ORDER BY materialised_through ASC NULLS FIRST
		 LIMIT $2`, Horizon.Seconds(), batch)
	if qerr != nil {
		return 0, 0, fmt.Errorf("subscriptions: due query: %w", qerr)
	}

	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if serr := rows.Scan(&id); serr != nil {
			rows.Close()
			return 0, 0, fmt.Errorf("subscriptions: scan due: %w", serr)
		}
		ids = append(ids, id)
	}
	rows.Close()
	if rerr := rows.Err(); rerr != nil {
		return 0, 0, rerr
	}

	now := time.Now()
	for _, id := range ids {
		// One transaction per subscription. A single transaction over two
		// hundred of them would hold locks on capacity buckets — the hottest
		// table in the system — for the length of the whole batch, and one bad
		// row would roll back everybody's fortnight.
		n, merr := s.MaterialiseOne(ctx, id, now)
		if merr != nil {
			slog.ErrorContext(ctx, "materialisation failed", "subscription_id", id, "error", merr)
			continue
		}
		subs++
		created += n
	}
	return subs, created, nil
}

// --- revenue recognition -------------------------------------------------------

// RecogniseDelivered moves earned revenue out of deferred.
//
// Runs daily against completed fulfillments. The unit of recognition is a
// delivered meal, which is what makes it *earned* — the vendor cooked it and
// someone received it. Recognising on a schedule instead would credit a vendor
// for a week they were closed.
//
// One journal per cycle per run, not one per meal: the ledger records money
// movements, and thirty thousand identical two-fils entries is a table nobody
// can query and an audit nobody can read.
func (s *Service) RecogniseDelivered(ctx context.Context, batch int) (int, error) {
	if batch <= 0 {
		batch = 500
	}

	type target struct {
		cycleID    uuid.UUID
		subID      uuid.UUID
		vendorID   uuid.UUID
		currency   string
		amount     int64
		recognised int64
		subsidy    int64
		recSubsidy int64
		deliver    int
		delivered  int
	}

	rows, err := s.pool.Query(ctx, `
		SELECT c.id, c.subscription_id, s.vendor_id, c.currency,
		       c.amount_minor, c.recognised_minor, c.promo_subsidy_minor,
		       c.recognised_subsidy_minor, c.deliverable_days, COALESCE(f.delivered, 0)
		  FROM subscription_cycles c
		  JOIN subscriptions s ON s.id = c.subscription_id
		  LEFT JOIN LATERAL (
		      SELECT count(*) AS delivered
		        FROM fulfillments f
		       WHERE f.subscription_id = c.subscription_id
		         AND f.status = 'completed'
		         AND f.scheduled_date BETWEEN c.starts_on AND c.ends_on
		  ) f ON true
		 WHERE c.status = 'paid'
		   AND (c.recognised_minor < c.amount_minor
		        OR c.recognised_subsidy_minor < c.promo_subsidy_minor)
		   AND c.deliverable_days > 0
		 ORDER BY c.starts_on
		 LIMIT $1`, batch)
	if err != nil {
		return 0, fmt.Errorf("subscriptions: recognition query: %w", err)
	}

	var targets []target
	for rows.Next() {
		var t target
		if err := rows.Scan(&t.cycleID, &t.subID, &t.vendorID, &t.currency,
			&t.amount, &t.recognised, &t.subsidy, &t.recSubsidy, &t.deliver, &t.delivered); err != nil {
			rows.Close()
			return 0, fmt.Errorf("subscriptions: scan recognition: %w", err)
		}
		targets = append(targets, t)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}

	posted := 0
	for _, t := range targets {
		step := RecogniseStep(t.amount, t.delivered, t.deliver, t.recognised) +
			RecogniseStep(t.subsidy, t.delivered, t.deliver, t.recSubsidy)
		if step <= 0 {
			continue
		}

		err := postgres.InTx(ctx, s.pool, func(tx pgx.Tx) error {
			// Re-read under lock. Two runs of this job overlapping would
			// otherwise both compute the same step from the same stale
			// `recognised_minor` and post it twice, and the CHECK on the column
			// would only catch it on the second one.
			var current, currentSubsidy int64
			if err := tx.QueryRow(ctx,
				`SELECT recognised_minor, recognised_subsidy_minor FROM subscription_cycles WHERE id = $1 FOR UPDATE`,
				t.cycleID).Scan(&current, &currentSubsidy); err != nil {
				return fmt.Errorf("subscriptions: lock cycle: %w", err)
			}
			amount := RecogniseStep(t.amount, t.delivered, t.deliver, current)
			subsidy := RecogniseStep(t.subsidy, t.delivered, t.deliver, currentSubsidy)
			if amount <= 0 {
				// The ledger needs customer money in every recognition; a
				// subsidy alone waits for the next delivery's step.
				return nil
			}

			// Commission on what the meals were worth, so a platform-funded
			// promo does not also cut the platform's take from the kitchen.
			commission := money.Percentage(money.New(amount+subsidy, t.currency), s.commissionBP).Minor
			commissionVAT := money.ExtractTax(money.New(commission, t.currency), s.platformVATBP).Minor

			if _, err := ledger.PostSubscriptionRecognised(ctx, tx, ledger.SubscriptionRecognition{
				SubscriptionID:     t.subID,
				CycleID:            t.cycleID,
				VendorID:           t.vendorID,
				Currency:           t.currency,
				AmountMinor:        amount,
				SubsidyMinor:       subsidy,
				CommissionMinor:    commission,
				CommissionVATMinor: commissionVAT,
			}); err != nil {
				return err
			}

			if _, err := tx.Exec(ctx,
				`UPDATE subscription_cycles SET recognised_minor = recognised_minor + $2,
				        recognised_subsidy_minor = recognised_subsidy_minor + $3
				  WHERE id = $1`, t.cycleID, amount, subsidy); err != nil {
				return fmt.Errorf("subscriptions: advance recognition: %w", err)
			}
			return nil
		})
		if err != nil {
			slog.ErrorContext(ctx, "revenue recognition failed", "cycle_id", t.cycleID, "error", err)
			continue
		}
		posted++
	}
	return posted, nil
}

// liveServiceType reads a subscription's vertical.
//
// A separate tiny query rather than another column on Live: it is a primary-key
// lookup Postgres serves from cache, and threading it through LoadLive would
// mean every caller of that function carries a field only this one needs.
func liveServiceType(ctx context.Context, tx pgx.Tx, subID uuid.UUID) string {
	var serviceType string
	if err := tx.QueryRow(ctx,
		`SELECT service_type FROM subscriptions WHERE id = $1`, subID).Scan(&serviceType); err != nil {
		return ""
	}
	return serviceType
}
