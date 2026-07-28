package ledger

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Listing-fee money.
//
// The classifieds revenue model, and the first money in this system that does
// not pass through a vendor. There is no GMV to take a percentage of: the car
// is sold at an RTA counter the platform never sees, so the platform charges
// for the listing itself.
//
// The shape, in three events:
//
//	charge      Dr gateway receivable      Cr deferred revenue    cash in, nothing earned
//	                                       Cr VAT payable
//	recognise   Dr deferred revenue        Cr platform revenue    earned, per day of the window
//	refund      Dr deferred revenue        Cr customer wallet     unearned portion returned
//	            Dr VAT payable
//
// Two differences from PostSubscriptionRecognised are worth reading carefully,
// because they are the *only* two and everything else about this ledger is
// unchanged.
//
// **No vendor payable.** Not "a vendor payable of zero" — no posting at all.
// Nobody supplied anything but the platform. A payout run must never see these
// journals, which is why they reference `package_purchase` and never an order.
//
// **VAT is booked at charge, not at recognition.** In a subscription the vendor
// is the supplier of record and the platform's own VAT is on its commission,
// which is earned as the meals are delivered. Here the platform *is* the
// supplier, and UAE VAT on a supply is due at the earlier of invoice or
// payment — which is the moment the seller pays for the package, regardless of
// how much of the 30 days has run. Deferring it to match revenue recognition
// would understate the output tax on every return filed mid-window.

// ListingFeeCharge is a package purchase being paid for.
type ListingFeeCharge struct {
	PurchaseID uuid.UUID
	BuyerID    uuid.UUID
	Currency   string
	// NetMinor and TaxMinor sum to what the seller pays. Split by the caller
	// because the tax rate is a property of the package, and re-deriving it here
	// from a gross figure is how a rounding difference gets introduced between
	// the invoice and the ledger.
	NetMinor int64
	TaxMinor int64
}

func PostListingFeeCharged(ctx context.Context, tx pgx.Tx, c ListingFeeCharge) (uuid.UUID, error) {
	if c.NetMinor <= 0 {
		return uuid.Nil, fmt.Errorf("ledger: listing fee must be positive, got %d", c.NetMinor)
	}
	if c.TaxMinor < 0 {
		return uuid.Nil, fmt.Errorf("ledger: listing fee tax cannot be negative, got %d", c.TaxMinor)
	}

	postings := []Posting{
		{
			Account:     AccountRef{Type: GatewayReceivable, OwnerType: OwnerPlatform, Currency: c.Currency},
			Direction:   Debit,
			AmountMinor: c.NetMinor + c.TaxMinor,
		},
		{
			// Deferred, not revenue. On the day a seller buys a 30-day package
			// the platform has delivered nothing: a seller who cancels that
			// evening is owed all of it, and the balance sheet should say so.
			Account:     AccountRef{Type: DeferredRevenue, OwnerType: OwnerPlatform, Currency: c.Currency},
			Direction:   Credit,
			AmountMinor: c.NetMinor,
		},
	}
	if c.TaxMinor > 0 {
		postings = append(postings, Posting{
			Account:     AccountRef{Type: VATPayable, OwnerType: OwnerPlatform, Currency: c.Currency},
			Direction:   Credit,
			AmountMinor: c.TaxMinor,
		})
	}

	return Post(ctx, tx, Journal{
		EventType: "listing_fee_charged",
		// Referenced by purchase, never by listing. One purchase funds many
		// listings, and reconciling "what did we collect for classifieds in
		// March" against a listing-level reference means summing and hoping.
		ReferenceType: "package_purchase",
		ReferenceID:   c.PurchaseID,
		Description:   fmt.Sprintf("listing package purchase %s", c.PurchaseID),
		Currency:      c.Currency,
		Postings:      postings,
	})
}

// ListingFeeRecognition is one day's worth of an entitlement window.
type ListingFeeRecognition struct {
	PurchaseID  uuid.UUID
	Currency    string
	AmountMinor int64
}

// PostListingFeeRecognised moves earned listing revenue out of deferred.
//
// Net only. The VAT was settled at charge and moving it again here would credit
// the same output tax twice — the mistake this file's header comment exists to
// prevent, and the one that is invisible until a VAT return is filed.
func PostListingFeeRecognised(ctx context.Context, tx pgx.Tx, r ListingFeeRecognition) (uuid.UUID, error) {
	if r.AmountMinor <= 0 {
		return uuid.Nil, fmt.Errorf("ledger: recognition must be positive, got %d", r.AmountMinor)
	}

	return Post(ctx, tx, Journal{
		EventType:     "listing_fee_recognised",
		ReferenceType: "package_purchase",
		ReferenceID:   r.PurchaseID,
		Description:   fmt.Sprintf("listing package %s earned", r.PurchaseID),
		Currency:      r.Currency,
		Postings: []Posting{
			{
				Account:     AccountRef{Type: DeferredRevenue, OwnerType: OwnerPlatform, Currency: r.Currency},
				Direction:   Debit,
				AmountMinor: r.AmountMinor,
			},
			{
				// Straight to platform revenue. There is no vendor split
				// because there is no vendor — the whole fee is the platform's.
				Account:     AccountRef{Type: PlatformRevenue, OwnerType: OwnerPlatform, Currency: r.Currency},
				Direction:   Credit,
				AmountMinor: r.AmountMinor,
			},
		},
	})
}

// ListingFeeRefund returns the unearned part of a package.
type ListingFeeRefund struct {
	PurchaseID uuid.UUID
	BuyerID    uuid.UUID
	Currency   string
	NetMinor   int64
	TaxMinor   int64
}

// PostListingFeeRefunded credits the seller for entitlement they will not get.
//
// To the wallet, not to the gateway. A refund to the original card is a
// different operation with a different failure mode and a provider round-trip;
// this posting records the obligation the moment it arises, and the payout path
// settles it. The VAT debit reverses the output tax on the portion being
// returned — a refund that left the VAT credited would have the platform paying
// tax on money it gave back.
func PostListingFeeRefunded(ctx context.Context, tx pgx.Tx, r ListingFeeRefund) (uuid.UUID, error) {
	if r.NetMinor <= 0 {
		return uuid.Nil, fmt.Errorf("ledger: refund must be positive, got %d", r.NetMinor)
	}

	postings := []Posting{
		{
			Account:     AccountRef{Type: DeferredRevenue, OwnerType: OwnerPlatform, Currency: r.Currency},
			Direction:   Debit,
			AmountMinor: r.NetMinor,
		},
		{
			Account: AccountRef{
				Type: CustomerWallet, OwnerType: OwnerUser, OwnerID: &r.BuyerID, Currency: r.Currency,
			},
			Direction:   Credit,
			AmountMinor: r.NetMinor + r.TaxMinor,
		},
	}
	if r.TaxMinor > 0 {
		postings = append(postings, Posting{
			Account:     AccountRef{Type: VATPayable, OwnerType: OwnerPlatform, Currency: r.Currency},
			Direction:   Debit,
			AmountMinor: r.TaxMinor,
		})
	}

	return Post(ctx, tx, Journal{
		EventType:     "listing_fee_refunded",
		ReferenceType: "package_purchase",
		ReferenceID:   r.PurchaseID,
		Description:   fmt.Sprintf("listing package %s unearned remainder", r.PurchaseID),
		Currency:      r.Currency,
		Postings:      postings,
	})
}
