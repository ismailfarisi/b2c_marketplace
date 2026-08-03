package vehicles

import (
	"context"
	"fmt"

	"github.com/google/uuid"
)

// MyListing is one row of the seller's own list.
//
// Deliberately not the buyer-facing search Result. A seller needs to see the
// things a buyer must never be shown — a draft, a rejection and its reason, a
// listing that expired last week — and reusing the search projection would
// either leak those to buyers or hide them from the person who needs them most.
type MyListing struct {
	ID          uuid.UUID
	ReferenceNo string
	Status      Status
	Title       string
	Year        int
	MileageKM   int
	PriceMinor  int64
	Currency    string
	City        string
	// ReelID is the reel filmed for this listing, when there is one. Null for a
	// listing created through the web form, which is a normal state.
	ReelID *uuid.UUID
	// ReelState is that reel's pipeline state — the seller's answer to "why is
	// my car not showing up", which is far more often about the video than the
	// listing.
	ReelState string
	// PosterKey and LQIP are the reel's still and its blurhash placeholder.
	//
	// On this screen they matter for a reason they do not on the feed: a
	// seller scanning their own listings is looking for one specific car, and
	// four rows of identical grey rectangles is the slowest possible way to
	// find it. Empty while the reel is still transcoding, which is exactly the
	// row the seller is most likely to be looking for.
	PosterKey   string
	LQIP        string
	PublishedAt *string
	ExpiresAt   *string
	SoldAt      *string
}

// MyListings is everything this seller has, in every state.
//
// Ordered by how much attention each needs rather than by date: anything
// awaiting the seller or a reviewer first, then live, then finished. A seller
// opening this screen is almost always here because something is wrong with
// one listing, and making them scroll past six sold cars to find it is the
// difference between fixing it and abandoning it.
func (s *Service) MyListings(ctx context.Context, sellerID uuid.UUID, limit int) ([]MyListing, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}

	rows, err := s.pool.Query(ctx, `
		SELECT l.id, l.reference_no, l.status,
		       -- Composed here when the seller gave no title, which is every
		       -- listing the app creates: the sell flow has no title field,
		       -- because the overlay's words come from Summarize. A seller
		       -- looking at a list of their own cars still has to be able to
		       -- tell them apart, and "(untitled)" does not do that.
		       COALESCE(NULLIF(l.title, ''),
		                l.year || ' ' || mk.name || ' ' || md.name),
		       l.year, l.mileage_km, l.price_minor, l.currency,
		       COALESCE(l.city, l.emirate, ''),
		       r.id, COALESCE(r.state::text, ''),
		       COALESCE(r.poster_key, ''), COALESCE(r.lqip, ''),
		       to_char(l.published_at, 'YYYY-MM-DD"T"HH24:MI:SSOF:00'),
		       to_char(l.expires_at,   'YYYY-MM-DD"T"HH24:MI:SSOF:00'),
		       to_char(l.sold_at,      'YYYY-MM-DD"T"HH24:MI:SSOF:00')
		  FROM vehicle_listings l
		  JOIN vehicle_makes mk ON mk.id = l.make_id
		  JOIN vehicle_models md ON md.id = l.model_id
		  -- The newest reel about this listing. LATERAL rather than a plain join
		  -- because a relisted car legitimately has several, and a join would
		  -- return the listing once per reel.
		  LEFT JOIN LATERAL (
		      SELECT id, state, poster_key, lqip FROM reels
		       WHERE subject_type = 'vehicle_listing' AND subject_id = l.id
		         AND state <> 'removed'
		       ORDER BY created_at DESC
		       LIMIT 1
		  ) r ON true
		 WHERE l.seller_id = $1 AND l.status <> 'removed'
		 ORDER BY CASE l.status
		              WHEN 'rejected'       THEN 0
		              WHEN 'draft'          THEN 1
		              WHEN 'pending_review' THEN 2
		              WHEN 'active'         THEN 3
		              WHEN 'paused'         THEN 4
		              WHEN 'expired'        THEN 5
		              ELSE 6
		          END,
		          l.created_at DESC
		 LIMIT $2`, sellerID, limit)
	if err != nil {
		return nil, fmt.Errorf("vehicles: my listings: %w", err)
	}
	defer rows.Close()

	var out []MyListing
	for rows.Next() {
		var m MyListing
		var status string
		if err := rows.Scan(&m.ID, &m.ReferenceNo, &status, &m.Title,
			&m.Year, &m.MileageKM, &m.PriceMinor, &m.Currency, &m.City,
			&m.ReelID, &m.ReelState, &m.PosterKey, &m.LQIP,
			&m.PublishedAt, &m.ExpiresAt, &m.SoldAt); err != nil {
			return nil, fmt.Errorf("vehicles: scan my listing: %w", err)
		}
		m.Status = Status(status)
		out = append(out, m)
	}
	return out, rows.Err()
}
