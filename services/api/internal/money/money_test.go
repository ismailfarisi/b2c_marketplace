package money

import "testing"

// The tax-inclusive rule is the single most consequential piece of arithmetic
// in the product: get it backwards and every price in every GCC market is wrong
// by the VAT rate. These cases pin the behaviour.
func TestExtractTax(t *testing.T) {
	tests := []struct {
		name    string
		total   int64
		rateBP  int
		wantTax int64
	}{
		// 105.00 AED inclusive of 5% contains exactly 5.00 of VAT.
		// The wrong formula (total × rate) gives 525, not 500.
		{"uae 5pct clean", 10_500, 500, 500},
		{"uae 5pct on 100", 10_000, 500, 476},
		// 115.00 SAR inclusive of 15% contains exactly 15.00.
		{"ksa 15pct clean", 11_500, 1500, 1500},
		{"ksa 15pct on 100", 10_000, 1500, 1304},
		{"zero rate", 10_000, 0, 0},
		{"zero amount", 0, 500, 0},
		// Rounds half up rather than truncating.
		{"rounding", 2_100, 500, 100},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ExtractTax(Amount{Minor: tc.total, Currency: "AED"}, tc.rateBP)
			if got.Minor != tc.wantTax {
				t.Errorf("ExtractTax(%d, %dbp) = %d, want %d", tc.total, tc.rateBP, got.Minor, tc.wantTax)
			}
		})
	}
}

// Extracting then re-adding must return the original total. If this fails the
// two functions disagree about what "inclusive" means, and invoices will not
// foot.
func TestExtractAddRoundTrip(t *testing.T) {
	for _, rate := range []int{500, 1500} {
		for total := int64(1); total < 5_000; total += 37 {
			amt := Amount{Minor: total, Currency: "AED"}
			tax := ExtractTax(amt, rate)
			net := Amount{Minor: total - tax.Minor, Currency: "AED"}
			back := AddTax(net, rate)

			if diff := back.Minor - tax.Minor; diff < -1 || diff > 1 {
				t.Errorf("round trip at rate %d total %d: extracted %d, re-added %d",
					rate, total, tax.Minor, back.Minor)
			}
		}
	}
}

func TestPercentageRoundsHalfUp(t *testing.T) {
	tests := []struct {
		amount int64
		bp     int
		want   int64
	}{
		{10_000, 1500, 1_500},
		{3_333, 1500, 500}, // 499.95 rounds to 500, not down to 499
		{1, 1500, 0},
		{0, 1500, 0},
	}
	for _, tc := range tests {
		got := Percentage(Amount{Minor: tc.amount, Currency: "AED"}, tc.bp)
		if got.Minor != tc.want {
			t.Errorf("Percentage(%d, %dbp) = %d, want %d", tc.amount, tc.bp, got.Minor, tc.want)
		}
	}
}

func TestAddRejectsMixedCurrency(t *testing.T) {
	a := Amount{Minor: 100, Currency: "AED"}
	b := Amount{Minor: 100, Currency: "SAR"}
	if _, err := a.Add(b); err == nil {
		t.Fatal("adding AED to SAR must fail")
	}
}
