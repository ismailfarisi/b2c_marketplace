package api

import (
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/marketplace/api/internal/httpx"
	"github.com/marketplace/api/internal/modules/auctions"
)

// AuctionHandlers is the plate-auction surface.
type AuctionHandlers struct {
	Auctions *auctions.Service
}

type bidBody struct {
	AmountMinor int64 `json:"amount_minor"`
}

// PlaceBid is the one endpoint in this product where a tap creates a binding
// obligation, and it is written accordingly.
//
// A refused bid returns 409 with a machine-readable reason and, for the common
// case, the amount that *would* have worked. "Too low" alone makes the bidder
// guess, and on a phone they guess by tapping the same wrong number again.
func (h *AuctionHandlers) PlaceBid(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpx.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteProblem(w, r, httpx.ErrUnauthorized)
		return
	}
	auctionID, err := uuid.Parse(chi.URLParam(r, "auctionId"))
	if err != nil {
		httpx.WriteProblem(w, r, httpx.ErrNotFound)
		return
	}

	var body bidBody
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}

	lot, err := h.Auctions.Bid(r.Context(), auctionID, userID, body.AmountMinor)
	if err != nil {
		var refused *auctions.BidError
		if errors.As(err, &refused) {
			// Coded rather than a bare 409, because the five refusals need five
			// different screens: "come back when it opens" and "you already
			// hold the high bid" are not the same news.
			httpx.WriteJSON(w, http.StatusConflict, "application/problem+json", map[string]any{
				"type":   "about:blank",
				"title":  "Bid refused",
				"status": http.StatusConflict,
				"code":   string(refused.Reason),
				// The number that would work, when there is one. Zero means
				// there is no amount that would help — the lot is closed.
				"ask_minor": refused.AskMinor,
				"currency":  refused.Currency,
				"instance":  r.URL.Path,
			})
			return
		}
		httpx.WriteProblem(w, r, err)
		return
	}

	httpx.WriteJSON(w, http.StatusCreated, "", map[string]any{
		"current_minor": lot.CurrentMinor,
		"currency":      lot.Currency,
		"bid_count":     lot.BidCount,
		"ask_minor":     lot.AskMinor(),
		// Echoed because a bid may have moved it. A client that keeps its own
		// countdown and never re-reads this shows a lot closing while bidding
		// is still open, which is the worst possible clock to be wrong.
		"closes_at":   lot.ClosesAt.Format(time.RFC3339),
		"reserve_met": lot.ReserveMet(),
		"leading":     true,
	})
}

// BidHistory is the lot's bid list.
func (h *AuctionHandlers) BidHistory(w http.ResponseWriter, r *http.Request) {
	auctionID, err := uuid.Parse(chi.URLParam(r, "auctionId"))
	if err != nil {
		httpx.WriteProblem(w, r, httpx.ErrNotFound)
		return
	}
	uid, hasUser := httpx.UserIDFromContext(r.Context())

	bids, err := h.Auctions.History(r.Context(), auctionID,
		uuid.NullUUID{UUID: uid, Valid: hasUser},
		atoiOr(r.URL.Query().Get("limit"), 25))
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}

	items := make([]map[string]any, 0, len(bids))
	for _, b := range bids {
		items = append(items, map[string]any{
			"amount_minor": b.Amount,
			// Whether this bid pushed the close out. Surfaced because "why did
			// this lot close four minutes late" is the commonest question about
			// any auction with anti-sniping.
			"extended":  b.Extended,
			"placed_at": b.PlacedAt.Format(time.RFC3339),
			// Whose bid it was, reduced to the only fact a viewer needs. A
			// public list of who bid what hands a competitor a map of the
			// platform's buyers, and on a plate the bidder's identity is the one
			// thing nobody needs to know.
			"mine": b.IsMine,
		})
	}
	httpx.WriteJSON(w, http.StatusOK, "", map[string]any{"items": items})
}
