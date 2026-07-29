// Package ledger posts double-entry journals.
//
// Nothing outside this package writes to ledger_entries. Every caller describes
// what happened in business terms and this package decides which accounts move.
// That is what keeps the chart of accounts from being reinvented slightly
// differently in the payments module and the payouts module.
package ledger

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type Direction string

const (
	Debit  Direction = "debit"
	Credit Direction = "credit"
)

type AccountType string

const (
	PlatformCash      AccountType = "platform_cash"
	VendorPayable     AccountType = "vendor_payable"
	DeferredRevenue   AccountType = "deferred_revenue"
	PlatformRevenue   AccountType = "platform_revenue"
	VATPayable        AccountType = "vat_payable"
	DriverFloat       AccountType = "driver_float"
	CustomerWallet    AccountType = "customer_wallet"
	GatewayReceivable AccountType = "payment_gateway_receivable"
)

type OwnerType string

const (
	OwnerPlatform OwnerType = "platform"
	OwnerVendor   OwnerType = "vendor"
	OwnerUser     OwnerType = "user"
	OwnerDriver   OwnerType = "driver"
)

// AccountRef identifies an account without the caller needing its UUID.
type AccountRef struct {
	Type      AccountType
	OwnerType OwnerType
	OwnerID   *uuid.UUID
	Currency  string
}

type Posting struct {
	Account     AccountRef
	Direction   Direction
	AmountMinor int64
}

type Journal struct {
	EventType     string
	ReferenceType string
	ReferenceID   uuid.UUID
	Description   string
	Currency      string
	Postings      []Posting
}

var ErrUnbalanced = errors.New("ledger: journal is unbalanced")

// Post writes a journal and its entries inside the caller's transaction.
//
// Taking a pgx.Tx rather than a pool is the point: a ledger entry that commits
// separately from the state change it records is worse than no ledger at all,
// because it looks authoritative while disagreeing with reality.
func Post(ctx context.Context, tx pgx.Tx, j Journal) (uuid.UUID, error) {
	if err := j.validate(); err != nil {
		return uuid.Nil, err
	}

	var journalID uuid.UUID
	err := tx.QueryRow(ctx, `
		INSERT INTO ledger_journals (event_type, reference_type, reference_id, description, currency)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id`,
		j.EventType, j.ReferenceType, j.ReferenceID, j.Description, j.Currency,
	).Scan(&journalID)
	if err != nil {
		return uuid.Nil, fmt.Errorf("insert journal: %w", err)
	}

	for _, p := range j.Postings {
		accountID, err := resolveAccount(ctx, tx, p.Account)
		if err != nil {
			return uuid.Nil, err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO ledger_entries (journal_id, account_id, direction, amount_minor, currency)
			VALUES ($1, $2, $3, $4, $5)`,
			journalID, accountID, p.Direction, p.AmountMinor, p.Account.Currency,
		); err != nil {
			return uuid.Nil, fmt.Errorf("insert entry: %w", err)
		}
	}

	// The database re-checks balance at COMMIT via a deferred constraint
	// trigger. This check is here to fail early with a message that names the
	// business event, which is far more useful during development than a
	// constraint violation surfacing at commit time with no context.
	return journalID, nil
}

func (j Journal) validate() error {
	if len(j.Postings) < 2 {
		return fmt.Errorf("%w: %s has %d postings, need at least 2",
			ErrUnbalanced, j.EventType, len(j.Postings))
	}

	var debits, credits int64
	for _, p := range j.Postings {
		if p.AmountMinor <= 0 {
			return fmt.Errorf("ledger: %s has a non-positive posting (%d) — express direction with Debit/Credit, not a sign",
				j.EventType, p.AmountMinor)
		}
		if p.Account.Currency != j.Currency {
			return fmt.Errorf("ledger: %s mixes %s and %s in one journal",
				j.EventType, j.Currency, p.Account.Currency)
		}
		switch p.Direction {
		case Debit:
			debits += p.AmountMinor
		case Credit:
			credits += p.AmountMinor
		default:
			return fmt.Errorf("ledger: unknown direction %q", p.Direction)
		}
	}

	if debits != credits {
		return fmt.Errorf("%w: %s debits %d, credits %d",
			ErrUnbalanced, j.EventType, debits, credits)
	}
	return nil
}

// resolveAccount finds or creates the account.
//
// Lazy creation is why a new vendor needs no provisioning step. The
// ON CONFLICT handles two concurrent first-orders for the same vendor, which is
// otherwise a rare and confusing unique-violation.
//
// The `WHERE is_active` on the conflict target is load-bearing and names the
// bug it fixes (migration 22). This upsert used to bind to a plain UNIQUE
// constraint over the same four columns — and Postgres treats NULLs as distinct
// in a UNIQUE, so for platform-level accounts, which have no owner_id, the
// conflict never fired. Every capture, refund and cash collection created a
// fresh account holding one entry, and platform revenue became unsummable.
//
// The index it now targets is `NULLS NOT DISTINCT` and partial on `is_active`,
// which is the rule stated properly: one live account per identity, and a
// missing owner is one identity rather than infinitely many.
func resolveAccount(ctx context.Context, tx pgx.Tx, ref AccountRef) (uuid.UUID, error) {
	var id uuid.UUID

	err := tx.QueryRow(ctx, `
		INSERT INTO ledger_accounts (type, owner_type, owner_id, currency, name)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (type, owner_type, owner_id, currency) WHERE is_active DO UPDATE
		    SET type = EXCLUDED.type
		RETURNING id`,
		ref.Type, ref.OwnerType, ref.OwnerID, ref.Currency, accountName(ref),
	).Scan(&id)
	if err != nil {
		return uuid.Nil, fmt.Errorf("resolve account %s/%s: %w", ref.Type, ref.OwnerType, err)
	}
	return id, nil
}

func accountName(ref AccountRef) string {
	if ref.OwnerID == nil {
		return fmt.Sprintf("%s (%s)", ref.Type, ref.Currency)
	}
	return fmt.Sprintf("%s %s (%s)", ref.Type, ref.OwnerID.String()[:8], ref.Currency)
}

// Balance sums an account. Debits are positive.
func Balance(ctx context.Context, tx pgx.Tx, ref AccountRef) (int64, error) {
	var balance int64
	err := tx.QueryRow(ctx, `
		SELECT COALESCE(SUM(
		           CASE WHEN e.direction = 'debit' THEN e.amount_minor
		                ELSE -e.amount_minor END), 0)
		  FROM ledger_entries e
		  JOIN ledger_accounts a ON a.id = e.account_id
		 WHERE a.type = $1
		   AND a.owner_type IS NOT DISTINCT FROM $2
		   AND a.owner_id IS NOT DISTINCT FROM $3
		   AND a.currency = $4`,
		ref.Type, ref.OwnerType, ref.OwnerID, ref.Currency,
	).Scan(&balance)
	if err != nil {
		return 0, fmt.Errorf("balance: %w", err)
	}
	return balance, nil
}
