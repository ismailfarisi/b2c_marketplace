package ledger

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Cash, and paying vendors.
//
// `driver_float` has been in the ledger_account_type enum since migration 000001
// with nothing posting to it — the same situation `deferred_revenue` was in
// before Phase 5. This is the file that finally uses it, and it is worth being
// precise about what the account *means*, because a float account with an
// unexplained balance is worse than no float account:
//
//	the balance of a driver's float account is exactly the cash they are
//	holding that the platform has not received.
//
// Everything below follows from keeping that sentence true.
//
//	collect   Dr driver float        Cr vendor payable       cash taken at the door
//	                                 Cr platform revenue
//	                                 Cr VAT payable
//	remit     Dr platform cash       Cr driver float         cash handed in
//	write off Dr platform revenue    Cr driver float          cash that will not arrive
//	pay out   Dr vendor payable      Cr platform cash        the vendor is paid
//
// Note that a *shortfall* has no posting of its own. A driver who declares 900
// and hands over 850 gets a remittance of 850, and the 50 stays in their float —
// which is correct, because they are still holding it, and the account still
// means what the sentence above says. Only a decision to stop pursuing it moves
// the money, and that is the write-off.

// CODCollection is cash taken at the door, split the same way a card capture is.
//
// The split is identical to PostOrderCaptured because it *is* the same economic
// event — the vendor sold something, the platform earned commission, VAT is due.
// Only the debit differs: the money is in a driver's bag rather than at a
// gateway. Duplicating the split rather than sharing it with PostOrderCaptured is
// deliberate; the two will diverge the moment cash acquires a handling fee, and
// discovering that through a shared function is how one of them silently gets
// the other's rules.
type CODCollection struct {
	FulfillmentID uuid.UUID
	OrderID       uuid.UUID
	DriverID      uuid.UUID
	VendorID      uuid.UUID
	Currency      string

	CollectedMinor     int64
	CommissionMinor    int64
	CommissionVATMinor int64
}

func PostCODCollected(ctx context.Context, tx pgx.Tx, c CODCollection) (uuid.UUID, error) {
	if c.CollectedMinor <= 0 {
		return uuid.Nil, fmt.Errorf("ledger: cod collection must be positive, got %d", c.CollectedMinor)
	}
	if c.CommissionMinor > c.CollectedMinor {
		return uuid.Nil, fmt.Errorf("ledger: commission %d exceeds cash collected %d on fulfillment %s",
			c.CommissionMinor, c.CollectedMinor, c.FulfillmentID)
	}
	if c.CommissionVATMinor > c.CommissionMinor {
		return uuid.Nil, fmt.Errorf("ledger: commission VAT %d exceeds commission %d",
			c.CommissionVATMinor, c.CommissionMinor)
	}

	driverID, vendorID := c.DriverID, c.VendorID
	postings := []Posting{
		{
			Account: AccountRef{
				Type: DriverFloat, OwnerType: OwnerDriver, OwnerID: &driverID, Currency: c.Currency,
			},
			Direction:   Debit,
			AmountMinor: c.CollectedMinor,
		},
	}

	// Same omit-when-zero rule as PostOrderCaptured: the schema rejects
	// zero-amount entries, and all three of these legitimately reach zero.
	if vendorNet := c.CollectedMinor - c.CommissionMinor; vendorNet > 0 {
		postings = append(postings, Posting{
			Account: AccountRef{
				Type: VendorPayable, OwnerType: OwnerVendor, OwnerID: &vendorID, Currency: c.Currency,
			},
			Direction:   Credit,
			AmountMinor: vendorNet,
		})
	}
	if net := c.CommissionMinor - c.CommissionVATMinor; net > 0 {
		postings = append(postings, Posting{
			Account:     AccountRef{Type: PlatformRevenue, OwnerType: OwnerPlatform, Currency: c.Currency},
			Direction:   Credit,
			AmountMinor: net,
		})
	}
	if c.CommissionVATMinor > 0 {
		postings = append(postings, Posting{
			Account:     AccountRef{Type: VATPayable, OwnerType: OwnerPlatform, Currency: c.Currency},
			Direction:   Credit,
			AmountMinor: c.CommissionVATMinor,
		})
	}

	return Post(ctx, tx, Journal{
		EventType: "cod_collected",
		// Referenced by fulfillment, not by order. An order with a pickup and a
		// delivery can in principle collect at either, and "which visit was the
		// money taken on" is the question a cash dispute turns on.
		ReferenceType: "fulfillment",
		ReferenceID:   c.FulfillmentID,
		Description:   fmt.Sprintf("Cash collected on fulfillment %s", c.FulfillmentID),
		Currency:      c.Currency,
		Postings:      postings,
	})
}

// CODRemittance is a driver handing cash in.
type CODRemittance struct {
	RemittanceID uuid.UUID
	DriverID     uuid.UUID
	Currency     string
	// CountedMinor is what was actually received and counted, never what the
	// driver declared. Posting the declared figure would make the float say the
	// platform holds money it does not have, which is the one thing this account
	// exists to get right.
	CountedMinor int64
}

func PostCODRemitted(ctx context.Context, tx pgx.Tx, r CODRemittance) (uuid.UUID, error) {
	if r.CountedMinor <= 0 {
		return uuid.Nil, fmt.Errorf("ledger: remittance must be positive, got %d", r.CountedMinor)
	}

	driverID := r.DriverID
	return Post(ctx, tx, Journal{
		EventType:     "cod_remitted",
		ReferenceType: "cod_remittance",
		ReferenceID:   r.RemittanceID,
		Description:   fmt.Sprintf("Cash remitted, remittance %s", r.RemittanceID),
		Currency:      r.Currency,
		Postings: []Posting{
			{
				Account:     AccountRef{Type: PlatformCash, OwnerType: OwnerPlatform, Currency: r.Currency},
				Direction:   Debit,
				AmountMinor: r.CountedMinor,
			},
			{
				Account: AccountRef{
					Type: DriverFloat, OwnerType: OwnerDriver, OwnerID: &driverID, Currency: r.Currency,
				},
				Direction:   Credit,
				AmountMinor: r.CountedMinor,
			},
		},
	})
}

// CODWriteOff is a decision to stop pursuing a shortfall.
type CODWriteOff struct {
	RemittanceID uuid.UUID
	DriverID     uuid.UUID
	Currency     string
	AmountMinor  int64
}

// PostCODWrittenOff clears cash that will not arrive.
//
// Debited to platform revenue, which is contra-revenue rather than the bad-debt
// *expense* an accountant would want. The chart of accounts in migration 000001
// has no expense accounts at all — no COGS, no operating expenses — so
// contra-revenue is the internally consistent choice rather than the correct
// one. Adding `bad_debt_expense` to `ledger_account_type` is a small migration
// and a real chart-of-accounts decision, which is tied to docs/README.md open
// decision #4 and is not this phase's to take. Flagged in
// docs/PHASE-7-CLIENTS-AND-LOGISTICS.md §8 rather than quietly assumed.
func PostCODWrittenOff(ctx context.Context, tx pgx.Tx, w CODWriteOff) (uuid.UUID, error) {
	if w.AmountMinor <= 0 {
		return uuid.Nil, fmt.Errorf("ledger: write-off must be positive, got %d", w.AmountMinor)
	}

	driverID := w.DriverID
	return Post(ctx, tx, Journal{
		EventType:     "cod_written_off",
		ReferenceType: "cod_remittance",
		ReferenceID:   w.RemittanceID,
		Description:   fmt.Sprintf("Cash shortfall written off, remittance %s", w.RemittanceID),
		Currency:      w.Currency,
		Postings: []Posting{
			{
				Account:     AccountRef{Type: PlatformRevenue, OwnerType: OwnerPlatform, Currency: w.Currency},
				Direction:   Debit,
				AmountMinor: w.AmountMinor,
			},
			{
				Account: AccountRef{
					Type: DriverFloat, OwnerType: OwnerDriver, OwnerID: &driverID, Currency: w.Currency,
				},
				Direction:   Credit,
				AmountMinor: w.AmountMinor,
			},
		},
	})
}

// VendorPayout is a statement being paid.
type VendorPayout struct {
	StatementID uuid.UUID
	VendorID    uuid.UUID
	Currency    string
	NetMinor    int64
}

// PostVendorPaid settles a payout statement.
//
// Nothing is posted when a statement is *issued*, only when it is paid. A
// statement is a derived view of `vendor_payable` for a period — issuing one
// creates no obligation that did not already exist, and posting on issue would
// double-count every payable it summarises.
func PostVendorPaid(ctx context.Context, tx pgx.Tx, p VendorPayout) (uuid.UUID, error) {
	if p.NetMinor <= 0 {
		return uuid.Nil, fmt.Errorf("ledger: payout must be positive, got %d", p.NetMinor)
	}

	vendorID := p.VendorID
	return Post(ctx, tx, Journal{
		EventType:     "vendor_paid",
		ReferenceType: "payout_statement",
		ReferenceID:   p.StatementID,
		Description:   fmt.Sprintf("Payout for statement %s", p.StatementID),
		Currency:      p.Currency,
		Postings: []Posting{
			{
				Account: AccountRef{
					Type: VendorPayable, OwnerType: OwnerVendor, OwnerID: &vendorID, Currency: p.Currency,
				},
				Direction:   Debit,
				AmountMinor: p.NetMinor,
			},
			{
				Account:     AccountRef{Type: PlatformCash, OwnerType: OwnerPlatform, Currency: p.Currency},
				Direction:   Credit,
				AmountMinor: p.NetMinor,
			},
		},
	})
}
