package attribution

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Funnel is one reel's conversion record over a date range.
//
// Every stage is a raw count. There is deliberately no `conversion_rate` field,
// and that omission is the most important decision in this file.
//
// "Conversion rate" is meaningless without its denominator, and the available
// denominators differ by an order of magnitude: orders ÷ clicks flatters
// roughly tenfold against orders ÷ impressions. A single field forces whoever
// renders it to pick one, and the one picked in a board deck is never the
// conservative one. Shipping the counts and computing rates at the call site,
// with the denominator named, makes that choice explicit every time.
type Funnel struct {
	ReelID uuid.UUID
	From   time.Time
	To     time.Time

	Impressions int64
	Views       int64
	Clicks      int64

	// Split by model and never summed. A view-through order and a click-through
	// order are different claims about whether the feed works, and adding them
	// asserts they are the same claim.
	OrdersLastClick   int64
	OrdersDeclared    int64
	OrdersViewThrough int64

	// GMV from verifiable attributions only. The unverifiable models are
	// counted above but contribute no revenue figure, because a revenue number
	// nobody can defend is worse than no revenue number.
	GMVLastClickMinor int64
	Currency          string

	// OffFeedOrders is the subset that arrived by shared link or deep link
	// rather than through the feed. Reported separately because a vendor
	// pushing their own reel to a WhatsApp group converts at rates the feed
	// will never approach, and folding that in makes the feed look like it
	// works when it does not.
	OffFeedOrders int64
}

// ClickThroughRate is clicks ÷ impressions. Named for its denominator.
func (f Funnel) ClickThroughRate() float64 {
	if f.Impressions == 0 {
		return 0
	}
	return float64(f.Clicks) / float64(f.Impressions)
}

// OrderRatePerClick is the flattering one, and it is named so nobody quotes it
// by accident.
func (f Funnel) OrderRatePerClick() float64 {
	if f.Clicks == 0 {
		return 0
	}
	return float64(f.OrdersLastClick) / float64(f.Clicks)
}

// OrderRatePerImpression is the number that answers "is the feed worth its
// egress bill", because impressions are what the egress is spent on.
func (f Funnel) OrderRatePerImpression() float64 {
	if f.Impressions == 0 {
		return 0
	}
	return float64(f.OrdersLastClick) / float64(f.Impressions)
}

// Funnel computes one reel's record from the raw tables.
//
// Reads the source tables rather than the daily rollup so that a vendor
// checking a reel they published an hour ago sees something. The rollup exists
// for range queries across many reels, where scanning impressions is not
// viable.
func (r *Repository) Funnel(ctx context.Context, reelID uuid.UUID, from, to time.Time) (*Funnel, error) {
	f := &Funnel{ReelID: reelID, From: from, To: to}

	err := r.pool.QueryRow(ctx, `
		SELECT
		  (SELECT count(*) FROM feed_impressions i
		    WHERE i.reel_id = $1 AND i.served_at >= $2 AND i.served_at < $3),
		  (SELECT count(*) FROM client_events e
		    WHERE e.reel_id = $1 AND e.event_type = 'view_progress'
		      AND e.occurred_at >= $2 AND e.occurred_at < $3),
		  (SELECT count(*) FROM action_clicks c
		    WHERE c.reel_id = $1 AND c.clicked_at >= $2 AND c.clicked_at < $3)`,
		reelID, from, to,
	).Scan(&f.Impressions, &f.Views, &f.Clicks)
	if err != nil {
		return nil, fmt.Errorf("attribution: funnel counts: %w", err)
	}

	// Orders in one pass, bucketed by model. FILTER rather than three scans:
	// this is the expensive half and it reads the same rows each time.
	err = r.pool.QueryRow(ctx, `
		SELECT
		  count(*) FILTER (WHERE model = 'last_click'),
		  count(*) FILTER (WHERE model = 'declared'),
		  count(*) FILTER (WHERE model = 'view_through'),
		  COALESCE(sum(gmv_minor) FILTER (WHERE model = 'last_click'), 0),
		  count(*) FILTER (WHERE off_feed),
		  COALESCE(max(currency), '')
		  FROM order_attributions
		 WHERE reel_id = $1 AND attributed_at >= $2 AND attributed_at < $3`,
		reelID, from, to,
	).Scan(&f.OrdersLastClick, &f.OrdersDeclared, &f.OrdersViewThrough,
		&f.GMVLastClickMinor, &f.OffFeedOrders, &f.Currency)
	if err != nil {
		return nil, fmt.Errorf("attribution: funnel orders: %w", err)
	}

	return f, nil
}

// PlatformFunnel is the number the Phase 3 decision actually rests on: across
// every reel, does the feed produce bookings?
type PlatformFunnel struct {
	From, To time.Time

	Impressions int64
	Clicks      int64

	OrdersLastClick   int64
	OrdersDeclared    int64
	OrdersViewThrough int64
	// OrdersOrganic is orders that attributed to no reel. The denominator that
	// makes the rest interpretable: "the feed drove 400 orders" means one thing
	// against 500 total and another against 50,000.
	OrdersOrganic int64

	GMVLastClickMinor int64
	GMVTotalMinor     int64
	Currency          string

	ReelsWithImpressions int64
	ReelsWithAnyOrder    int64
}

// FeedShareOfOrders is the honest headline: verifiable feed-driven orders as a
// share of all orders placed.
func (p PlatformFunnel) FeedShareOfOrders() float64 {
	total := p.OrdersLastClick + p.OrdersDeclared + p.OrdersViewThrough + p.OrdersOrganic
	if total == 0 {
		return 0
	}
	return float64(p.OrdersLastClick) / float64(total)
}

// PlatformFunnel aggregates across all reels.
func (r *Repository) PlatformFunnel(ctx context.Context, from, to time.Time) (*PlatformFunnel, error) {
	p := &PlatformFunnel{From: from, To: to}

	err := r.pool.QueryRow(ctx, `
		SELECT
		  count(*) FILTER (WHERE model = 'last_click'),
		  count(*) FILTER (WHERE model = 'declared'),
		  count(*) FILTER (WHERE model = 'view_through'),
		  count(*) FILTER (WHERE model = 'none'),
		  COALESCE(sum(gmv_minor) FILTER (WHERE model = 'last_click'), 0),
		  COALESCE(sum(gmv_minor), 0),
		  COALESCE(max(currency), ''),
		  count(DISTINCT reel_id) FILTER (WHERE reel_id IS NOT NULL)
		  FROM order_attributions
		 WHERE attributed_at >= $1 AND attributed_at < $2`,
		from, to,
	).Scan(&p.OrdersLastClick, &p.OrdersDeclared, &p.OrdersViewThrough,
		&p.OrdersOrganic, &p.GMVLastClickMinor, &p.GMVTotalMinor,
		&p.Currency, &p.ReelsWithAnyOrder)
	if err != nil {
		return nil, fmt.Errorf("attribution: platform orders: %w", err)
	}

	err = r.pool.QueryRow(ctx, `
		SELECT
		  (SELECT count(*) FROM action_clicks c
		    WHERE c.clicked_at >= $1 AND c.clicked_at < $2),
		  (SELECT count(*) FROM feed_impressions i
		    WHERE i.served_at >= $1 AND i.served_at < $2),
		  (SELECT count(DISTINCT i.reel_id) FROM feed_impressions i
		    WHERE i.served_at >= $1 AND i.served_at < $2)`,
		from, to,
	).Scan(&p.Clicks, &p.Impressions, &p.ReelsWithImpressions)
	if err != nil {
		return nil, fmt.Errorf("attribution: platform impressions: %w", err)
	}

	return p, nil
}

// RollupDay recomputes one day's per-reel funnel into reel_funnel_daily.
//
// Recompute-and-replace rather than incremental accumulation. Events arrive
// late — a mobile client that was offline delivers yesterday's batch this
// morning — and an incremental counter has no way to absorb that without
// double-counting. Recomputing a closed day is cheap and is always right.
func (r *Repository) RollupDay(ctx context.Context, day time.Time) (int64, error) {
	from := day.UTC().Truncate(24 * time.Hour)
	to := from.Add(24 * time.Hour)

	tag, err := r.pool.Exec(ctx, `
		INSERT INTO reel_funnel_daily
		    (reel_id, day, impressions, views, clicks,
		     orders_last_click, orders_declared, orders_view_through,
		     gmv_last_click_minor, currency, computed_at)
		SELECT r.id, $1::date,
		       COALESCE(i.n, 0), COALESCE(v.n, 0), COALESCE(c.n, 0),
		       COALESCE(a.last_click, 0), COALESCE(a.declared, 0),
		       COALESCE(a.view_through, 0), COALESCE(a.gmv, 0),
		       COALESCE(a.currency, 'AED'), now()
		  FROM reels r
		  LEFT JOIN LATERAL (
		      SELECT count(*) AS n FROM feed_impressions fi
		       WHERE fi.reel_id = r.id AND fi.served_at >= $2 AND fi.served_at < $3
		  ) i ON true
		  LEFT JOIN LATERAL (
		      SELECT count(*) AS n FROM client_events e
		       WHERE e.reel_id = r.id AND e.event_type = 'view_progress'
		         AND e.occurred_at >= $2 AND e.occurred_at < $3
		  ) v ON true
		  LEFT JOIN LATERAL (
		      SELECT count(*) AS n FROM action_clicks ac
		       WHERE ac.reel_id = r.id AND ac.clicked_at >= $2 AND ac.clicked_at < $3
		  ) c ON true
		  LEFT JOIN LATERAL (
		      SELECT count(*) FILTER (WHERE oa.model = 'last_click')   AS last_click,
		             count(*) FILTER (WHERE oa.model = 'declared')     AS declared,
		             count(*) FILTER (WHERE oa.model = 'view_through') AS view_through,
		             COALESCE(sum(oa.gmv_minor) FILTER (WHERE oa.model = 'last_click'), 0) AS gmv,
		             max(oa.currency) AS currency
		        FROM order_attributions oa
		       WHERE oa.reel_id = r.id
		         AND oa.attributed_at >= $2 AND oa.attributed_at < $3
		  ) a ON true
		 WHERE r.state = 'published'
		   -- Only reels that did something that day. Writing a zero row for
		   -- every published reel every day makes the table grow with the
		   -- catalogue rather than with the traffic.
		   AND (COALESCE(i.n,0) + COALESCE(v.n,0) + COALESCE(c.n,0) + COALESCE(a.last_click,0)) > 0
		ON CONFLICT (reel_id, day) DO UPDATE
		   SET impressions = EXCLUDED.impressions,
		       views = EXCLUDED.views,
		       clicks = EXCLUDED.clicks,
		       orders_last_click = EXCLUDED.orders_last_click,
		       orders_declared = EXCLUDED.orders_declared,
		       orders_view_through = EXCLUDED.orders_view_through,
		       gmv_last_click_minor = EXCLUDED.gmv_last_click_minor,
		       currency = EXCLUDED.currency,
		       computed_at = now()`,
		from, from, to)
	if err != nil {
		return 0, fmt.Errorf("attribution: rollup day: %w", err)
	}
	return tag.RowsAffected(), nil
}
