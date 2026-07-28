package ledger

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// CaptureSplit is the money breakdown for one captured order.
type CaptureSplit struct {
	OrderID  uuid.UUID
	VendorID uuid.UUID
	Currency string

	// TotalMinor is what the customer was charged, VAT included.
	TotalMinor int64
	// CommissionMinor is the platform's cut of TotalMinor.
	CommissionMinor int64
	// CommissionVATMinor is the platform's own VAT on its commission, extracted
	// from CommissionMinor rather than added to it.
	CommissionVATMinor int64
}

// PostOrderCaptured records a successful capture.
//
//	Dr  gateway receivable      total          money owed to us by the PSP
//	Cr  vendor payable          total - commission
//	Cr  platform revenue        commission - commission VAT
//	Cr  VAT payable             commission VAT
//
// Scope note on VAT, and it is a real open question rather than an oversight:
// this treats the **vendor as the supplier of record**. The VAT the customer
// paid on the service sits inside vendor_payable because the vendor remits it;
// the platform only books VAT on its own commission. If the platform is instead
// a deemed supplier — which changes with structure and jurisdiction, and
// differs between UAE and KSA — the split changes and this function does too.
// It is flagged in docs/README.md as open decision #4 (commission model).
//
// Nothing is posted at authorization. An authorization is a hold, not a
// movement of money, and booking revenue on it would overstate every reporting
// period by the value of everything that was later voided.
func PostOrderCaptured(ctx context.Context, tx pgx.Tx, s CaptureSplit) (uuid.UUID, error) {
	if s.CommissionMinor > s.TotalMinor {
		return uuid.Nil, fmt.Errorf("ledger: commission %d exceeds total %d on order %s",
			s.CommissionMinor, s.TotalMinor, s.OrderID)
	}
	if s.CommissionVATMinor > s.CommissionMinor {
		return uuid.Nil, fmt.Errorf("ledger: commission VAT %d exceeds commission %d on order %s",
			s.CommissionVATMinor, s.CommissionMinor, s.OrderID)
	}

	vendorID := s.VendorID
	postings := []Posting{
		{
			Account:     AccountRef{Type: GatewayReceivable, OwnerType: OwnerPlatform, Currency: s.Currency},
			Direction:   Debit,
			AmountMinor: s.TotalMinor,
		},
	}

	// Every component is omitted when it rounds to zero. The schema rejects
	// zero-amount entries — correctly, since a zero entry carries no
	// information — and all three of these legitimately reach zero: a
	// zero-commission launch partner, a fully-subsidised order, or a
	// jurisdiction with no VAT on platform fees.
	if vendorNet := s.TotalMinor - s.CommissionMinor; vendorNet > 0 {
		postings = append(postings, Posting{
			Account:     AccountRef{Type: VendorPayable, OwnerType: OwnerVendor, OwnerID: &vendorID, Currency: s.Currency},
			Direction:   Credit,
			AmountMinor: vendorNet,
		})
	}
	if net := s.CommissionMinor - s.CommissionVATMinor; net > 0 {
		postings = append(postings, Posting{
			Account:     AccountRef{Type: PlatformRevenue, OwnerType: OwnerPlatform, Currency: s.Currency},
			Direction:   Credit,
			AmountMinor: net,
		})
	}
	if s.CommissionVATMinor > 0 {
		postings = append(postings, Posting{
			Account:     AccountRef{Type: VATPayable, OwnerType: OwnerPlatform, Currency: s.Currency},
			Direction:   Credit,
			AmountMinor: s.CommissionVATMinor,
		})
	}

	return Post(ctx, tx, Journal{
		EventType:     "order_captured",
		ReferenceType: "order",
		ReferenceID:   s.OrderID,
		Description:   fmt.Sprintf("Capture for order %s", s.OrderID),
		Currency:      s.Currency,
		Postings:      postings,
	})
}

// PostOrderRefunded reverses a capture, in full or in part.
//
// A new journal, never an edit — ledger_entries is append-only by trigger, and
// a refund is a real event with its own date that reporting must be able to see
// separately from the original sale.
func PostOrderRefunded(ctx context.Context, tx pgx.Tx, s CaptureSplit) (uuid.UUID, error) {
	vendorID := s.VendorID
	postings := []Posting{
		{
			Account:     AccountRef{Type: GatewayReceivable, OwnerType: OwnerPlatform, Currency: s.Currency},
			Direction:   Credit,
			AmountMinor: s.TotalMinor,
		},
	}

	if vendorNet := s.TotalMinor - s.CommissionMinor; vendorNet > 0 {
		postings = append(postings, Posting{
			Account:     AccountRef{Type: VendorPayable, OwnerType: OwnerVendor, OwnerID: &vendorID, Currency: s.Currency},
			Direction:   Debit,
			AmountMinor: vendorNet,
		})
	}

	if net := s.CommissionMinor - s.CommissionVATMinor; net > 0 {
		postings = append(postings, Posting{
			Account:     AccountRef{Type: PlatformRevenue, OwnerType: OwnerPlatform, Currency: s.Currency},
			Direction:   Debit,
			AmountMinor: net,
		})
	}
	if s.CommissionVATMinor > 0 {
		postings = append(postings, Posting{
			Account:     AccountRef{Type: VATPayable, OwnerType: OwnerPlatform, Currency: s.Currency},
			Direction:   Debit,
			AmountMinor: s.CommissionVATMinor,
		})
	}

	return Post(ctx, tx, Journal{
		EventType:     "order_refunded",
		ReferenceType: "order",
		ReferenceID:   s.OrderID,
		Description:   fmt.Sprintf("Refund for order %s", s.OrderID),
		Currency:      s.Currency,
		Postings:      postings,
	})
}
