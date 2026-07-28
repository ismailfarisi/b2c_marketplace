package payouts

import (
	"testing"
	"time"
)

func line(kind Kind, gross, commission, vat, net int64) Line {
	return Line{Kind: kind, GrossMinor: gross, CommissionMinor: commission,
		VATMinor: vat, NetMinor: net}
}

func TestSumBalances(t *testing.T) {
	lines := []Line{
		line(KindOrder, 15000, 2250, 112, 12750),
		line(KindOrder, 8900, 1335, 66, 7565),
		line(KindSubscription, 44900, 6735, 336, 38165),
	}
	got := Sum(lines)

	if got.GrossMinor != 68800 {
		t.Fatalf("gross %d", got.GrossMinor)
	}
	if err := got.Validate(); err != nil {
		t.Fatal(err)
	}
	if want := 68800 - 10320 - 514; got.NetMinor != int64(want) {
		t.Fatalf("net %d, want %d", got.NetMinor, want)
	}
}

func TestAdjustmentsDoNotAttractCommission(t *testing.T) {
	// The platform does not take a cut of a goodwill credit it issued, or of a
	// penalty it levied itself. Running adjustments through the gross column
	// would do exactly that and the vendor would be charged commission on their
	// own compensation.
	lines := []Line{
		line(KindOrder, 10000, 1500, 75, 8500),
		line(KindAdjustment, 0, 0, 0, -2000),
	}
	got := Sum(lines)

	if got.GrossMinor != 10000 {
		t.Fatalf("adjustment leaked into gross: %d", got.GrossMinor)
	}
	if got.CommissionMinor != 1500 {
		t.Fatalf("adjustment attracted commission: %d", got.CommissionMinor)
	}
	if got.AdjustmentsMinor != -2000 {
		t.Fatalf("adjustments %d", got.AdjustmentsMinor)
	}
	if err := got.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestCashCollectedAtTheDoorIsDeducted(t *testing.T) {
	// The failure this exists to prevent: a vendor paid twice for one order,
	// once in cash the driver handed them and once in the payout. The COD offset
	// is a negative adjustment for money that never reached the platform.
	lines := []Line{
		line(KindOrder, 20000, 3000, 150, 16850),
		line(KindCODOffset, 0, 0, 0, -16850),
	}
	got := Sum(lines)
	if got.NetMinor != 0 {
		t.Fatalf("net %d — a fully cash-paid order should net to zero in the payout", got.NetMinor)
	}
	if got.Payable() {
		t.Fatal("a zero statement must not become a payment instruction")
	}
}

func TestNetIsDerivedNotSummed(t *testing.T) {
	// A line whose own net is wrong must not produce a total that still looks
	// internally consistent. Deriving the total from the components is what
	// makes the database CHECK test something real.
	lines := []Line{
		line(KindOrder, 10000, 1500, 75, 999999), // nonsense line net
	}
	got := Sum(lines)
	if got.NetMinor != 10000-1500-75 {
		t.Fatalf("net %d was summed from the lines rather than derived", got.NetMinor)
	}
}

func TestValidateCatchesAnImbalance(t *testing.T) {
	bad := Totals{GrossMinor: 10000, CommissionMinor: 1500, VATMinor: 75, NetMinor: 9000}
	if err := bad.Validate(); err == nil {
		t.Fatal("an unbalanced statement must not validate")
	}
}

func TestValidateCatchesVATExceedingCommission(t *testing.T) {
	// Platform VAT is charged on the platform's commission. VAT above it means
	// the two were computed from different bases — a bug that otherwise shows up
	// as a number quietly wrong by a few percent every month.
	bad := Totals{GrossMinor: 10000, CommissionMinor: 100, VATMinor: 500}
	bad.NetMinor = bad.GrossMinor - bad.CommissionMinor - bad.VATMinor
	if err := bad.Validate(); err == nil {
		t.Fatal("VAT above commission must not validate")
	}
}

func TestNegativeStatementsAreNotPayable(t *testing.T) {
	// A vendor whose refunds exceeded their sales genuinely owes the platform.
	// That is not an error, and it is also not a payment instruction — it is
	// carried into the next period.
	lines := []Line{
		line(KindOrder, 5000, 750, 37, 4213),
		line(KindRefund, 0, 0, 0, 0),
		line(KindAdjustment, 0, 0, 0, -9000),
	}
	got := Sum(lines)
	if got.Payable() {
		t.Fatalf("net %d must not be payable", got.NetMinor)
	}
	if err := got.Validate(); err != nil {
		t.Fatalf("a negative statement is still a valid statement: %v", err)
	}
}

func TestEmptyPeriodIsValidAndNotPayable(t *testing.T) {
	got := Sum(nil)
	if err := got.Validate(); err != nil {
		t.Fatal(err)
	}
	if got.Payable() {
		t.Fatal("a vendor who traded nothing is owed nothing")
	}
}

// ── periods ──────────────────────────────────────────────────────────────────

func TestWeeksStartOnSunday(t *testing.T) {
	// The GCC working week. An ISO week starting Monday cuts the Gulf weekend in
	// half, so a vendor's busiest two days land in different statements and
	// their weekly figures oscillate for reasons unrelated to their business.
	loc := time.FixedZone("Asia/Dubai", 4*3600)
	// Wednesday 29 July 2026.
	wed := time.Date(2026, 7, 29, 15, 0, 0, 0, loc)

	p := WeekOf(wed, loc)
	if p.Start.Weekday() != time.Sunday {
		t.Fatalf("week starts on %s", p.Start.Weekday())
	}
	if p.End.Weekday() != time.Saturday {
		t.Fatalf("week ends on %s", p.End.Weekday())
	}
	if !p.Contains(wed) {
		t.Fatal("the week does not contain the day it was derived from")
	}
}

func TestPeriodIncludesItsLastDayEntirely(t *testing.T) {
	// An order at 23:50 on Saturday belongs to that week. A period that ends at
	// midnight on the last day silently drops the busiest evening of the week.
	loc := time.FixedZone("Asia/Dubai", 4*3600)
	p := WeekOf(time.Date(2026, 7, 29, 12, 0, 0, 0, loc), loc)

	lateOnLastDay := time.Date(p.End.Year(), p.End.Month(), p.End.Day(), 23, 50, 0, 0, loc)
	if !p.Contains(lateOnLastDay) {
		t.Fatal("23:50 on the closing day fell outside the period")
	}
	if p.Contains(lateOnLastDay.Add(20 * time.Minute)) {
		t.Fatal("00:10 the next day fell inside the period")
	}
}

func TestPayoutRunsCloseThePreviousWeek(t *testing.T) {
	// Never the current one. A statement covering a period that is still
	// accruing changes after the vendor was shown it, and they reconcile
	// against the version they saw.
	loc := time.FixedZone("Asia/Dubai", 4*3600)
	now := time.Date(2026, 7, 29, 3, 0, 0, 0, loc)

	prev := PreviousWeek(now, loc)
	if prev.Contains(now) {
		t.Fatal("a payout run must not close a period that is still open")
	}
	if got := WeekOf(now, loc).Start.Sub(prev.End); got > 48*time.Hour {
		t.Fatalf("previous week ends %s before the current one starts — there is a gap", got)
	}
}

func TestNoWeekIsSkippedOrDoubleCounted(t *testing.T) {
	// Walk a year of Sundays and check the periods tile without gaps or overlap.
	// A one-day gap once a year is a day of orders nobody is ever paid for.
	loc := time.FixedZone("Asia/Dubai", 4*3600)
	day := time.Date(2026, 1, 1, 12, 0, 0, 0, loc)

	var last Period
	for i := 0; i < 365; i++ {
		p := WeekOf(day, loc)
		if !last.Start.IsZero() && !p.Start.Equal(last.Start) {
			if gap := p.Start.Sub(last.End); gap != 24*time.Hour {
				t.Fatalf("day %d: gap of %s between periods", i, gap)
			}
		}
		last = p
		day = day.AddDate(0, 0, 1)
	}
}
