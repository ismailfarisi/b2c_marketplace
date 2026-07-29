package payments

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/marketplace/api/internal/httpx"
	"github.com/marketplace/api/internal/modules/ledger"
	"github.com/marketplace/api/internal/money"
	"github.com/marketplace/api/internal/platform/postgres"
)

// Refund — written for the admin console, and written *here* rather than there.
//
// PLAN-ADMIN-CONSOLE §3.2: every admin action goes through the service that owns
// the data, and where no method exists, **the method gets written**. The admin
// path is not a licence to skip it.
//
// A refund written from a console handler would have had to insert a `payments`
// row, post a ledger journal, reverse the commission split and write an outbox
// event — four things this package already knows how to do consistently and no
// handler should learn. The first version that skipped one of them would be
// discovered as a ledger that does not balance, months later.

// RefundInput is what a refund needs to be defensible six months later.
type RefundInput struct {
	OrderID uuid.UUID
	// AmountMinor is zero for a full refund of what was captured. A partial
	// refund names its amount, and naming it is the difference between "we
	// refunded the delivery fee" and "we refunded something".
	AmountMinor int64
	// Reason is required. A refund with no stated reason is indistinguishable
	// from an unauthorised one six months later, which is the whole argument
	// for the field.
	Reason string
}

// Refund reverses part or all of a capture.
//
// Reverses the *commission split too*, proportionally. That is the part a
// hand-written refund gets wrong: refunding the customer without reversing the
// platform's revenue leaves the ledger balanced and the P&L overstated, and
// nobody notices until a quarter closes.
func (s *Service) Refund(ctx context.Context, in RefundInput) (uuid.UUID, error) {
	if in.Reason == "" {
		return uuid.Nil, &httpx.ValidationError{Fields: []httpx.FieldError{{
			Field: "reason", Code: "required",
			Message: "a refund with no stated reason cannot be distinguished from an unauthorised one",
		}}}
	}

	cap, err := s.loadCapture(ctx, in.OrderID)
	if err != nil {
		return uuid.Nil, err
	}

	amount := in.AmountMinor
	if amount <= 0 {
		amount = cap.CapturedMinor - cap.RefundedMinor
	}
	if amount <= 0 {
		return uuid.Nil, fmt.Errorf("%w: this order is already fully refunded", httpx.ErrConflict)
	}
	if amount > cap.CapturedMinor-cap.RefundedMinor {
		return uuid.Nil, fmt.Errorf(
			"%w: refunding %d would exceed the %d still refundable on this order",
			httpx.ErrConflict, amount, cap.CapturedMinor-cap.RefundedMinor)
	}

	result, err := s.provider.Refund(ctx, cap.ProviderRef, amount)
	if err != nil {
		return uuid.Nil, fmt.Errorf("gateway refund: %w", err)
	}
	if result.Status != "succeeded" {
		return uuid.Nil, fmt.Errorf("refund failed at the gateway with status %q", result.Status)
	}

	// The commission comes back proportionally. A 50% refund returns half the
	// platform's cut and half the VAT on it — computed from the refund rather
	// than from the original, so a sequence of partial refunds sums to exactly
	// the original split rather than drifting a fils per operation.
	total := money.New(amount, cap.Currency)
	commission := money.Percentage(total, s.commissionBP)
	commissionVAT := money.ExtractTax(commission, s.platformVATBP)

	var refundID uuid.UUID
	err = postgres.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `
			INSERT INTO payments (
				order_id, quote_id, user_id, kind, status, provider,
				provider_ref, amount_minor, currency, parent_payment_id, settled_at
			) VALUES ($1,$2,$3,'refund','succeeded',$4,$5,$6,$7,$8,now())
			RETURNING id`,
			in.OrderID, cap.QuoteID, cap.UserID, s.provider.Name(),
			result.ProviderRef, amount, cap.Currency, cap.PaymentID,
		).Scan(&refundID); err != nil {
			return fmt.Errorf("insert refund: %w", err)
		}

		// The quote state moves only on a *full* refund. A partially refunded
		// order is still a captured order — it was delivered, it earned
		// commission on the part that stands, and marking it `refunded` would
		// remove it from every revenue report that filters on state.
		if amount == cap.CapturedMinor-cap.RefundedMinor {
			if _, err := tx.Exec(ctx,
				`UPDATE price_quotes SET state = 'refunded' WHERE id = $1`, cap.QuoteID); err != nil {
				return fmt.Errorf("advance quote: %w", err)
			}
		}

		if _, err := ledger.PostOrderRefunded(ctx, tx, ledger.CaptureSplit{
			OrderID:            in.OrderID,
			VendorID:           cap.VendorID,
			Currency:           cap.Currency,
			TotalMinor:         amount,
			CommissionMinor:    commission.Minor,
			CommissionVATMinor: commissionVAT.Minor,
		}); err != nil {
			return err
		}

		return insertOutbox(ctx, tx, in.OrderID, "order.refunded", map[string]any{
			"order_id":     in.OrderID,
			"amount_minor": amount,
			"currency":     cap.Currency,
			"reason":       in.Reason,
		})
	})
	if err != nil {
		return uuid.Nil, err
	}
	return refundID, nil
}

type capturedOrder struct {
	PaymentID     uuid.UUID
	QuoteID       uuid.UUID
	UserID        uuid.UUID
	VendorID      uuid.UUID
	ProviderRef   string
	Currency      string
	CapturedMinor int64
	RefundedMinor int64
}

func (s *Service) loadCapture(ctx context.Context, orderID uuid.UUID) (capturedOrder, error) {
	var c capturedOrder
	err := s.pool.QueryRow(ctx, `
		SELECT p.id, p.quote_id, p.user_id, b.vendor_id,
		       coalesce(p.provider_ref, ''), p.currency, p.amount_minor,
		       coalesce((SELECT sum(r.amount_minor) FROM payments r
		                  WHERE r.order_id = p.order_id AND r.kind = 'refund'
		                    AND r.status = 'succeeded'), 0)
		  FROM payments p
		  JOIN orders o ON o.id = p.order_id
		  JOIN vendor_branches b ON b.id = o.branch_id
		 WHERE p.order_id = $1 AND p.kind = 'capture' AND p.status = 'succeeded'
		 ORDER BY p.created_at DESC LIMIT 1`, orderID).
		Scan(&c.PaymentID, &c.QuoteID, &c.UserID, &c.VendorID,
			&c.ProviderRef, &c.Currency, &c.CapturedMinor, &c.RefundedMinor)
	if errors.Is(err, pgx.ErrNoRows) {
		return c, fmt.Errorf("%w: nothing has been captured on this order, so there is "+
			"nothing to refund — void the authorization instead", httpx.ErrConflict)
	}
	if err != nil {
		return c, fmt.Errorf("load capture: %w", err)
	}
	return c, nil
}
