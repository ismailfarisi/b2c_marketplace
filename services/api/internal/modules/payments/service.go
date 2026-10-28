package payments

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/marketplace/api/internal/httpx"
	"github.com/marketplace/api/internal/modules/ledger"
	"github.com/marketplace/api/internal/money"
	"github.com/marketplace/api/internal/platform/postgres"
)

// ErrNothingToCapture means the order has no successful authorization.
//
// A distinct sentinel rather than a generic conflict, because the capture loop
// needs to tell two cases apart: a card order whose gateway declined (retry) and
// a cash order that never had a hold to begin with (normal, and roughly half the
// orders in this market). Treating the second as a failure fills the outbox with
// events that will be attempted forever and never succeed.
var ErrNothingToCapture = fmt.Errorf("%w: no successful authorization", httpx.ErrConflict)

type Service struct {
	pool     *pgxpool.Pool
	provider Provider

	// commissionBP is the platform's cut in basis points.
	//
	// A constant because the commission model is still open (docs/README.md
	// decision #4) — flat rate, per-vertical, or tiered by volume all imply
	// different tables. Hard-coding one number is honest about that; inventing
	// a schema for a model nobody has chosen is not.
	commissionBP int
	// platformVATBP is VAT on the platform's own commission.
	platformVATBP int
}

func NewService(pool *pgxpool.Pool, provider Provider, commissionBP, platformVATBP int) *Service {
	return &Service{
		pool:          pool,
		provider:      provider,
		commissionBP:  commissionBP,
		platformVATBP: platformVATBP,
	}
}

type Intent struct {
	ID           uuid.UUID
	Status       string
	AmountMinor  int64
	Currency     string
	Provider     string
	ClientSecret string
	RedirectURL  string
}

// Authorize places a hold and, on success, commits the capacity the order is
// holding.
//
// Ordering matters and is not arbitrary. The gateway call happens *outside* the
// transaction, because holding a database transaction open across a network
// call to a third party ties up a connection for however long their p99 is —
// and during an incident, that is how one slow PSP takes down the whole API.
// The transaction opens only once there is a result to record.
func (s *Service) Authorize(ctx context.Context, userID, orderID uuid.UUID, idempotencyKey string) (*Intent, error) {
	quote, err := s.loadAuthorizableQuote(ctx, userID, orderID)
	if err != nil {
		return nil, err
	}

	result, err := s.provider.Authorize(ctx, AuthorizeInput{
		OrderID:        orderID,
		UserID:         userID,
		AmountMinor:    quote.AuthorizeMinor,
		Currency:       quote.Currency,
		IdempotencyKey: idempotencyKey,
	})
	if err != nil {
		return nil, fmt.Errorf("gateway authorize: %w", err)
	}

	intent := &Intent{
		AmountMinor:  quote.AuthorizeMinor,
		Currency:     quote.Currency,
		Provider:     s.provider.Name(),
		Status:       result.Status,
		ClientSecret: result.ClientSecret,
		RedirectURL:  result.RedirectURL,
	}

	err = postgres.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx, `
			INSERT INTO payments (
				order_id, quote_id, user_id, kind, status, provider,
				provider_ref, amount_minor, currency, failure_code
			) VALUES ($1,$2,$3,'authorization',$4,$5,$6,$7,$8,NULLIF($9,''))
			RETURNING id`,
			orderID, quote.ID, userID, mapStatus(result.Status), s.provider.Name(),
			result.ProviderRef, quote.AuthorizeMinor, quote.Currency, result.FailureCode,
		).Scan(&intent.ID)
		if err != nil {
			return fmt.Errorf("insert payment: %w", err)
		}

		if result.Status != "authorized" {
			// A failed authorization leaves capacity held until the TTL expires
			// rather than releasing it immediately. Deliberate: the customer
			// will usually retry with another card within seconds, and dropping
			// the slot means they lose it to someone else mid-checkout.
			return nil
		}

		if _, err := tx.Exec(ctx, `
			UPDATE price_quotes SET state = 'authorized' WHERE id = $1`, quote.ID); err != nil {
			return fmt.Errorf("advance quote: %w", err)
		}

		if _, err := tx.Exec(ctx, `
			UPDATE orders SET status = 'confirmed', confirmed_at = now()
			 WHERE id = $1 AND status = 'pending_payment'`, orderID); err != nil {
			return fmt.Errorf("confirm order: %w", err)
		}

		if err := activateSubscription(ctx, tx, orderID, userID); err != nil {
			return err
		}

		// Capacity moves from reserved to consumed only now. Before this point
		// the slot was held; now it is genuinely sold and the TTL sweeper must
		// not take it back.
		rows, err := tx.Query(ctx,
			`SELECT id FROM capacity_reservations WHERE order_id = $1 AND state = 'held'`, orderID)
		if err != nil {
			return fmt.Errorf("load reservations: %w", err)
		}
		var reservationIDs []uuid.UUID
		for rows.Next() {
			var id uuid.UUID
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			reservationIDs = append(reservationIDs, id)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}

		for _, id := range reservationIDs {
			var committed bool
			if err := tx.QueryRow(ctx, `SELECT commit_capacity($1)`, id).Scan(&committed); err != nil {
				return fmt.Errorf("commit capacity: %w", err)
			}
			if !committed {
				// The reservation expired between placing the order and paying.
				// Rolling back is the only honest outcome — the alternative is
				// a confirmed order against capacity that has been resold.
				return fmt.Errorf("%w: reservation lapsed before payment completed", httpx.ErrCapacityTaken)
			}
		}

		return insertOutbox(ctx, tx, orderID, "order.confirmed", map[string]any{
			"order_id":     orderID,
			"payment_id":   intent.ID,
			"amount_minor": quote.AuthorizeMinor,
			"currency":     quote.Currency,
		})
	})
	if err != nil {
		return nil, err
	}

	return intent, nil
}

// Capture settles the authorization and posts the ledger journal.
//
// This is where money is recognised — nothing is posted at authorization,
// because a hold is not a movement of money and booking revenue on one would
// overstate every reporting period by the value of everything later voided.
func (s *Service) Capture(ctx context.Context, orderID uuid.UUID, finalAmountMinor int64) error {
	// Already captured is success, not an error.
	//
	// The capture loop retries on gateway failures, and a retry that lands
	// after a capture that actually succeeded — a timeout on our side, a
	// response lost, the loop restarting mid-flight — must not charge the
	// customer twice. Checking first is cheap; discovering it from a
	// chargeback is not.
	var already bool
	if err := s.pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM payments
			 WHERE order_id = $1 AND kind = 'capture' AND status = 'succeeded')`,
		orderID).Scan(&already); err != nil {
		return fmt.Errorf("check existing capture: %w", err)
	}
	if already {
		return nil
	}

	auth, err := s.loadAuthorization(ctx, orderID)
	if err != nil {
		return err
	}

	amount := finalAmountMinor
	if amount <= 0 {
		amount = auth.QuoteTotalMinor
	}
	if amount > auth.AuthorizedMinor {
		// Capturing above the hold is rejected by every gateway. Failing here
		// with a clear message beats a cryptic decline after the work is done
		// and the customer has gone.
		return fmt.Errorf("%w: capture %d exceeds authorized %d — the estimate headroom was too small",
			httpx.ErrConflict, amount, auth.AuthorizedMinor)
	}

	result, err := s.provider.Capture(ctx, auth.ProviderRef, amount)
	if err != nil {
		return fmt.Errorf("gateway capture: %w", err)
	}
	if result.Status != "succeeded" {
		return fmt.Errorf("capture failed: %s", result.FailureCode)
	}

	total := money.New(amount, auth.Currency)
	commission := money.Percentage(total, s.commissionBP)
	commissionVAT := money.ExtractTax(commission, s.platformVATBP)

	return postgres.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `
			INSERT INTO payments (
				order_id, quote_id, user_id, kind, status, provider,
				provider_ref, amount_minor, currency, parent_payment_id, settled_at
			) VALUES ($1,$2,$3,'capture','succeeded',$4,$5,$6,$7,$8,now())`,
			orderID, auth.QuoteID, auth.UserID, s.provider.Name(),
			result.ProviderRef, amount, auth.Currency, auth.PaymentID,
		); err != nil {
			return fmt.Errorf("insert capture: %w", err)
		}

		if _, err := tx.Exec(ctx,
			`UPDATE price_quotes SET state = 'captured' WHERE id = $1`, auth.QuoteID); err != nil {
			return fmt.Errorf("advance quote: %w", err)
		}

		if _, err := ledger.PostOrderCaptured(ctx, tx, ledger.CaptureSplit{
			OrderID:            orderID,
			VendorID:           auth.VendorID,
			Currency:           auth.Currency,
			TotalMinor:         amount,
			CommissionMinor:    commission.Minor,
			CommissionVATMinor: commissionVAT.Minor,
		}); err != nil {
			return err
		}

		return insertOutbox(ctx, tx, orderID, "order.captured", map[string]any{
			"order_id":         orderID,
			"amount_minor":     amount,
			"commission_minor": commission.Minor,
			"currency":         auth.Currency,
		})
	})
}

type authorizableQuote struct {
	ID             uuid.UUID
	AuthorizeMinor int64
	Currency       string
}

func (s *Service) loadAuthorizableQuote(ctx context.Context, userID, orderID uuid.UUID) (authorizableQuote, error) {
	var q authorizableQuote
	err := s.pool.QueryRow(ctx, `
		SELECT q.id, COALESCE(q.authorized_minor, q.total_minor), q.currency
		  FROM price_quotes q
		  JOIN orders o ON o.id = q.order_id
		 WHERE q.order_id = $1
		   AND o.user_id = $2
		   AND q.superseded_by IS NULL
		   AND q.state = 'estimated'`,
		orderID, userID).Scan(&q.ID, &q.AuthorizeMinor, &q.Currency)
	if errors.Is(err, pgx.ErrNoRows) {
		// Covers both "no such order" and "already authorized". Not
		// distinguishing them is intentional — the second is a legitimate
		// double-submit and telling a caller which one it hit leaks order
		// existence to someone who does not own it.
		return q, fmt.Errorf("%w: order is not awaiting authorization", httpx.ErrConflict)
	}
	if err != nil {
		return q, fmt.Errorf("load quote: %w", err)
	}
	return q, nil
}

type authorization struct {
	PaymentID       uuid.UUID
	QuoteID         uuid.UUID
	UserID          uuid.UUID
	VendorID        uuid.UUID
	ProviderRef     string
	AuthorizedMinor int64
	QuoteTotalMinor int64
	Currency        string
}

func (s *Service) loadAuthorization(ctx context.Context, orderID uuid.UUID) (authorization, error) {
	var a authorization
	err := s.pool.QueryRow(ctx, `
		SELECT p.id, p.quote_id, p.user_id, o.vendor_id, p.provider_ref,
		       p.amount_minor, q.total_minor, p.currency
		  FROM payments p
		  JOIN orders o ON o.id = p.order_id
		  JOIN price_quotes q ON q.id = p.quote_id
		 WHERE p.order_id = $1
		   AND p.kind = 'authorization'
		   AND p.status = 'succeeded'
		 ORDER BY p.created_at DESC
		 LIMIT 1`, orderID).
		Scan(&a.PaymentID, &a.QuoteID, &a.UserID, &a.VendorID, &a.ProviderRef,
			&a.AuthorizedMinor, &a.QuoteTotalMinor, &a.Currency)
	if errors.Is(err, pgx.ErrNoRows) {
		return a, fmt.Errorf("%w for order %s", ErrNothingToCapture, orderID)
	}
	if err != nil {
		return a, fmt.Errorf("load authorization: %w", err)
	}
	return a, nil
}

func mapStatus(providerStatus string) string {
	switch providerStatus {
	case "authorized":
		return "succeeded"
	case "requires_action":
		return "requires_action"
	default:
		return "failed"
	}
}

func insertOutbox(ctx context.Context, tx pgx.Tx, orderID uuid.UUID, eventType string, payload map[string]any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal outbox payload: %w", err)
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO outbox_events (aggregate_type, aggregate_id, event_type, payload, idempotency_key)
		VALUES ('order', $1, $2, $3, $4)
		ON CONFLICT (idempotency_key) DO NOTHING`,
		orderID, eventType, body, fmt.Sprintf("order:%s:%s", orderID, eventType))
	if err != nil {
		return fmt.Errorf("insert outbox event: %w", err)
	}
	return nil
}

// activateSubscription settles a subscription billing order: its cycle is paid
// and, for the first cycle, the subscription goes live so the scheduler starts
// materialising deliveries. A no-op for any other order.
func activateSubscription(ctx context.Context, tx pgx.Tx, orderID, userID uuid.UUID) error {
	var subID uuid.UUID
	err := tx.QueryRow(ctx, `
		UPDATE subscription_cycles SET status = 'paid', paid_at = now()
		 WHERE order_id = $1 AND status IN ('pending', 'past_due')
		RETURNING subscription_id`, orderID).Scan(&subID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("settle cycle: %w", err)
	}
	// A promo held at checkout counts as used once the checkout is paid.
	if _, err := tx.Exec(ctx, `
		UPDATE promo_redemptions SET status = 'redeemed', redeemed_at = now()
		 WHERE status = 'pending'
		   AND (subscription_id = $1
		        OR rotation_id = (SELECT rotation_id FROM subscriptions WHERE id = $1))`, subID); err != nil {
		return fmt.Errorf("redeem promo: %w", err)
	}
	tag, err := tx.Exec(ctx, `
		UPDATE subscriptions SET status = 'active', updated_at = now()
		 WHERE id = $1 AND status IN ('pending_payment', 'suspended')`, subID)
	if err != nil {
		return fmt.Errorf("activate subscription: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return nil
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO subscription_events
		    (subscription_id, event_type, from_status, to_status, actor_type, actor_id, metadata)
		VALUES ($1, 'activated', 'pending_payment', 'active', 'customer', $2,
		        jsonb_build_object('order_id', $3::uuid))`, subID, userID, orderID)
	if err != nil {
		return fmt.Errorf("record activation: %w", err)
	}
	return nil
}
