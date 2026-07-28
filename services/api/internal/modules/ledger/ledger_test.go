package ledger

import (
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func acct(t AccountType) AccountRef {
	return AccountRef{Type: t, OwnerType: OwnerPlatform, Currency: "AED"}
}

func TestJournalMustBalance(t *testing.T) {
	j := Journal{
		EventType: "test",
		Currency:  "AED",
		Postings: []Posting{
			{Account: acct(GatewayReceivable), Direction: Debit, AmountMinor: 10_000},
			{Account: acct(VendorPayable), Direction: Credit, AmountMinor: 9_000},
		},
	}
	err := j.validate()
	if !errors.Is(err, ErrUnbalanced) {
		t.Fatalf("expected ErrUnbalanced, got %v", err)
	}
}

func TestJournalRejectsSignedAmounts(t *testing.T) {
	// Direction carries the sign. Allowing negative amounts as well would make
	// every journal ambiguous — a negative credit and a positive debit would
	// both balance, and reports would silently disagree about which.
	j := Journal{
		EventType: "test",
		Currency:  "AED",
		Postings: []Posting{
			{Account: acct(GatewayReceivable), Direction: Debit, AmountMinor: -100},
			{Account: acct(VendorPayable), Direction: Credit, AmountMinor: -100},
		},
	}
	if err := j.validate(); err == nil {
		t.Fatal("expected a negative posting to be rejected")
	}
}

func TestJournalRejectsMixedCurrency(t *testing.T) {
	sar := AccountRef{Type: VendorPayable, OwnerType: OwnerPlatform, Currency: "SAR"}
	j := Journal{
		EventType: "test",
		Currency:  "AED",
		Postings: []Posting{
			{Account: acct(GatewayReceivable), Direction: Debit, AmountMinor: 100},
			{Account: sar, Direction: Credit, AmountMinor: 100},
		},
	}
	err := j.validate()
	if err == nil || !strings.Contains(err.Error(), "mixes") {
		t.Fatalf("expected a mixed-currency rejection, got %v", err)
	}
}

func TestJournalNeedsTwoPostings(t *testing.T) {
	j := Journal{
		EventType: "test",
		Currency:  "AED",
		Postings: []Posting{
			{Account: acct(GatewayReceivable), Direction: Debit, AmountMinor: 100},
		},
	}
	if err := j.validate(); !errors.Is(err, ErrUnbalanced) {
		t.Fatalf("expected ErrUnbalanced, got %v", err)
	}
}

// The capture split is the shape that has to foot: what the customer paid must
// equal what the vendor is owed plus what the platform kept plus VAT. This
// builds the same postings PostOrderCaptured does and checks they balance
// across a range of commission and VAT values, including the boundaries where
// a component rounds to zero and gets omitted.
func TestCaptureSplitAlwaysBalances(t *testing.T) {
	vendorID := uuid.New()

	for _, total := range []int64{1, 99, 10_000, 12_345, 999_999} {
		for _, commission := range []int64{0, 1, total / 10, total} {
			if commission > total {
				continue
			}
			for _, vat := range []int64{0, 1, commission / 21} {
				if vat > commission {
					continue
				}

				postings := []Posting{
					{Account: acct(GatewayReceivable), Direction: Debit, AmountMinor: total},
				}
				if vendorNet := total - commission; vendorNet > 0 {
					postings = append(postings, Posting{
						Account: AccountRef{
							Type: VendorPayable, OwnerType: OwnerVendor,
							OwnerID: &vendorID, Currency: "AED",
						},
						Direction:   Credit,
						AmountMinor: vendorNet,
					})
				}
				if net := commission - vat; net > 0 {
					postings = append(postings, Posting{
						Account: acct(PlatformRevenue), Direction: Credit, AmountMinor: net,
					})
				}
				if vat > 0 {
					postings = append(postings, Posting{
						Account: acct(VATPayable), Direction: Credit, AmountMinor: vat,
					})
				}

				j := Journal{EventType: "order_captured", Currency: "AED", Postings: postings}

				// A journal that collapses to one posting cannot balance and is
				// not a journal. total == commission == vat means every credit
				// rounded away; there is nothing to record.
				if len(postings) < 2 {
					continue
				}

				if err := j.validate(); err != nil {
					t.Fatalf("total=%d commission=%d vat=%d: %v", total, commission, vat, err)
				}
			}
		}
	}
}
