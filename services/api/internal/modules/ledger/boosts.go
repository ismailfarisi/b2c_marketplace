package ledger

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Boost money.
//
// The third revenue stream (PLAN-VIDEO-FIRST §7) and the second one where the
// platform is unambiguously the supplier. Its shape is the listing-fee shape,
// which is not a coincidence — both sell platform inventory rather than take a
// cut of somebody else's supply:
//
//	funded   Dr gateway receivable     Cr deferred revenue    prepaid, nothing delivered
//	                                   Cr VAT payable
//	spend    Dr deferred revenue       Cr platform revenue    impressions delivered
//	refund   Dr deferred revenue       Cr customer wallet     unspent balance returned
//	         Dr VAT payable
//
// **No vendor payable, ever.** Not a payable of zero — no posting. A payout run
// must never see these journals, which is why they reference `boost_campaign`
// and never an order.
//
// **VAT is booked when the campaign is funded, not when impressions deliver.**
// The same rule as listing fees, and for the same reason: UAE VAT on a supply
// is due at the earlier of invoice or payment, and the advertiser pays up front.
// Deferring the output tax to match delivery would understate it on every
// return filed mid-campaign.

// BoostFunding is a prepayment into a campaign.
type BoostFunding struct {
	CampaignID     uuid.UUID
	AdvertiserType string
	AdvertiserID   uuid.UUID
	Currency       string
	// Split by the caller, for the same reason the listing fee is: re-deriving
	// the tax here from a gross figure is how a fils of difference appears
	// between the invoice and the ledger.
	NetMinor int64
	TaxMinor int64
}

func PostBoostFunded(ctx context.Context, tx pgx.Tx, f BoostFunding) (uuid.UUID, error) {
	if f.NetMinor <= 0 {
		return uuid.Nil, fmt.Errorf("ledger: boost funding must be positive, got %d", f.NetMinor)
	}
	if f.TaxMinor < 0 {
		return uuid.Nil, fmt.Errorf("ledger: boost tax cannot be negative, got %d", f.TaxMinor)
	}

	postings := []Posting{
		{
			Account:     AccountRef{Type: GatewayReceivable, OwnerType: OwnerPlatform, Currency: f.Currency},
			Direction:   Debit,
			AmountMinor: f.NetMinor + f.TaxMinor,
		},
		{
			// Deferred. An advertiser who funds a campaign and pauses it that
			// evening is owed all of it, and the balance sheet should say so.
			Account:     AccountRef{Type: DeferredRevenue, OwnerType: OwnerPlatform, Currency: f.Currency},
			Direction:   Credit,
			AmountMinor: f.NetMinor,
		},
	}
	if f.TaxMinor > 0 {
		postings = append(postings, Posting{
			Account:     AccountRef{Type: VATPayable, OwnerType: OwnerPlatform, Currency: f.Currency},
			Direction:   Credit,
			AmountMinor: f.TaxMinor,
		})
	}

	return Post(ctx, tx, Journal{
		EventType:     "boost_funded",
		ReferenceType: "boost_campaign",
		ReferenceID:   f.CampaignID,
		Description:   fmt.Sprintf("campaign %s funded", f.CampaignID),
		Currency:      f.Currency,
		Postings:      postings,
	})
}

// BoostSpend is one billing period's delivered impressions.
type BoostSpend struct {
	CampaignID     uuid.UUID
	AdvertiserType string
	AdvertiserID   uuid.UUID
	Currency       string
	AmountMinor    int64
}

// PostBoostSpend recognises delivered reach.
//
// Net only. The VAT was settled at funding and crediting it again here would
// book the same output tax twice — the mistake `listings.go` exists to warn
// about, repeated in a second place because it is invisible until a VAT return
// is filed.
func PostBoostSpend(ctx context.Context, tx pgx.Tx, s BoostSpend) (uuid.UUID, error) {
	if s.AmountMinor <= 0 {
		return uuid.Nil, fmt.Errorf("ledger: boost spend must be positive, got %d", s.AmountMinor)
	}

	return Post(ctx, tx, Journal{
		EventType:     "boost_spend",
		ReferenceType: "boost_campaign",
		ReferenceID:   s.CampaignID,
		Description:   fmt.Sprintf("campaign %s impressions delivered", s.CampaignID),
		Currency:      s.Currency,
		Postings: []Posting{
			{
				Account:     AccountRef{Type: DeferredRevenue, OwnerType: OwnerPlatform, Currency: s.Currency},
				Direction:   Debit,
				AmountMinor: s.AmountMinor,
			},
			{
				// Straight to platform revenue. No vendor split, because
				// nobody but the platform supplied anything.
				Account:     AccountRef{Type: PlatformRevenue, OwnerType: OwnerPlatform, Currency: s.Currency},
				Direction:   Credit,
				AmountMinor: s.AmountMinor,
			},
		},
	})
}

// BoostRefund returns an unspent balance.
type BoostRefund struct {
	CampaignID     uuid.UUID
	AdvertiserType string
	AdvertiserID   uuid.UUID
	Currency       string
	NetMinor       int64
	TaxMinor       int64
}

// PostBoostRefunded returns what a cancelled campaign never spent.
//
// To the wallet rather than to the card. Advertisers cancel and re-fund
// constantly, and routing every unspent 40 AED back through a gateway refund
// costs a fee and a week for money that is about to be spent on the next
// campaign. An advertiser who wants it off the platform asks, and that is a
// separate and rarer operation.
func PostBoostRefunded(ctx context.Context, tx pgx.Tx, r BoostRefund) (uuid.UUID, error) {
	if r.NetMinor <= 0 {
		return uuid.Nil, fmt.Errorf("ledger: boost refund must be positive, got %d", r.NetMinor)
	}
	if r.AdvertiserType != "user" && r.AdvertiserType != "vendor" {
		return uuid.Nil, fmt.Errorf("ledger: unknown advertiser type %q", r.AdvertiserType)
	}

	owner := OwnerUser
	if r.AdvertiserType == "vendor" {
		owner = OwnerVendor
	}

	postings := []Posting{
		{
			Account:     AccountRef{Type: DeferredRevenue, OwnerType: OwnerPlatform, Currency: r.Currency},
			Direction:   Debit,
			AmountMinor: r.NetMinor,
		},
		{
			Account: AccountRef{
				Type: CustomerWallet, OwnerType: owner,
				OwnerID: &r.AdvertiserID, Currency: r.Currency,
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
		EventType:     "boost_refunded",
		ReferenceType: "boost_campaign",
		ReferenceID:   r.CampaignID,
		Description:   fmt.Sprintf("campaign %s unspent balance returned", r.CampaignID),
		Currency:      r.Currency,
		Postings:      postings,
	})
}
