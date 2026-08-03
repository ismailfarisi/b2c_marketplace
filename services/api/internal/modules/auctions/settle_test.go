package auctions

import (
	"testing"
	"time"

	"github.com/marketplace/api/internal/money"
)

// The commission arithmetic, checked here rather than only against a database,
// because these are the numbers that leave a seller's account.

func TestTheCommissionIsChargedOnTheHammerPrice(t *testing.T) {
	// 15% of AED 100,000. Not of the opening bid, not of the reserve — of what
	// the lot actually went for.
	commission := money.Percentage(money.New(10_000_000, "AED"), 1_500)
	if commission.Minor != 1_500_000 {
		t.Fatalf("commission is %d minor, want 1,500,000", commission.Minor)
	}
}

func TestVATIsChargedOnTheCommissionAndNotOnTheSale(t *testing.T) {
	// The platform supplies a service worth the commission; it does not supply
	// the plate. Charging 5% of the hammer price would be invoicing VAT on a
	// transaction the platform is not party to — five times too much, and for
	// the wrong supply.
	hammer := money.New(10_000_000, "AED")
	commission := money.Percentage(hammer, 1_500)
	vat := money.Percentage(commission, 500)

	if vat.Minor != 75_000 {
		t.Fatalf("VAT is %d minor, want 75,000 (5%% of the commission)", vat.Minor)
	}
	if vat.Minor >= money.Percentage(hammer, 500).Minor {
		t.Fatal("VAT was charged against the sale rather than the fee")
	}
}

func TestVATIsNotCompounded(t *testing.T) {
	// Computed from the commission once, not from a rounded commission that has
	// itself been rounded. Two systems doing it differently end up a fils apart
	// on the same sale, and a fils apart is a reconciliation ticket.
	hammer := money.New(3_333_333, "AED")
	commission := money.Percentage(hammer, 1_500)
	vat := money.Percentage(commission, 500)

	total := commission.Minor + vat.Minor
	// 15% then 5% of that is 15.75% of the hammer, within rounding.
	want := money.Percentage(hammer, 1_575).Minor
	if diff := total - want; diff > 1 || diff < -1 {
		t.Fatalf("fee is %d, expected within a fils of %d", total, want)
	}
}

func TestAFreeLotCostsNothing(t *testing.T) {
	// A commission of zero basis points is a legitimate configuration — a
	// launch promotion — and it must produce a settlement of zero rather than a
	// constraint violation. The table's CHECK is `>= 0`, not `> 0`, for exactly
	// this.
	commission := money.Percentage(money.New(10_000_000, "AED"), 0)
	if commission.Minor != 0 {
		t.Fatalf("a zero-rate commission produced %d", commission.Minor)
	}
}

func TestThePaymentTermIsLongEnoughToBeMet(t *testing.T) {
	// The plate transfer happens at an RTA counter on the two parties' own
	// timetable. A fee due before they have had a chance to meet is a fee that
	// arrives while the seller is still unsure the sale is real.
	if PaymentTerm < 3*24*time.Hour {
		t.Fatalf("payment term is %s — too short to arrange a transfer", PaymentTerm)
	}
}

func TestDatesRenderInTheMarketZoneNotTheProcessZone(t *testing.T) {
	// The bug this caught, kept as a test because it is invisible on a machine
	// that happens to run at +04.
	//
	// A deadline four hours before midnight UTC is the *next day* in Dubai.
	// Rendering it in the process's zone put "07 Aug" in the settlement row and
	// "8 Aug" in the seller's notification, for one instant — and in a UTC
	// container the seller is told their payment is due a day early.
	instant := time.Date(2026, 8, 7, 21, 30, 0, 0, time.UTC)

	if got := instant.In(marketZone).Format("2 Jan"); got != "8 Aug" {
		t.Fatalf("rendered %q in the market zone, want 8 Aug", got)
	}
	if instant.UTC().Format("2 Jan") == instant.In(marketZone).Format("2 Jan") {
		t.Fatal("the test instant does not straddle midnight — it proves nothing")
	}
}

func TestTheMarketZoneResolves(t *testing.T) {
	// Guards the tzdata import. Without `_ "time/tzdata"` in the binaries, a
	// scratch container silently falls back to UTC and every wall-clock string
	// in the product is four hours out.
	if marketZone == time.UTC {
		t.Fatal("Asia/Dubai did not resolve — is time/tzdata imported by the binaries?")
	}
	_, offset := time.Date(2026, 8, 1, 12, 0, 0, 0, marketZone).Zone()
	if offset != 4*3600 {
		t.Fatalf("market zone offset is %ds, want +04:00", offset)
	}
}
