// Package auctions runs plate auctions — the vertical ModeCountdown was
// designed for and has never had.
//
// Everything in here that looks like ceremony is defending against one of four
// failures that auctions have and nothing else in this product does:
//
//	two bids arriving at once      → FOR UPDATE, so they serialise
//	a bid landing at the last second → anti-snipe extension
//	a seller bidding on their own lot → refused outright
//	a lot closing below its reserve  → unsold, not sold cheap
package auctions

import (
	"fmt"
	"strings"
	"time"
)

type Status string

const (
	StatusDraft     Status = "draft"
	StatusScheduled Status = "scheduled"
	StatusLive      Status = "live"
	// StatusClosedWon is "the hammer fell", not "the money arrived".
	StatusClosedWon    Status = "closed_won"
	StatusClosedUnsold Status = "closed_unsold"
	StatusSettled      Status = "settled"
	StatusCancelled    Status = "cancelled"
)

// SnipeWindow and SnipeExtension are the anti-sniping rule.
//
// A bid inside the last two minutes pushes the close out by two minutes, as
// many times as it takes. Without it the winning strategy is to bid once, at
// the last possible instant, with an amount nobody has time to answer — which
// is rational, and which makes every other bidder's participation pointless.
// An auction where bidding early is punished stops having bidders.
//
// Two minutes rather than thirty seconds: this is a phone on a cellular
// connection, and thirty seconds is not enough time to see a notification,
// open the app and decide.
const (
	SnipeWindow    = 2 * time.Minute
	SnipeExtension = 2 * time.Minute
)

// MinDuration and MaxDuration bound how long a lot may run.
//
// An hour is the floor because an auction shorter than that cannot accumulate
// bidders from a feed — nobody scrolls past it twice. Thirty days is the
// ceiling because a countdown that long is not a countdown, and the seam's own
// Horizon exists precisely so a nine-day lot can still draw a legible bar.
const (
	MinDuration = time.Hour
	MaxDuration = 30 * 24 * time.Hour
)

// CountdownHorizon is the span the time bar is drawn across.
//
// One hour, regardless of how long the lot runs. This is the number the mode's
// own comment argues for: a bar spread over nine days does not visibly move
// during the two minutes anyone is watching, which is worse than no bar. The
// bar sits saturated until the final hour and then drains, which is when
// urgency is real.
const CountdownHorizon = time.Hour

// Auction is one lot.
type Auction struct {
	ID          string
	ReferenceNo string
	SellerID    string

	Emirate     string
	PlateCode   string
	PlateNumber string
	DigitCount  int

	Currency       string
	OpeningMinor   int64
	ReserveMinor   *int64
	IncrementMinor int64

	OpensAt  time.Time
	ClosesAt time.Time
	Status   Status

	CurrentMinor  *int64
	CurrentBidder *string
	BidCount      int
	WinnerID      *string
}

// Plate is the lot's name, as a bidder reads it.
func (a Auction) Plate() string {
	return strings.TrimSpace(a.PlateCode + " " + a.PlateNumber)
}

// AskMinor is what the next bid must be at least.
//
// The opening bid when nobody has bid, and current + increment after that. Not
// "current + 1": an auction where a bidder can raise by one fils is an auction
// that takes four thousand bids to move, and every one of them is a
// notification to everybody else.
func (a Auction) AskMinor() int64 {
	if a.CurrentMinor == nil {
		return a.OpeningMinor
	}
	return *a.CurrentMinor + a.IncrementMinor
}

// ReserveMet reports whether the lot will actually sell at the current price.
//
// The reserve itself is never returned to a client — see the column comment.
// This boolean is, because "reserve not met" is information a bidder needs and
// acts on, and withholding it wastes everybody's time on a lot that will not
// sell.
func (a Auction) ReserveMet() bool {
	if a.ReserveMinor == nil {
		return true
	}
	if a.CurrentMinor == nil {
		return false
	}
	return *a.CurrentMinor >= *a.ReserveMinor
}

// Open reports whether bids are being taken *right now*.
//
// Checked against a passed-in instant rather than time.Now(), because the
// caller has already taken the row lock and read the clock once; reading it
// again here would let a lot be live for one check and closed for the next
// inside a single transaction.
func (a Auction) Open(now time.Time) bool {
	return a.Status == StatusLive && now.Before(a.ClosesAt)
}

// Refusal is why a bid was not accepted.
//
// A closed set rather than sentences, for the reason every other refusal in
// this codebase is: the client renders these in two languages, and a bidder
// being told "too low" needs to be told the actual number alongside it.
type Refusal string

const (
	RefusalNotOpen     Refusal = "not_open"
	RefusalNotYetOpen  Refusal = "not_yet_open"
	RefusalTooLow      Refusal = "too_low"
	RefusalOwnLot      Refusal = "own_lot"
	RefusalAlreadyHigh Refusal = "already_highest"
)

// BidError carries the refusal and, for a low bid, the number that would work.
type BidError struct {
	Reason   Refusal
	AskMinor int64
	Currency string
}

func (e *BidError) Error() string {
	return fmt.Sprintf("auctions: bid refused (%s)", e.Reason)
}

// CheckBid is the whole of the bidding rule, as a pure function.
//
// Separated from the transaction so it can be tested without a database — the
// four failures this package exists to prevent are all decidable from an
// auction, a bidder and an amount, and the SQL around it is only there to make
// the decision atomic.
func CheckBid(a Auction, bidderID string, amountMinor int64, now time.Time) error {
	if a.Status == StatusScheduled || (a.Status == StatusLive && now.Before(a.OpensAt)) {
		return &BidError{Reason: RefusalNotYetOpen, AskMinor: a.OpeningMinor, Currency: a.Currency}
	}
	if !a.Open(now) {
		return &BidError{Reason: RefusalNotOpen, Currency: a.Currency}
	}
	if a.SellerID == bidderID {
		// A seller bidding on their own lot is shill bidding whatever the
		// intent, and it is the single thing that destroys trust in an auction
		// platform fastest. Refused here rather than merely logged.
		return &BidError{Reason: RefusalOwnLot, Currency: a.Currency}
	}
	if a.CurrentBidder != nil && *a.CurrentBidder == bidderID {
		// Bidding against yourself raises the price you will pay and changes
		// nothing else. Almost always a double-tap on a slow connection.
		return &BidError{Reason: RefusalAlreadyHigh, AskMinor: a.AskMinor(), Currency: a.Currency}
	}
	if amountMinor < a.AskMinor() {
		return &BidError{Reason: RefusalTooLow, AskMinor: a.AskMinor(), Currency: a.Currency}
	}
	return nil
}

// ExtendedClose returns the lot's new closing time after a bid at `now`, and
// whether it moved.
//
// Compared against the *current* close rather than the original, so a lot under
// sustained bidding keeps extending — which is the point. A rule that extended
// only once would simply move the sniping target by two minutes.
func ExtendedClose(closesAt, now time.Time) (time.Time, bool) {
	if now.Add(SnipeWindow).Before(closesAt) {
		return closesAt, false
	}
	return now.Add(SnipeExtension), true
}

// ValidateSchedule checks the two instants a seller picks.
func ValidateSchedule(opensAt, closesAt time.Time) error {
	d := closesAt.Sub(opensAt)
	switch {
	case d <= 0:
		return fmt.Errorf("auctions: a lot must close after it opens")
	case d < MinDuration:
		return fmt.Errorf("auctions: a lot must run for at least %s", MinDuration)
	case d > MaxDuration:
		return fmt.Errorf("auctions: a lot may not run for more than %s", MaxDuration)
	}
	return nil
}

// DigitsOf counts the digits in a plate number, which is most of what makes a
// plate valuable.
//
// Refuses anything that is not digits rather than stripping them out: a plate
// number with a letter in it is a seller who has put the code in the wrong box,
// and silently accepting "A5" as one digit would list it wrongly and sort it
// wrongly for the whole auction.
func DigitsOf(plateNumber string) (int, error) {
	trimmed := strings.TrimSpace(plateNumber)
	if trimmed == "" {
		return 0, fmt.Errorf("auctions: a plate needs a number")
	}
	for _, r := range trimmed {
		if r < '0' || r > '9' {
			return 0, fmt.Errorf("auctions: %q is not a plate number — digits only", plateNumber)
		}
	}
	if len(trimmed) > 6 {
		return 0, fmt.Errorf("auctions: %q has more than six digits", plateNumber)
	}
	return len(trimmed), nil
}
