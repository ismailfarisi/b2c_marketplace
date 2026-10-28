package payments

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/marketplace/api/internal/httpx"
	"github.com/marketplace/api/internal/platform/postgres"
)

// AuthorizeRotation takes one payment for the four weekly billing orders of a
// kitchen rotation.
//
// One hold at the gateway for the sum, because the customer bought one thing.
// It is then recorded against each week's order at that week's own amount, so
// capture, the ledger and the payout statement still see four orders from four
// kitchens and attribute each week's money to the kitchen that cooks it.
// Capturing the parts separately needs a provider that supports multiple
// captures against one authorization.
func (s *Service) AuthorizeRotation(ctx context.Context, userID, rotationID uuid.UUID, idempotencyKey string) (*Intent, error) {
	var status string
	err := s.pool.QueryRow(ctx,
		`SELECT status FROM meal_rotations WHERE id = $1 AND user_id = $2`,
		rotationID, userID).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, httpx.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load rotation: %w", err)
	}
	if status != "pending_payment" {
		return nil, fmt.Errorf("%w: rotation is not awaiting payment", httpx.ErrConflict)
	}

	rows, err := s.pool.Query(ctx, `
		SELECT c.order_id
		  FROM meal_rotations r
		  JOIN subscriptions s ON s.rotation_id = r.id
		  JOIN subscription_cycles c ON c.subscription_id = s.id AND c.cycle_no = 1
		 WHERE r.id = $1 AND r.user_id = $2 AND r.status = 'pending_payment'
		   AND c.order_id IS NOT NULL
		 ORDER BY s.rotation_week`, rotationID, userID)
	if err != nil {
		return nil, fmt.Errorf("load rotation orders: %w", err)
	}
	var orderIDs []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		orderIDs = append(orderIDs, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(orderIDs) == 0 {
		return nil, fmt.Errorf("%w: rotation is not awaiting payment", httpx.ErrConflict)
	}

	quotes := make([]authorizableQuote, 0, len(orderIDs))
	var total int64
	for _, id := range orderIDs {
		q, err := s.loadAuthorizableQuote(ctx, userID, id)
		if err != nil {
			return nil, err
		}
		if len(quotes) > 0 && q.Currency != quotes[0].Currency {
			return nil, fmt.Errorf("rotation orders in different currencies")
		}
		quotes = append(quotes, q)
		total += q.AuthorizeMinor
	}

	result, err := s.provider.Authorize(ctx, AuthorizeInput{
		OrderID:        orderIDs[0],
		UserID:         userID,
		AmountMinor:    total,
		Currency:       quotes[0].Currency,
		IdempotencyKey: idempotencyKey,
	})
	if err != nil {
		return nil, fmt.Errorf("gateway authorize: %w", err)
	}

	intent := &Intent{
		AmountMinor:  total,
		Currency:     quotes[0].Currency,
		Provider:     s.provider.Name(),
		Status:       result.Status,
		ClientSecret: result.ClientSecret,
		RedirectURL:  result.RedirectURL,
	}

	err = postgres.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		for i, orderID := range orderIDs {
			q := quotes[i]
			var paymentID uuid.UUID
			if err := tx.QueryRow(ctx, `
				INSERT INTO payments (
					order_id, quote_id, user_id, kind, status, provider,
					provider_ref, amount_minor, currency, failure_code
				) VALUES ($1,$2,$3,'authorization',$4,$5,$6,$7,$8,NULLIF($9,''))
				RETURNING id`,
				orderID, q.ID, userID, mapStatus(result.Status), s.provider.Name(),
				result.ProviderRef, q.AuthorizeMinor, q.Currency, result.FailureCode,
			).Scan(&paymentID); err != nil {
				return fmt.Errorf("insert payment: %w", err)
			}
			if i == 0 {
				intent.ID = paymentID
			}
			if result.Status != "authorized" {
				continue
			}
			if _, err := tx.Exec(ctx,
				`UPDATE price_quotes SET state = 'authorized' WHERE id = $1`, q.ID); err != nil {
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
			if err := insertOutbox(ctx, tx, orderID, "order.confirmed", map[string]any{
				"order_id": orderID, "payment_id": paymentID,
				"amount_minor": q.AuthorizeMinor, "currency": q.Currency,
				"rotation_id": rotationID,
			}); err != nil {
				return err
			}
		}
		if result.Status != "authorized" {
			return nil
		}
		_, err := tx.Exec(ctx, `
			UPDATE meal_rotations SET status = 'active', updated_at = now()
			 WHERE id = $1 AND status = 'pending_payment'`, rotationID)
		return err
	})
	if err != nil {
		return nil, err
	}
	return intent, nil
}
