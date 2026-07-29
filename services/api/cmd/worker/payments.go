package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/marketplace/api/internal/modules/payments"
)

// The capture loop.
//
// QA finding 13: `payments.Capture` was written, correct, and reachable from
// nowhere. Card orders authorized at checkout and were never captured — the
// platform did not take the money, and because `ledger.PostOrderCaptured` sits
// behind Capture, card orders never entered the ledger at all. Vendor payables
// were being computed from cash collections and subscriptions only, and looked
// entirely plausible while missing every card sale.
//
// The trigger is `order.delivered`, emitted by `driver.Complete` when the last
// stop on an order reaches a terminal state. Capture is *not* done inside that
// transaction: it calls a payment gateway, and holding a fulfillment row locked
// for the length of a gateway round trip means a driver waiting in the street
// for their app — and a gateway timeout rolling back a delivery that physically
// happened.
//
// So the delivery commits as a fact, and the capture follows as a consequence
// that can be retried. Which is also the honest description of the money: the
// customer received the goods either way, and what remains is our problem.

// captureDelivered drains order.delivered events into gateway captures.
func captureDelivered(ctx context.Context, pool *pgxpool.Pool, svc *payments.Service) {
	rows, err := pool.Query(ctx, `
		SELECT id, aggregate_id
		  FROM outbox_events
		 WHERE published_at IS NULL
		   AND aggregate_type = 'order'
		   AND event_type = 'order.delivered'
		   AND (next_attempt_at IS NULL OR next_attempt_at < now())
		 ORDER BY id
		 LIMIT 100`)
	if err != nil {
		slog.ErrorContext(ctx, "capture queue query failed", "error", err)
		return
	}

	type event struct {
		id      int64
		orderID uuid.UUID
	}
	var events []event
	for rows.Next() {
		var e event
		if err := rows.Scan(&e.id, &e.orderID); err != nil {
			rows.Close()
			slog.ErrorContext(ctx, "capture queue scan failed", "error", err)
			return
		}
		events = append(events, e)
	}
	rows.Close()

	for _, e := range events {
		err := svc.Capture(ctx, e.orderID, 0)

		// A cash order has no authorization to capture, and that is not a
		// failure — it is the normal path for roughly half the orders in this
		// market. Marking it handled rather than retrying stops the queue
		// filling with events that will never succeed and will never stop
		// being attempted.
		if errors.Is(err, payments.ErrNothingToCapture) {
			markCaptured(ctx, pool, e.id, e.orderID, "no authorization — cash order")
			continue
		}

		if err != nil {
			// Backoff, and keep the reason. `last_error` on this table is the
			// only place a declined capture is written down; without it the
			// symptom is a payable that is quietly short and no explanation.
			if _, uerr := pool.Exec(ctx, `
				UPDATE outbox_events
				   SET attempts = attempts + 1,
				       last_error = $2,
				       next_attempt_at = now() + (least(attempts + 1, 10) * interval '2 minutes')
				 WHERE id = $1`, e.id, err.Error()); uerr != nil {
				slog.ErrorContext(ctx, "capture backoff update failed", "error", uerr)
			}
			slog.WarnContext(ctx, "capture failed, will retry",
				"order_id", e.orderID, "error", err)
			continue
		}

		markCaptured(ctx, pool, e.id, e.orderID, "captured")
	}
}

func markCaptured(ctx context.Context, pool *pgxpool.Pool, id int64, orderID uuid.UUID, why string) {
	if _, err := pool.Exec(ctx,
		`UPDATE outbox_events SET published_at = now() WHERE id = $1`, id); err != nil {
		slog.ErrorContext(ctx, "marking capture handled failed", "error", err)
		return
	}
	slog.InfoContext(ctx, fmt.Sprintf("order settled: %s", why), "order_id", orderID)
}

// runCaptureLoop ticks the capture queue.
//
// Two minutes, the slowest loop in this process. Everything else here has
// someone waiting on it — a driver for a route, a customer for an OTP, a
// campaign for its budget. Nothing waits on a capture: the delivery already
// happened, and what is outstanding is a number in a report and money in a
// gateway that will still be there in two minutes.
func runCaptureLoop(ctx context.Context, pool *pgxpool.Pool, svc *payments.Service) {
	ticker := time.NewTicker(2 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			tick, cancel := context.WithTimeout(ctx, 90*time.Second)
			captureDelivered(tick, pool, svc)
			cancel()
		}
	}
}
