package ledger

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Subscription money.
//
// A prepaid cycle is the one shape in this system where cash arrives long
// before the service is delivered, and PLAN.md §2.2 named it as a reason the
// ledger had to exist from Phase 1: "prepaid subscription balances (deferred
// revenue)". The `deferred_revenue` and `customer_wallet` account types have
// been in the enum since migration 000001 with nothing posting to them. This is
// the file that finally does.
//
// The shape, in three events:
//
//	charge      Dr gateway receivable      Cr deferred revenue      cash in, nothing earned
//	recognise   Dr deferred revenue        Cr vendor payable        earned, per delivery day
//	                                       Cr platform revenue
//	                                       Cr VAT payable
//	credit      Dr deferred revenue        Cr customer wallet       paused, owed back
//
// The reason this cannot be collapsed into "book it all as revenue on charge"
// is not accounting pedantry. A vendor's payable would jump by a full cycle the
// moment a customer signs up, and a payout run the next day would pay them for
// four weeks of meals they have not cooked. Recovering that from a vendor who
// then cancels is a conversation nobody wins.

// SubscriptionCharge is a prepaid cycle payment.
type SubscriptionCharge struct {
	SubscriptionID uuid.UUID
	CycleID        uuid.UUID
	OrderID        uuid.UUID
	Currency       string
	// AmountMinor is the whole cycle, VAT included.
	AmountMinor int64
}

// PostSubscriptionCharged records cash received against an unearned cycle.
//
// Nothing reaches the vendor here, and nothing reaches revenue. At this instant
// the platform holds money it owes back in full — a customer who cancels the
// next morning is entitled to all of it — and the balance sheet should say so.
func PostSubscriptionCharged(ctx context.Context, tx pgx.Tx, c SubscriptionCharge) (uuid.UUID, error) {
	if c.AmountMinor <= 0 {
		return uuid.Nil, fmt.Errorf("ledger: subscription charge must be positive, got %d", c.AmountMinor)
	}

	return Post(ctx, tx, Journal{
		EventType: "subscription_charged",
		// Referenced by cycle rather than by subscription: a subscription has
		// many cycles and reconciling "what did we collect for the March cycle"
		// against a subscription-level reference means summing and hoping.
		ReferenceType: "subscription_cycle",
		ReferenceID:   c.CycleID,
		Description:   fmt.Sprintf("Prepaid cycle %s", c.CycleID),
		Currency:      c.Currency,
		Postings: []Posting{
			{
				Account:     AccountRef{Type: GatewayReceivable, OwnerType: OwnerPlatform, Currency: c.Currency},
				Direction:   Debit,
				AmountMinor: c.AmountMinor,
			},
			{
				Account:     AccountRef{Type: DeferredRevenue, OwnerType: OwnerPlatform, Currency: c.Currency},
				Direction:   Credit,
				AmountMinor: c.AmountMinor,
			},
		},
	})
}

// SubscriptionRecognition is one day's earned revenue.
type SubscriptionRecognition struct {
	SubscriptionID uuid.UUID
	CycleID        uuid.UUID
	VendorID       uuid.UUID
	Currency       string

	// AmountMinor is the portion of the cycle earned by the deliveries being
	// recognised, VAT included.
	AmountMinor int64
	// CommissionMinor is the platform's cut of AmountMinor.
	CommissionMinor int64
	// CommissionVATMinor is the platform's VAT on its own commission, extracted
	// from CommissionMinor rather than added to it — VAT is displayed
	// tax-inclusive in every GCC market.
	CommissionVATMinor int64
	// SubsidyMinor is the platform-funded promo discount recognised alongside
	// AmountMinor, paid to the kitchen out of platform revenue.
	SubsidyMinor int64
}

// PostSubscriptionRecognised moves earned revenue out of deferred.
//
// The split mirrors PostOrderCaptured exactly, including its open question:
// this treats the **vendor as the supplier of record**, so the VAT the customer
// paid on the meals sits inside vendor_payable and the platform books VAT only
// on its commission. If the platform is a deemed supplier — which changes with
// structure and jurisdiction, and differs between UAE and KSA — both functions
// change together. Flagged in docs/README.md as open decision #4.
//
// Posted per recognition run rather than per meal. A daily job that recognises
// yesterday's deliveries writes one journal per subscription-cycle-day, not one
// per box; the ledger records money movements, and thirty thousand identical
// two-fils entries is a table nobody can query and an audit nobody can read.
func PostSubscriptionRecognised(ctx context.Context, tx pgx.Tx, r SubscriptionRecognition) (uuid.UUID, error) {
	if r.AmountMinor <= 0 {
		return uuid.Nil, fmt.Errorf("ledger: recognition must be positive, got %d", r.AmountMinor)
	}
	if r.SubsidyMinor < 0 {
		return uuid.Nil, fmt.Errorf("ledger: promo subsidy must not be negative, got %d", r.SubsidyMinor)
	}
	// The kitchen earns on what the meals were worth before a platform-funded
	// promo: the customer's money plus the subsidy.
	gross := r.AmountMinor + r.SubsidyMinor
	if r.CommissionMinor > gross {
		return uuid.Nil, fmt.Errorf("ledger: commission %d exceeds recognised %d on cycle %s",
			r.CommissionMinor, gross, r.CycleID)
	}
	if r.CommissionVATMinor > r.CommissionMinor {
		return uuid.Nil, fmt.Errorf("ledger: commission VAT %d exceeds commission %d on cycle %s",
			r.CommissionVATMinor, r.CommissionMinor, r.CycleID)
	}

	vendorID := r.VendorID
	postings := []Posting{
		{
			Account:     AccountRef{Type: DeferredRevenue, OwnerType: OwnerPlatform, Currency: r.Currency},
			Direction:   Debit,
			AmountMinor: r.AmountMinor,
		},
	}

	if vendorNet := gross - r.CommissionMinor; vendorNet > 0 {
		postings = append(postings, Posting{
			Account:     AccountRef{Type: VendorPayable, OwnerType: OwnerVendor, OwnerID: &vendorID, Currency: r.Currency},
			Direction:   Credit,
			AmountMinor: vendorNet,
		})
	}
	// Platform revenue nets the commission against the promo it paid for; a
	// promo bigger than the commission is a debit — the platform's cost.
	if net := r.CommissionMinor - r.CommissionVATMinor - r.SubsidyMinor; net > 0 {
		postings = append(postings, Posting{
			Account:     AccountRef{Type: PlatformRevenue, OwnerType: OwnerPlatform, Currency: r.Currency},
			Direction:   Credit,
			AmountMinor: net,
		})
	} else if net < 0 {
		postings = append(postings, Posting{
			Account:     AccountRef{Type: PlatformRevenue, OwnerType: OwnerPlatform, Currency: r.Currency},
			Direction:   Debit,
			AmountMinor: -net,
		})
	}
	if r.CommissionVATMinor > 0 {
		postings = append(postings, Posting{
			Account:     AccountRef{Type: VATPayable, OwnerType: OwnerPlatform, Currency: r.Currency},
			Direction:   Credit,
			AmountMinor: r.CommissionVATMinor,
		})
	}

	return Post(ctx, tx, Journal{
		EventType:     "subscription_recognised",
		ReferenceType: "subscription_cycle",
		ReferenceID:   r.CycleID,
		Description:   fmt.Sprintf("Revenue recognised for cycle %s", r.CycleID),
		Currency:      r.Currency,
		Postings:      postings,
	})
}

// SubscriptionCredit is a pause credited to the customer's wallet.
type SubscriptionCredit struct {
	SubscriptionID uuid.UUID
	CycleID        uuid.UUID
	UserID         uuid.UUID
	Currency       string
	AmountMinor    int64
	Reason         string
}

// PostSubscriptionCredited moves unearned money from deferred revenue to the
// customer's wallet.
//
// Only ever unearned money. `subscriptions.PauseCredit` bounds the amount by
// what is left unrecognised, because crediting revenue that has already been
// earned by delivering meals is a refund — a different event, with a different
// approval path, that has to reverse the vendor's payable rather than the
// platform's deferred balance.
//
// A wallet credit rather than a card refund because that is what a pause is:
// the customer is not leaving, they are away for a week. Refunding to the card
// costs gateway fees on money that is about to come straight back, and in this
// region a refund can take a week to appear — which reads as the platform
// having taken the money twice.
func PostSubscriptionCredited(ctx context.Context, tx pgx.Tx, c SubscriptionCredit) (uuid.UUID, error) {
	if c.AmountMinor <= 0 {
		return uuid.Nil, fmt.Errorf("ledger: credit must be positive, got %d", c.AmountMinor)
	}

	userID := c.UserID
	reason := c.Reason
	if reason == "" {
		reason = "subscription pause"
	}

	return Post(ctx, tx, Journal{
		EventType:     "subscription_credited",
		ReferenceType: "subscription_cycle",
		ReferenceID:   c.CycleID,
		Description:   fmt.Sprintf("Wallet credit for %s", reason),
		Currency:      c.Currency,
		Postings: []Posting{
			{
				Account:     AccountRef{Type: DeferredRevenue, OwnerType: OwnerPlatform, Currency: c.Currency},
				Direction:   Debit,
				AmountMinor: c.AmountMinor,
			},
			{
				Account:     AccountRef{Type: CustomerWallet, OwnerType: OwnerUser, OwnerID: &userID, Currency: c.Currency},
				Direction:   Credit,
				AmountMinor: c.AmountMinor,
			},
		},
	})
}

// PostSubscriptionCancelled returns the unearned remainder of a cycle.
//
// The mirror of a charge, for the portion never delivered. A cancellation
// mid-cycle leaves deferred revenue holding money for meals that will not be
// cooked, and that balance has to go somewhere explicit — leaving it to age
// quietly in deferred revenue is how a liability account stops meaning
// anything.
func PostSubscriptionCancelled(ctx context.Context, tx pgx.Tx, c SubscriptionCredit) (uuid.UUID, error) {
	if c.AmountMinor <= 0 {
		return uuid.Nil, fmt.Errorf("ledger: cancellation refund must be positive, got %d", c.AmountMinor)
	}

	userID := c.UserID
	return Post(ctx, tx, Journal{
		EventType:     "subscription_cancelled",
		ReferenceType: "subscription_cycle",
		ReferenceID:   c.CycleID,
		Description:   fmt.Sprintf("Unearned remainder returned on cancelling %s", c.SubscriptionID),
		Currency:      c.Currency,
		Postings: []Posting{
			{
				Account:     AccountRef{Type: DeferredRevenue, OwnerType: OwnerPlatform, Currency: c.Currency},
				Direction:   Debit,
				AmountMinor: c.AmountMinor,
			},
			{
				Account:     AccountRef{Type: CustomerWallet, OwnerType: OwnerUser, OwnerID: &userID, Currency: c.Currency},
				Direction:   Credit,
				AmountMinor: c.AmountMinor,
			},
		},
	})
}

// PostCompanySubscriptionCancelled returns the unearned remainder of an office
// lunch cycle to the company that paid for it. The company settles by
// statement, so the credit reduces what the platform is owed rather than
// landing in the employee's wallet.
func PostCompanySubscriptionCancelled(ctx context.Context, tx pgx.Tx, c SubscriptionCredit) (uuid.UUID, error) {
	if c.AmountMinor <= 0 {
		return uuid.Nil, fmt.Errorf("ledger: cancellation refund must be positive, got %d", c.AmountMinor)
	}
	return Post(ctx, tx, Journal{
		EventType:     "subscription_cancelled",
		ReferenceType: "subscription_cycle",
		ReferenceID:   c.CycleID,
		Description:   fmt.Sprintf("Unearned remainder credited to the company on cancelling %s", c.SubscriptionID),
		Currency:      c.Currency,
		Postings: []Posting{
			{
				Account:     AccountRef{Type: DeferredRevenue, OwnerType: OwnerPlatform, Currency: c.Currency},
				Direction:   Debit,
				AmountMinor: c.AmountMinor,
			},
			{
				Account:     AccountRef{Type: GatewayReceivable, OwnerType: OwnerPlatform, Currency: c.Currency},
				Direction:   Credit,
				AmountMinor: c.AmountMinor,
			},
		},
	})
}
