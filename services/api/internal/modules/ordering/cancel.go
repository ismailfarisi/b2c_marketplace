package ordering

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/marketplace/api/internal/httpx"
	"github.com/marketplace/api/internal/platform/postgres"
)

// Cancel — written for the admin console, and written here for the reason
// PLAN-ADMIN-CONSOLE §3.2 gives: an admin action goes through the service that
// owns the data, and where no method exists, the method gets written.
//
// A console handler that did this by hand would have set `orders.status` and
// stopped. What it would have missed is everything below: the capacity the order
// is holding, the fulfillments a driver still has on their manifest, and the
// outbox event the rest of the system reacts to. A branch quietly full of
// capacity nobody is using is the failure mode, and it is invisible until a
// vendor complains they have no bookings.

// CancelInput is what cancelling needs.
type CancelInput struct {
	OrderID uuid.UUID
	Reason  string
	// ActorID is the staff member, for the events this writes. Nil when the
	// customer cancels their own order through the app.
	ActorID *uuid.UUID
}

// Cancel stops an order and releases everything it was holding.
//
// Deliberately does **not** refund. Cancelling and refunding are different
// decisions — an order cancelled before dispatch is refunded in full, one
// cancelled after the food was cooked usually is not — and a method that did
// both would take that decision away from the person making it. The console
// offers them as two actions, in that order.
func (s *Service) Cancel(ctx context.Context, in CancelInput) error {
	if in.Reason == "" {
		return &httpx.ValidationError{Fields: []httpx.FieldError{{
			Field: "reason", Code: "required",
			Message: "a cancellation with no stated reason cannot be explained to the customer or the vendor",
		}}}
	}

	return postgres.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		var status string
		if err := tx.QueryRow(ctx,
			`SELECT status::text FROM orders WHERE id = $1 FOR UPDATE`, in.OrderID).
			Scan(&status); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return httpx.ErrNotFound
			}
			return fmt.Errorf("load order: %w", err)
		}

		switch status {
		case "cancelled":
			return nil // idempotent
		case "completed":
			// A completed order is history. Reversing it is a refund and a
			// credit note, not a status change, and calling that "cancellation"
			// would put a delivered order into every "cancelled" report.
			return fmt.Errorf("%w: this order is completed — refund it instead of cancelling",
				httpx.ErrConflict)
		}

		// Release capacity first. If anything below fails the transaction rolls
		// back and the hold survives, which is the safe direction: an order that
		// is still holding a slot it no longer needs is recoverable by the
		// sweeper, and a slot released for an order that is still live is
		// double-booking.
		//
		// Both states, and the second one is the whole point. A reservation
		// commits when the order is confirmed, and the first version of this
		// only released `held` rows — so cancelling a confirmed order, the
		// common case, gave nothing back. Committed rows need
		// release_committed_capacity (migration 21), which decrements
		// `consumed` where the other decrements `reserved`.
		rows, err := tx.Query(ctx, `
			SELECT r.id, r.state FROM capacity_reservations r
			  JOIN fulfillments f ON f.reservation_id = r.id
			 WHERE f.order_id = $1 AND r.state IN ('held', 'committed')`, in.OrderID)
		if err != nil {
			return fmt.Errorf("find reservations: %w", err)
		}
		type reservation struct {
			id    uuid.UUID
			state string
		}
		var reservations []reservation
		for rows.Next() {
			var r reservation
			if err := rows.Scan(&r.id, &r.state); err != nil {
				rows.Close()
				return err
			}
			reservations = append(reservations, r)
		}
		rows.Close()

		for _, r := range reservations {
			fn := "release_capacity"
			if r.state == "committed" {
				fn = "release_committed_capacity"
			}
			var ok bool
			if err := tx.QueryRow(ctx,
				`SELECT `+fn+`($1, 'cancelled')`, r.id).Scan(&ok); err != nil {
				return fmt.Errorf("release capacity: %w", err)
			}
			if !ok {
				// The row changed state between the SELECT and here, which
				// under FOR UPDATE on the order should not happen. Loud rather
				// than silent: a slot that quietly failed to come back is the
				// bug this function exists to fix.
				return fmt.Errorf("reservation %s was %s but %s declined it",
					r.id, r.state, fn)
			}
		}

		// Fulfillments that have not happened yet are cancelled; ones that have
		// are left alone. A pickup that already happened is a fact, and
		// rewriting it to `cancelled` would tell the driver's own history that
		// they never collected the bag they are holding.
		if _, err := tx.Exec(ctx, `
			UPDATE fulfillments
			   SET status = 'cancelled', driver_id = NULL, route_id = NULL
			 WHERE order_id = $1
			   AND status IN ('scheduled', 'dispatched', 'en_route', 'arrived')`,
			in.OrderID); err != nil {
			return fmt.Errorf("cancel fulfillments: %w", err)
		}

		if _, err := tx.Exec(ctx, `
			INSERT INTO fulfillment_events (fulfillment_id, from_status, to_status,
			                                actor_type, actor_id, metadata)
			SELECT f.id, f.status, 'cancelled', $2, $3,
			       jsonb_build_object('reason', $4::text)
			  FROM fulfillments f
			 WHERE f.order_id = $1 AND f.status = 'cancelled'`,
			in.OrderID, actorType(in.ActorID), in.ActorID, in.Reason); err != nil {
			return fmt.Errorf("record fulfillment events: %w", err)
		}

		if _, err := tx.Exec(ctx, `
			UPDATE orders
			   SET status = 'cancelled', cancelled_at = now(), cancellation_reason = $2
			 WHERE id = $1`, in.OrderID, in.Reason); err != nil {
			return fmt.Errorf("cancel order: %w", err)
		}

		return s.repo.InsertOutbox(ctx, tx, "order", in.OrderID, "order.cancelled",
			map[string]any{
				"order_id": in.OrderID,
				"reason":   in.Reason,
				"by":       actorType(in.ActorID),
			})
	})
}

func actorType(actorID *uuid.UUID) string {
	if actorID == nil {
		return "customer"
	}
	return "admin"
}
