package auctions

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/marketplace/api/internal/seam/action"
)

// BidHandler renders the bid button.
//
// # Where the amount lives, and why not in the label
//
// The one thing a viewer needs before tapping is what tapping commits them to.
// A bid is binding, and the distance between "I tapped a button" and "I owe
// someone twelve thousand dirhams" should be as short and legible as possible —
// which argues for a label reading "Bid AED 12,000".
//
// It is in `meta.ask_minor` instead, and the label stays generic. Formatting
// money on the server contradicts the rule the rest of this codebase follows
// for exactly one reason that matters here: an Arabic viewer and an English
// viewer are served the same cached page, and a server-formatted "AED 12,000"
// cannot become "١٢٬٠٠٠ د.إ" — nor, more importantly, can it stay Western-digit
// while the *rest* of the overlay's price line already localises through the
// client's formatter. Two money formatters in one screen disagree eventually.
//
// So the server states the number and the client renders it, the same way
// `Summary.PriceMinor` already works. The client composes the label.
type BidHandler struct{ pool *pgxpool.Pool }

func NewBidHandler(pool *pgxpool.Pool) *BidHandler { return &BidHandler{pool: pool} }

func (h *BidHandler) Resolve(ctx context.Context, a action.Action, v action.Viewer) (action.Resolved, error) {
	label, labelAr := action.Override("Place bid", "قدّم مزايدة", a.Label, a.LabelAr)

	res := action.Resolved{
		Label:     label,
		LabelAr:   labelAr,
		Available: true,
		Route:     fmt.Sprintf("/auctions/%s/bid", a.TargetID),
	}

	lot, err := scanAuction(h.pool.QueryRow(ctx,
		`SELECT`+auctionColumns+` FROM plate_auctions WHERE id = $1`, a.TargetID))
	if errors.Is(err, pgx.ErrNoRows) {
		res.Available = false
		res.Reason = "This lot is no longer listed"
		return res, nil
	}
	if err != nil {
		return action.Resolved{}, fmt.Errorf("auctions: resolve cta: %w", err)
	}

	now := time.Now()

	// Every refusal names what happened rather than greying out silently. A
	// disabled button with no explanation reads as a broken app, and on an
	// auction it reads as the platform having taken the lot away.
	switch {
	case lot.Status == StatusCancelled:
		res.Available = false
		res.Reason = "This lot was withdrawn"
		return res, nil

	case lot.Status == StatusClosedWon || lot.Status == StatusSettled:
		res.Available = false
		res.Reason = "Sold"
		return res, nil

	case lot.Status == StatusClosedUnsold:
		res.Available = false
		res.Reason = "Closed without a sale"
		return res, nil

	case lot.Status == StatusScheduled || now.Before(lot.OpensAt):
		// Not an error and not a dead end: the viewer should come back. The
		// clock above the button is already counting to exactly this moment.
		res.Available = false
		res.Reason = "Opens " + lot.OpensAt.Format("2 Jan, 15:04")
		return res, nil

	case !lot.Open(now):
		res.Available = false
		res.Reason = "Bidding has closed"
		return res, nil

	case v.UserID.Valid && v.UserID.UUID.String() == lot.SellerID:
		// The seller sees their own lot in their own feed. Saying so is more
		// useful than a button that would be refused on tap.
		res.Available = false
		res.Reason = "This is your lot"
		return res, nil

	case v.UserID.Valid && lot.CurrentBidder != nil && *lot.CurrentBidder == v.UserID.UUID.String():
		// Leading. Not a refusal so much as the answer to the question they
		// were about to ask.
		res.Available = false
		res.Reason = "You are the highest bidder"
		return res, nil
	}

	// The client posts back the number it was shown, rather than recomputing it
	// from a price that may have moved between render and tap. A bid for an
	// amount the bidder never saw is the one mistake an auction must not make.
	res.Meta = map[string]any{
		"ask_minor":       lot.AskMinor(),
		"currency":        lot.Currency,
		"increment_minor": lot.IncrementMinor,
		"bid_count":       lot.BidCount,
		"closes_at":       lot.ClosesAt.Format(time.RFC3339),
		// Whether the lot will actually trade. The reserve *amount* is never
		// sent — publishing it turns the auction into a fixed-price sale with
		// extra steps.
		"reserve_met": lot.ReserveMet(),
	}
	return res, nil
}
