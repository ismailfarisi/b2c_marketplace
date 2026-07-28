package invoicing

import (
	"strings"
	"testing"
	"time"
)

func line(net, tax int64, rateBP int) Line {
	return Line{
		Description: "Item", Quantity: 1,
		UnitNetMinor: net, NetMinor: net,
		TaxCategory: "standard", TaxRateBP: rateBP,
		TaxMinor: tax, GrossMinor: net + tax,
	}
}

func doc(lines ...Line) Document {
	d := Document{
		Kind: Simplified, Country: "AE", Currency: "AED",
		Supplier:   Party{Name: "Karama Kitchen LLC", TRN: "100123456700003"},
		Buyer:      Party{Name: "Layla"},
		SupplyDate: time.Date(2026, 8, 3, 0, 0, 0, 0, time.UTC),
		Lines:      lines,
	}
	for _, l := range lines {
		d.NetMinor += l.NetMinor
		d.TaxMinor += l.TaxMinor
		d.GrossMinor += l.GrossMinor
	}
	return d
}

// ── numbering ────────────────────────────────────────────────────────────────

func TestNumbersAreUniquePerCountryKindAndYear(t *testing.T) {
	seen := map[string]bool{}
	for _, country := range []string{"AE", "SA"} {
		for _, kind := range []Kind{TaxInvoice, Simplified, CreditNote} {
			for _, year := range []int{2026, 2027} {
				for seq := int64(1); seq <= 3; seq++ {
					n := FormatNumber(Prefix(kind), country, year, seq)
					if seen[n] {
						t.Fatalf("%s collides", n)
					}
					seen[n] = true
				}
			}
		}
	}
}

func TestNumberFormat(t *testing.T) {
	got := FormatNumber(Prefix(TaxInvoice), "ae", 2026, 117)
	if got != "INV-AE-2026-000117" {
		t.Fatalf("got %q", got)
	}
	// Padding must not truncate once the sequence exceeds six digits.
	if got := FormatNumber("INV", "AE", 2026, 1_234_567); got != "INV-AE-2026-1234567" {
		t.Fatalf("got %q — the sequence must survive, the padding is cosmetic", got)
	}
}

// ── which document ───────────────────────────────────────────────────────────

// A buyer who supplies a TRN intends to reclaim the input tax, and they cannot
// do that from a simplified invoice. Value does not override that.
func TestABuyerTRNForcesAFullTaxInvoice(t *testing.T) {
	if got := KindFor(500, "100123456700003"); got != TaxInvoice {
		t.Fatalf("got %s, want tax_invoice even for a 5 AED order", got)
	}
	if got := KindFor(500, ""); got != Simplified {
		t.Fatalf("got %s, want simplified", got)
	}
	if got := KindFor(SimplifiedThresholdMinor+1, ""); got != TaxInvoice {
		t.Fatalf("got %s, want tax_invoice above the threshold", got)
	}
	if got := KindFor(SimplifiedThresholdMinor, ""); got != Simplified {
		t.Fatalf("got %s — the threshold is inclusive", got)
	}
}

// ── the tax point ────────────────────────────────────────────────────────────

// An order delivered on 31 December and invoiced on 2 January belongs to the
// earlier tax period. Using the issue date puts revenue in the wrong quarter
// and applies the wrong rate on any date a rate changes.
func TestTheTaxPointIsDeliveryNotIssue(t *testing.T) {
	placed := time.Date(2026, 12, 30, 18, 0, 0, 0, time.UTC)
	delivered := time.Date(2026, 12, 31, 20, 0, 0, 0, time.UTC)

	if got := SupplyDate(delivered, placed); !got.Equal(delivered) {
		t.Fatalf("got %v, want the delivery date", got)
	}
	// Undelivered — a cancelled or prepaid order — falls back to placement.
	if got := SupplyDate(time.Time{}, placed); !got.Equal(placed) {
		t.Fatalf("got %v, want the placement date", got)
	}
}

// ── validation ───────────────────────────────────────────────────────────────

func TestAnInvoiceMustAddUp(t *testing.T) {
	d := doc(line(10000, 500, 500))
	if err := d.Validate(); err != nil {
		t.Fatalf("valid document rejected: %v", err)
	}

	broken := d
	broken.GrossMinor += 1
	if err := broken.Validate(); err == nil {
		t.Fatal("totals that disagree with the lines must be rejected")
	}

	badLine := doc(Line{NetMinor: 100, TaxMinor: 5, GrossMinor: 999})
	if err := badLine.Validate(); err == nil {
		t.Fatal("a line that does not close must be rejected")
	}
}

func TestTaxChargedWithoutARegistrationIsRefused(t *testing.T) {
	d := doc(line(10000, 500, 500))
	d.Supplier.TRN = ""
	err := d.Validate()
	if err == nil {
		t.Fatal("charging VAT with no registration number produces an invoice the " +
			"buyer's accountant will reject")
	}
	if !strings.Contains(err.Error(), "registration") {
		t.Fatalf("unhelpful error: %v", err)
	}

	// Zero-rated with no TRN is fine — a supplier below the threshold charges
	// no tax and has no number.
	zero := doc(line(10000, 0, 0))
	zero.Supplier.TRN = ""
	if err := zero.Validate(); err != nil {
		t.Fatalf("a zero-tax invoice needs no TRN: %v", err)
	}
}

func TestAFullTaxInvoiceNeedsABuyer(t *testing.T) {
	d := doc(line(10000, 500, 500))
	d.Kind = TaxInvoice
	d.Buyer.Name = ""
	if err := d.Validate(); err == nil {
		t.Fatal("a full tax invoice with no buyer is not a tax invoice")
	}

	// A simplified one does not — the person ordering dinner is anonymous on
	// the document, which is the entire point of the simplified form.
	d.Kind = Simplified
	if err := d.Validate(); err != nil {
		t.Fatalf("a simplified invoice needs no buyer name: %v", err)
	}
}

func TestAnEmptyInvoiceIsRejected(t *testing.T) {
	if err := doc().Validate(); err == nil {
		t.Fatal("an invoice with no lines describes nothing and must not get a number")
	}
}

// A credit note's lines are negative and must still close. Flipping the signs
// on net and tax but not gross is the obvious way to get this wrong.
func TestACreditNotesLinesCloseWhenNegative(t *testing.T) {
	d := doc(line(-10000, -500, 500))
	d.Kind = CreditNote
	if err := d.Validate(); err != nil {
		t.Fatalf("a negative document must validate: %v", err)
	}
	if d.GrossMinor != -10500 {
		t.Fatalf("gross = %d, want -10500", d.GrossMinor)
	}
}

// ── ZATCA ────────────────────────────────────────────────────────────────────

func TestTheChainStartsAtTheGenesisHash(t *testing.T) {
	links := []ChainLink{{ICV: 1, Hash: "h1", PreviousHash: GenesisHash}}
	if got := VerifyChain(links); got != -1 {
		t.Fatalf("first link reported broken at %d", got)
	}

	wrong := []ChainLink{{ICV: 1, Hash: "h1", PreviousHash: ""}}
	if got := VerifyChain(wrong); got != 0 {
		t.Fatal("a first link that does not carry the genesis hash must be a break")
	}
}

func TestABrokenChainIsFoundAtItsFirstBreak(t *testing.T) {
	links := []ChainLink{
		{ICV: 1, Hash: "h1", PreviousHash: GenesisHash},
		{ICV: 2, Hash: "h2", PreviousHash: "h1"},
		{ICV: 3, Hash: "h3", PreviousHash: "not-h2"},
		{ICV: 4, Hash: "h4", PreviousHash: "h3"},
	}
	if got := VerifyChain(links); got != 2 {
		t.Fatalf("break reported at %d, want 2", got)
	}
}

// A deleted-then-reinserted row leaves the hashes consistent and the counter
// short. Checking only the hashes would call that intact.
func TestAGapInTheCounterIsABreakEvenWhenHashesLineUp(t *testing.T) {
	links := []ChainLink{
		{ICV: 1, Hash: "h1", PreviousHash: GenesisHash},
		{ICV: 3, Hash: "h2", PreviousHash: "h1"},
	}
	if got := VerifyChain(links); got != 1 {
		t.Fatalf("break reported at %d, want 1", got)
	}
}

func TestChangingAnEarlyInvoiceChangesEveryHashAfterIt(t *testing.T) {
	a := ChainHash([]byte("invoice-1"), GenesisHash)
	b := ChainHash([]byte("invoice-2"), a)

	tampered := ChainHash([]byte("invoice-1-altered"), GenesisHash)
	bAfter := ChainHash([]byte("invoice-2"), tampered)

	if b == bAfter {
		t.Fatal("the second hash did not move — the chain is not chaining")
	}
}

func TestQRRoundTrips(t *testing.T) {
	at := time.Date(2026, 8, 3, 14, 30, 0, 0, time.UTC)
	payload, err := QRTLV("مطبخ الكرامة", "300012345600003", at, "115.00", "15.00")
	if err != nil {
		t.Fatal(err)
	}

	fields, err := ParseQRTLV(payload)
	if err != nil {
		t.Fatal(err)
	}
	if fields[1] != "مطبخ الكرامة" {
		t.Errorf("seller name round-tripped as %q — TLV lengths must be in bytes, not runes", fields[1])
	}
	if fields[2] != "300012345600003" {
		t.Errorf("VAT number = %q", fields[2])
	}
	if fields[4] != "115.00" || fields[5] != "15.00" {
		t.Errorf("totals = %q / %q", fields[4], fields[5])
	}
}

func TestQRRefusesAnEmptyMandatoryField(t *testing.T) {
	at := time.Now()
	if _, err := QRTLV("", "300012345600003", at, "115.00", "15.00"); err == nil {
		t.Fatal("an empty seller name must fail rather than produce a QR that scans and lies")
	}
}

func TestQRRefusesAFieldTooLongForItsLengthByte(t *testing.T) {
	at := time.Now()
	if _, err := QRTLV(strings.Repeat("a", 256), "300012345600003", at, "1", "1"); err == nil {
		t.Fatal("a 256-byte field cannot be described by a single length byte")
	}
}

func TestKSAVATNumberShape(t *testing.T) {
	if err := ValidateKSAVATNumber("300012345600003"); err != nil {
		t.Fatalf("valid number rejected: %v", err)
	}
	for _, bad := range []string{
		"30001234560000",   // 14 digits
		"3000123456000033", // 16
		"100123456700003",  // does not start with 3
		"300012345600001",  // does not end with 3
		"30001234560000X",  // non-digit
		"",
	} {
		if err := ValidateKSAVATNumber(bad); err == nil {
			t.Errorf("%q was accepted", bad)
		}
	}
}

func TestSimplifiedInvoicesAreReportedAndFullOnesCleared(t *testing.T) {
	if FlowFor(Simplified) != Reporting {
		t.Error("a B2C invoice is reported within 24h, not cleared first")
	}
	if FlowFor(TaxInvoice) != Clearance {
		t.Error("a B2B invoice must be cleared before it reaches the buyer — this " +
			"changes when the PDF may be sent")
	}
	if FlowFor(CreditNote) != Clearance {
		t.Error("credit notes follow the flow of the document they correct")
	}
}
