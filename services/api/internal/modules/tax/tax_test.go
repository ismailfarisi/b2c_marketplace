package tax

import "testing"

// The invariant everything else rests on. An invoice whose net + tax does not
// equal its gross is rejected by the database CHECK and by an accountant, and
// at a 5% rate the naive "round both" implementation gets it wrong on roughly
// half of all amounts rather than on rare edge cases.
func TestSplitAlwaysCloses(t *testing.T) {
	for _, rate := range []int{500, 1500, 0, 250} {
		for gross := int64(1); gross <= 5000; gross++ {
			net, tax := Split(gross, rate)
			if net+tax != gross {
				t.Fatalf("rate %d, gross %d: net %d + tax %d = %d", rate, gross, net, tax, net+tax)
			}
			if tax < 0 || net < 0 {
				t.Fatalf("rate %d, gross %d: negative component (net %d, tax %d)", rate, gross, net, tax)
			}
		}
	}
}

func TestSplitKnownValues(t *testing.T) {
	cases := []struct {
		gross    int64
		rateBP   int
		net, tax int64
	}{
		// 105.00 AED at 5% is 100.00 + 5.00 exactly.
		{10500, 500, 10000, 500},
		// 115.00 SAR at 15% is 100.00 + 15.00 exactly.
		{11500, 1500, 10000, 1500},
		// 100.00 at 5%: tax = 10000×500/10500 = 476.19 → 476.
		{10000, 500, 9524, 476},
		// Exempt and zero-rated lines carry no tax at all.
		{10000, 0, 10000, 0},
		{1, 500, 1, 0},
	}
	for _, c := range cases {
		net, tax := Split(c.gross, c.rateBP)
		if net != c.net || tax != c.tax {
			t.Errorf("Split(%d, %d) = (%d, %d), want (%d, %d)",
				c.gross, c.rateBP, net, tax, c.net, c.tax)
		}
	}
}

// A refund must be the exact negative of the charge it reverses. Rounding half
// *up* rather than half away from zero leaves a fils behind on every reversal,
// and those fils accumulate in the VAT payable account where nobody is looking.
func TestARefundIsTheExactNegativeOfItsCharge(t *testing.T) {
	for gross := int64(1); gross <= 2000; gross++ {
		net, tax := Split(gross, 500)
		rNet, rTax := Split(-gross, 500)
		if rNet != -net || rTax != -tax {
			t.Fatalf("gross %d: charge (%d, %d), refund (%d, %d) — not symmetric",
				gross, net, tax, rNet, rTax)
		}
	}
}

func TestAddIsTheBusinessToBusinessDirection(t *testing.T) {
	tax, gross := Add(10000, 500)
	if tax != 500 || gross != 10500 {
		t.Fatalf("Add(10000, 500) = (%d, %d), want (500, 10500)", tax, gross)
	}
	// And it must close too.
	for net := int64(1); net <= 3000; net++ {
		tx, gr := Add(net, 500)
		if net+tx != gr {
			t.Fatalf("net %d: %d + %d != %d", net, net, tx, gr)
		}
	}
}

// Per line, then summed. A total computed over the document as a whole differs
// by a fils or two from the sum of what the lines print, and only one of those
// two numbers is on the invoice.
func TestTotalsAreSummedPerLineNotOverTheDocument(t *testing.T) {
	lines := []Line{
		{Description: "Chicken biryani", Category: Standard, RateBP: 500, GrossMinor: 3350},
		{Description: "Mixed grill", Category: Standard, RateBP: 500, GrossMinor: 4900},
		{Description: "Delivery", Category: Standard, RateBP: 500, GrossMinor: 1000},
	}
	totals := SumLines(lines)

	if !totals.Closes() {
		t.Fatalf("totals do not close: %d + %d != %d",
			totals.NetMinor, totals.TaxMinor, totals.GrossMinor)
	}

	var wantNet, wantTax int64
	for _, l := range lines {
		n, x := Split(l.GrossMinor, l.RateBP)
		wantNet += n
		wantTax += x
	}
	if totals.NetMinor != wantNet || totals.TaxMinor != wantTax {
		t.Fatalf("got (%d, %d), want (%d, %d)",
			totals.NetMinor, totals.TaxMinor, wantNet, wantTax)
	}

	// The whole-document shortcut, for contrast. When these differ the test has
	// found the bug it exists to prevent; when they agree the fixture simply
	// did not happen to trigger it.
	docNet, docTax := Split(totals.GrossMinor, 500)
	if docNet != wantNet || docTax != wantTax {
		t.Logf("per-line (%d, %d) differs from whole-document (%d, %d) — "+
			"this is why the invoice sums its own lines", wantNet, wantTax, docNet, docTax)
	}
}

// A tax invoice carrying more than one rate must print a subtotal per rate.
// Mixed-rate orders are not hypothetical: a delivery charge and an exempt item
// on the same order produce two.
func TestMixedRatesAreSummarisedSeparately(t *testing.T) {
	totals := SumLines([]Line{
		{Category: Standard, RateBP: 500, GrossMinor: 10500},
		{Category: Exempt, RateBP: 0, GrossMinor: 5000},
		{Category: Standard, RateBP: 500, GrossMinor: 2100},
	})

	if len(totals.ByRate) != 2 {
		t.Fatalf("got %d rate groups, want 2", len(totals.ByRate))
	}
	if got := totals.ByRate[500]; got.TaxMinor != 600 {
		t.Errorf("standard-rated tax = %d, want 600", got.TaxMinor)
	}
	if got := totals.ByRate[0]; got.TaxMinor != 0 || got.NetMinor != 5000 {
		t.Errorf("exempt group = %+v, want net 5000 tax 0", got)
	}
	if !totals.Closes() {
		t.Error("mixed-rate totals must still close")
	}
}

func TestEmptyDocumentIsZeroAndCloses(t *testing.T) {
	totals := SumLines(nil)
	if totals.GrossMinor != 0 || !totals.Closes() {
		t.Fatalf("empty totals = %+v", totals)
	}
}
