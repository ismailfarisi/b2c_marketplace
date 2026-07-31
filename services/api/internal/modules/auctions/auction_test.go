package auctions

import (
	"errors"
	"testing"
	"time"
)

var now = time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)

func liveLot() Auction {
	return Auction{
		ID: "lot", SellerID: "seller",
		Emirate: "Dubai", PlateCode: "AA", PlateNumber: "5", DigitCount: 1,
		Currency:       "AED",
		OpeningMinor:   10_000_00,
		IncrementMinor: 1_000_00,
		OpensAt:        now.Add(-time.Hour),
		ClosesAt:       now.Add(time.Hour),
		Status:         StatusLive,
	}
}

func refusal(t *testing.T, err error) Refusal {
	t.Helper()
	var be *BidError
	if !errors.As(err, &be) {
		t.Fatalf("expected a BidError, got %v", err)
	}
	return be.Reason
}

// ── the ask ──────────────────────────────────────────────────────────────────

func TestTheFirstBidIsTheOpeningPrice(t *testing.T) {
	if got := liveLot().AskMinor(); got != 10_000_00 {
		t.Fatalf("ask is %d before any bid", got)
	}
}

func TestEachBidRaisesTheAskByTheIncrement(t *testing.T) {
	// Not by one fils. An auction a bidder can raise by the smallest coin takes
	// four thousand bids to move, and every one is a notification to everyone
	// else.
	a := liveLot()
	current := int64(12_000_00)
	a.CurrentMinor = &current

	if got := a.AskMinor(); got != 13_000_00 {
		t.Fatalf("ask is %d, want current + increment", got)
	}
}

func TestABidBelowTheAskIsRefusedWithTheNumberThatWouldWork(t *testing.T) {
	// The number matters as much as the refusal. "Too low" alone makes the
	// bidder guess, and on a phone they guess by tapping the same wrong amount
	// again.
	a := liveLot()
	err := CheckBid(a, "buyer", 9_999_00, now)

	var be *BidError
	if !errors.As(err, &be) {
		t.Fatalf("expected a BidError, got %v", err)
	}
	if be.Reason != RefusalTooLow {
		t.Fatalf("reason is %s", be.Reason)
	}
	if be.AskMinor != 10_000_00 {
		t.Fatalf("refusal did not carry the ask: %d", be.AskMinor)
	}
}

func TestABidExactlyAtTheAskIsAccepted(t *testing.T) {
	// The boundary, and the common case: every client renders the ask and every
	// bidder taps it. An off-by-one here refuses the bid the UI suggested.
	if err := CheckBid(liveLot(), "buyer", 10_000_00, now); err != nil {
		t.Fatalf("the exact ask was refused: %v", err)
	}
}

// ── who may bid ──────────────────────────────────────────────────────────────

func TestASellerCannotBidOnTheirOwnLot(t *testing.T) {
	// Shill bidding whatever the intent, and the single thing that destroys
	// trust in an auction platform fastest.
	err := CheckBid(liveLot(), "seller", 20_000_00, now)
	if refusal(t, err) != RefusalOwnLot {
		t.Fatalf("a seller bid on their own lot")
	}
}

func TestTheHighestBidderCannotRaiseThemselves(t *testing.T) {
	// Bidding against yourself raises the price you will pay and changes
	// nothing else. Almost always a double-tap on a slow connection.
	a := liveLot()
	current := int64(12_000_00)
	bidder := "buyer"
	a.CurrentMinor, a.CurrentBidder = &current, &bidder

	if refusal(t, CheckBid(a, "buyer", 20_000_00, now)) != RefusalAlreadyHigh {
		t.Fatal("the leading bidder was allowed to outbid themselves")
	}
}

func TestSomeoneElseMayOutbidTheLeader(t *testing.T) {
	a := liveLot()
	current := int64(12_000_00)
	leader := "first"
	a.CurrentMinor, a.CurrentBidder = &current, &leader

	if err := CheckBid(a, "second", 13_000_00, now); err != nil {
		t.Fatalf("a genuine outbid was refused: %v", err)
	}
}

// ── when bids are taken ──────────────────────────────────────────────────────

func TestAClosedLotTakesNoBids(t *testing.T) {
	a := liveLot()
	a.ClosesAt = now.Add(-time.Minute)

	if refusal(t, CheckBid(a, "buyer", 99_000_00, now)) != RefusalNotOpen {
		t.Fatal("a lot took a bid after it closed")
	}
}

func TestAScheduledLotTakesNoBidsYet(t *testing.T) {
	// Distinct from closed, because the bidder's next move is opposite: come
	// back, rather than give up.
	a := liveLot()
	a.Status = StatusScheduled
	a.OpensAt = now.Add(time.Hour)

	if refusal(t, CheckBid(a, "buyer", 20_000_00, now)) != RefusalNotYetOpen {
		t.Fatal("a scheduled lot took a bid")
	}
}

func TestACancelledLotTakesNoBids(t *testing.T) {
	a := liveLot()
	a.Status = StatusCancelled

	if refusal(t, CheckBid(a, "buyer", 20_000_00, now)) != RefusalNotOpen {
		t.Fatal("a cancelled lot took a bid")
	}
}

// ── anti-sniping ─────────────────────────────────────────────────────────────

func TestABidWellBeforeTheEndDoesNotExtend(t *testing.T) {
	closes := now.Add(time.Hour)
	got, extended := ExtendedClose(closes, now)

	if extended || !got.Equal(closes) {
		t.Fatalf("an early bid moved the close to %s", got)
	}
}

func TestABidInsideTheFinalWindowExtends(t *testing.T) {
	// The rule that makes bidding early worth doing. Without it the winning
	// strategy is one bid at the last instant with an amount nobody has time to
	// answer — rational, and it makes every other bidder's participation
	// pointless.
	closes := now.Add(30 * time.Second)
	got, extended := ExtendedClose(closes, now)

	if !extended {
		t.Fatal("a bid 30 seconds from the close did not extend it")
	}
	if !got.Equal(now.Add(SnipeExtension)) {
		t.Fatalf("extended to %s, want now + %s", got, SnipeExtension)
	}
}

func TestExtensionIsMeasuredFromTheBidNotTheOriginalClose(t *testing.T) {
	// So a lot under sustained bidding keeps extending. A rule that added two
	// minutes to the *original* close would extend once and simply move the
	// sniping target.
	closes := now.Add(10 * time.Second)
	got, _ := ExtendedClose(closes, now)

	if got.Before(now.Add(SnipeExtension)) {
		t.Fatalf("extension was measured from the close, not the bid: %s", got)
	}
}

func TestExtendingRepeatedlyKeepsWorking(t *testing.T) {
	// Five bids in the last minute should push the close five times.
	closes := now.Add(time.Second)
	at := now
	for i := 0; i < 5; i++ {
		var extended bool
		closes, extended = ExtendedClose(closes, at)
		if !extended {
			t.Fatalf("bid %d did not extend", i)
		}
		at = at.Add(30 * time.Second)
	}
	if closes.Sub(now) < 2*time.Minute {
		t.Fatalf("five late bids only reached %s", closes.Sub(now))
	}
}

func TestTheWindowBoundaryExtends(t *testing.T) {
	// Exactly two minutes out. Inclusive, because a bid landing on the boundary
	// is the case a bidder aiming at the window will actually hit, and refusing
	// to extend for it hands them the snipe the rule exists to prevent.
	closes := now.Add(SnipeWindow)
	if _, extended := ExtendedClose(closes, now); !extended {
		t.Fatal("a bid exactly on the window boundary did not extend")
	}
}

// ── the reserve ──────────────────────────────────────────────────────────────

func TestALotWithNoReserveAlwaysSells(t *testing.T) {
	a := liveLot()
	current := int64(10_000_00)
	a.CurrentMinor = &current

	if !a.ReserveMet() {
		t.Fatal("a lot with no reserve reported its reserve unmet")
	}
}

func TestAReserveNotYetReachedIsReported(t *testing.T) {
	// Reported, not hidden. "Reserve not met" is information a bidder acts on,
	// and withholding it wastes everybody's time on a lot that will not sell.
	// The reserve *amount* is never sent — publishing it turns the auction into
	// a fixed-price sale with extra steps.
	a := liveLot()
	current, reserve := int64(12_000_00), int64(50_000_00)
	a.CurrentMinor, a.ReserveMinor = &current, &reserve

	if a.ReserveMet() {
		t.Fatal("a lot below its reserve reported the reserve met")
	}
}

func TestAReserveIsMetExactlyAtTheReserve(t *testing.T) {
	a := liveLot()
	amount := int64(50_000_00)
	a.CurrentMinor, a.ReserveMinor = &amount, &amount

	if !a.ReserveMet() {
		t.Fatal("a bid exactly at the reserve did not meet it")
	}
}

func TestAnUnbidLotWithAReserveHasNotMetIt(t *testing.T) {
	a := liveLot()
	reserve := int64(50_000_00)
	a.ReserveMinor = &reserve

	if a.ReserveMet() {
		t.Fatal("a lot with no bids reported its reserve met")
	}
}

// ── the schedule ─────────────────────────────────────────────────────────────

func TestALotMustRunLongEnoughToGatherBidders(t *testing.T) {
	// Shorter than an hour and nobody scrolls past it twice, which for a feed-
	// driven auction means it never accumulates bidders at all.
	if err := ValidateSchedule(now, now.Add(30*time.Minute)); err == nil {
		t.Fatal("a 30-minute lot was accepted")
	}
	if err := ValidateSchedule(now, now.Add(time.Hour)); err != nil {
		t.Fatalf("an hour was refused: %v", err)
	}
}

func TestALotMayNotRunForever(t *testing.T) {
	if err := ValidateSchedule(now, now.Add(90*24*time.Hour)); err == nil {
		t.Fatal("a 90-day lot was accepted")
	}
}

func TestALotMustCloseAfterItOpens(t *testing.T) {
	if err := ValidateSchedule(now, now.Add(-time.Hour)); err == nil {
		t.Fatal("a lot closing before it opens was accepted")
	}
}

// ── the plate ────────────────────────────────────────────────────────────────

func TestDigitsAreCounted(t *testing.T) {
	// The single biggest driver of what a plate is worth.
	for number, want := range map[string]int{"5": 1, "55": 2, "12345": 5} {
		got, err := DigitsOf(number)
		if err != nil {
			t.Fatalf("%q: %v", number, err)
		}
		if got != want {
			t.Errorf("%q counted %d digits", number, got)
		}
	}
}

func TestALetterInThePlateNumberIsRefusedRatherThanStripped(t *testing.T) {
	// A letter in the number box is a seller who put the code in the wrong
	// field. Stripping it would list "A5" as a one-digit plate — sorted and
	// priced as one of the most valuable things on the platform.
	if _, err := DigitsOf("A5"); err == nil {
		t.Fatal("a plate number with a letter was accepted")
	}
}

func TestAnEmptyOrOversizedPlateNumberIsRefused(t *testing.T) {
	if _, err := DigitsOf("  "); err == nil {
		t.Error("a blank plate number was accepted")
	}
	if _, err := DigitsOf("1234567"); err == nil {
		t.Error("a seven-digit plate number was accepted")
	}
}

// ── the horizon ──────────────────────────────────────────────────────────────

func TestTheCountdownBarIsDrawnOverTheFinalHour(t *testing.T) {
	// The number the mode's own comment argues for. A bar spread over a
	// nine-day lot does not visibly move during the two minutes anyone is
	// watching, which is worse than no bar.
	if CountdownHorizon != time.Hour {
		t.Fatalf("horizon is %s", CountdownHorizon)
	}
	if CountdownHorizon >= MaxDuration {
		t.Fatal("the horizon must be shorter than the longest lot, or it is not a horizon")
	}
}
