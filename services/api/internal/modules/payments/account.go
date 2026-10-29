package payments

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/marketplace/api/internal/httpx"
	"github.com/marketplace/api/internal/platform/postgres"
)

// AccountProvider is the payments.provider of an order billed to a company
// account instead of a card.
const AccountProvider = "company_account"

// AccountCharge runs inside the authorization transaction, after the order's
// amount is known and before anything is confirmed. Returning an error (for
// example, over the credit limit) rolls the whole authorization back.
type AccountCharge func(ctx context.Context, tx pgx.Tx, amountMinor int64, currency string) error

// AuthorizeOnAccount confirms an order the way a successful card authorization
// does, but billed to an account the caller vouches for. Everything after the
// payment row — confirming the order, activating the subscription, the
// order.confirmed event, capture on delivery and the ledger — is the same path
// a card payment takes.
func (s *Service) AuthorizeOnAccount(ctx context.Context, userID, orderID uuid.UUID, accountRef string, charge AccountCharge) (*Intent, error) {
	quote, err := s.loadAuthorizableQuote(ctx, userID, orderID)
	if err != nil {
		return nil, err
	}
	intent := &Intent{
		AmountMinor: quote.AuthorizeMinor,
		Currency:    quote.Currency,
		Provider:    AccountProvider,
		Status:      "authorized",
	}
	err = postgres.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		var state string
		if err := tx.QueryRow(ctx,
			`SELECT state FROM price_quotes WHERE id = $1 FOR UPDATE`, quote.ID).Scan(&state); err != nil {
			return fmt.Errorf("lock quote: %w", err)
		}
		if state != "estimated" {
			return fmt.Errorf("%w: order is not awaiting authorization", httpx.ErrConflict)
		}
		if err := charge(ctx, tx, quote.AuthorizeMinor, quote.Currency); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `
			INSERT INTO payments (
				order_id, quote_id, user_id, kind, status, provider,
				provider_ref, amount_minor, currency
			) VALUES ($1,$2,$3,'authorization','succeeded',$4,$5,$6,$7)
			RETURNING id`,
			orderID, quote.ID, userID, AccountProvider, accountRef,
			quote.AuthorizeMinor, quote.Currency,
		).Scan(&intent.ID); err != nil {
			return fmt.Errorf("insert payment: %w", err)
		}
		if _, err := tx.Exec(ctx,
			`UPDATE price_quotes SET state = 'authorized' WHERE id = $1`, quote.ID); err != nil {
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
		return insertOutbox(ctx, tx, orderID, "order.confirmed", map[string]any{
			"order_id":     orderID,
			"payment_id":   intent.ID,
			"amount_minor": quote.AuthorizeMinor,
			"currency":     quote.Currency,
			"billed_to":    accountRef,
		})
	})
	if err != nil {
		return nil, err
	}
	return intent, nil
}
