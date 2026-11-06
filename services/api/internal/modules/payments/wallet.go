package payments

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// WalletProvider is the "provider" recorded on a payment settled entirely from
// the customer's wallet credit, with no gateway involved.
const WalletProvider = "customer_wallet"

// AuthorizeFromWallet confirms an order whose whole price is covered by the
// customer's wallet credit. `charge` runs inside the transaction and must move
// the money in the ledger (wallet → the order's deferred revenue); it fails the
// authorization by returning an error, which is how an insufficient balance is
// refused without a payment row ever being written.
//
// Same shape as AuthorizeOnAccount: a payment row in 'succeeded', the quote
// advanced, the order confirmed and any subscription cycle behind it activated.
func (s *Service) AuthorizeFromWallet(ctx context.Context, userID, orderID uuid.UUID,
	charge func(ctx context.Context, tx pgx.Tx, amountMinor int64, currency string) error) error {
	_, err := s.authorizeOffGateway(ctx, userID, orderID, WalletProvider, "wallet:"+userID.String(), AccountCharge(charge))
	return err
}
