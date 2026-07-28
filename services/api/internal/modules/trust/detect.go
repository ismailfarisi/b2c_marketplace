package trust

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/marketplace/api/internal/platform/postgres"
)

// The cross-listing detectors.
//
// Everything here needs rows the listing being written does not have — a
// previous listing of the same car, the price of comparable cars, the seller's
// history — which is why they run in the worker rather than inline. The inline
// checks (odometer against model year, VIN check digit) live in
// vehicles.screen; the split is "does this need a second row".
//
// All four are written as INSERT … SELECT with the same ON CONFLICT DO NOTHING
// as Raise, so re-running them is free and produces no duplicate evidence. That
// is what makes it safe to run them every fifteen minutes rather than nightly:
// the detector's cost is a query, and the cost of finding a stolen car twelve
// hours late is somebody's money.

// DetectOdometerRollback finds a VIN whose mileage went backwards.
//
// The single most common used-car fraud, and one of the few that leaves
// unambiguous evidence: an odometer does not decrease. Only the platform's own
// history is available — a rollback done before the car was ever listed here is
// invisible without a Vehicle Gate integration (SCOPE-EXTENSIONS §3), which is
// the moat and is not built.
//
// The 1,000 km tolerance absorbs sellers rounding down and typos, and the
// requirement that the later listing be *newer* rather than merely lower-priced
// keeps two concurrent listings of the same car from flagging each other.
func DetectOdometerRollback(ctx context.Context, s *Service, limit int) (int64, error) {
	return s.detect(ctx, `
		INSERT INTO fraud_signals (subject_type, subject_id, kind, severity, source, detail)
		SELECT 'listing', later.id, 'odometer_rollback',
		       -- Confidence scales with the size of the discrepancy. A 3,000 km
		       -- reversal is a mistake; a 90,000 km reversal is a decision.
		       LEAST(100, 40 + (earlier.mileage_km - later.mileage_km) / 1000)::smallint,
		       'system',
		       jsonb_build_object(
		           'vin', later.vin,
		           'previous_listing_id', earlier.id,
		           'previous_mileage_km', earlier.mileage_km,
		           'current_mileage_km', later.mileage_km)
		  FROM vehicle_listings later
		  JOIN vehicle_listings earlier
		    ON earlier.vin = later.vin
		   AND earlier.id <> later.id
		   AND earlier.created_at < later.created_at
		 WHERE later.vin IS NOT NULL
		   AND later.status IN ('pending_review', 'active')
		   AND later.mileage_km < earlier.mileage_km - 1000
		   AND later.created_at > now() - interval '90 days'
		 LIMIT $1
		ON CONFLICT (subject_type, subject_id, kind) WHERE state = 'open' DO NOTHING`, limit)
}

// DetectDuplicateListing finds one car listed more than once at the same time.
//
// Two shapes, both worth catching for different reasons. The same seller
// listing a car twice is usually shelf-space abuse — it doubles their presence
// in search and it is what listing quotas exist to prevent. Two *different*
// sellers on one VIN is the serious one: at most one of them owns the car.
func DetectDuplicateListing(ctx context.Context, s *Service, limit int) (int64, error) {
	return s.detect(ctx, `
		INSERT INTO fraud_signals (subject_type, subject_id, kind, severity, source, detail)
		SELECT 'listing', a.id, 'duplicate_listing',
		       CASE WHEN a.seller_id <> b.seller_id THEN 85 ELSE 40 END,
		       'system',
		       jsonb_build_object(
		           'vin', a.vin,
		           'other_listing_id', b.id,
		           'same_seller', a.seller_id = b.seller_id)
		  FROM vehicle_listings a
		  JOIN vehicle_listings b
		    ON b.vin = a.vin AND b.id <> a.id
		 WHERE a.vin IS NOT NULL
		   AND a.status = 'active' AND b.status = 'active'
		 LIMIT $1
		ON CONFLICT (subject_type, subject_id, kind) WHERE state = 'open' DO NOTHING`, limit)
}

// DetectPriceOutlier finds listings priced far below their comparables.
//
// The bait: a car at half the market rate that turns out to need a deposit
// before viewing. The comparison is against the median of *asking* prices for
// the same make, model and year band, and that is a real limitation stated
// plainly — the platform never observes a sale, so there is no transaction
// price to compare against and there is no honest way to build one from this
// data (see §6 of the phase doc, on why `market_valuations` was deferred).
//
// The floor of five comparables is what stops a rare trim's only listing being
// flagged against nothing.
func DetectPriceOutlier(ctx context.Context, s *Service, limit int) (int64, error) {
	return s.detect(ctx, `
		WITH comparables AS (
			-- Grouped, not windowed. percentile_cont is an ordered-set
			-- aggregate and Postgres refuses OVER on those; the aggregate form
			-- is what actually runs.
			SELECT l.id, l.price_minor,
			       percentile_cont(0.5) WITHIN GROUP (ORDER BY peer.price_minor) AS median_minor,
			       count(peer.id) AS peer_count
			  FROM vehicle_listings l
			  JOIN vehicle_listings peer
			    ON peer.model_id = l.model_id
			   AND peer.year BETWEEN l.year - 1 AND l.year + 1
			   AND peer.id <> l.id
			   AND peer.status = 'active'
			 WHERE l.status = 'active'
			   AND l.published_at > now() - interval '30 days'
			 GROUP BY l.id, l.price_minor
		)
		INSERT INTO fraud_signals (subject_type, subject_id, kind, severity, source, detail)
		SELECT 'listing', c.id, 'price_outlier_low', 60, 'system',
		       jsonb_build_object(
		           'price_minor', c.price_minor,
		           'peer_median_minor', c.median_minor::bigint,
		           'peer_count', c.peer_count)
		  FROM comparables c
		 WHERE c.peer_count >= 5
		   AND c.price_minor * 2 < c.median_minor
		 LIMIT $1
		ON CONFLICT (subject_type, subject_id, kind) WHERE state = 'open' DO NOTHING`, limit)
}

// DetectRapidRelist finds a seller cycling the same car through the listing
// lifecycle.
//
// Mostly this is a seller gaming recency — delete and relist to jump to the top
// of "newest", which is free if nobody is counting. Occasionally it is somebody
// staying ahead of reports. Either way the count is the evidence, and it is a
// user-level signal rather than a listing-level one because no single listing
// is the problem.
func DetectRapidRelist(ctx context.Context, s *Service, limit int) (int64, error) {
	return s.detect(ctx, `
		INSERT INTO fraud_signals (subject_type, subject_id, kind, severity, source, detail)
		SELECT 'user', seller_id, 'rapid_relist',
		       LEAST(100, 30 + (count(*) - 3) * 10)::smallint, 'system',
		       jsonb_build_object('vin', vin, 'listings_30d', count(*))
		  FROM vehicle_listings
		 WHERE vin IS NOT NULL
		   AND created_at > now() - interval '30 days'
		 GROUP BY seller_id, vin
		HAVING count(*) >= 3
		 LIMIT $1
		ON CONFLICT (subject_type, subject_id, kind) WHERE state = 'open' DO NOTHING`, limit)
}

// DetectUnverifiedHighValue is a prompt for verification, not an accusation.
//
// A private seller with no verified identity listing a car above the threshold
// is the profile advance-fee scams use, and it is also a perfectly ordinary
// person who has not finished onboarding. It is weighted accordingly (15
// points) — enough to put them in front of a reviewer alongside anything else,
// never enough to restrict on its own.
const HighValueMinor = 15_000_000 // AED 150,000

func DetectUnverifiedHighValue(ctx context.Context, s *Service, limit int) (int64, error) {
	return s.detect(ctx, fmt.Sprintf(`
		INSERT INTO fraud_signals (subject_type, subject_id, kind, severity, source, detail)
		SELECT 'listing', l.id, 'unverified_high_value', 40, 'system',
		       jsonb_build_object('price_minor', l.price_minor)
		  FROM vehicle_listings l
		 WHERE l.status = 'active'
		   AND l.seller_kind = 'private'
		   AND l.price_minor >= %d
		   AND NOT EXISTS (
		       SELECT 1 FROM seller_verifications v
		        WHERE v.user_id = l.seller_id
		          AND v.kind IN ('emirates_id', 'trade_licence')
		          AND v.state = 'verified')
		 LIMIT $1
		ON CONFLICT (subject_type, subject_id, kind) WHERE state = 'open' DO NOTHING`,
		HighValueMinor), limit)
}

// RunDetectors runs every cross-listing detector.
//
// Each in its own transaction. One detector hitting a lock or a bad plan must
// not take the other four down with it — the value of this set is that it is a
// set, and four out of five running is far better than none.
func RunDetectors(ctx context.Context, s *Service, limit int) (int64, error) {
	detectors := []struct {
		name string
		fn   func(context.Context, *Service, int) (int64, error)
	}{
		{"odometer_rollback", DetectOdometerRollback},
		{"duplicate_listing", DetectDuplicateListing},
		{"price_outlier", DetectPriceOutlier},
		{"rapid_relist", DetectRapidRelist},
		{"unverified_high_value", DetectUnverifiedHighValue},
	}

	var total int64
	var firstErr error
	for _, d := range detectors {
		n, err := d.fn(ctx, s, limit)
		if err != nil && firstErr == nil {
			firstErr = fmt.Errorf("%s: %w", d.name, err)
			continue
		}
		total += n
	}
	return total, firstErr
}

func (s *Service) detect(ctx context.Context, sql string, limit int) (int64, error) {
	if limit <= 0 {
		limit = 500
	}
	var affected int64
	err := postgres.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, sql, limit)
		if err != nil {
			return err
		}
		affected = tag.RowsAffected()
		return nil
	})
	return affected, err
}
