// Package catalog answers questions about what is bookable, for callers that
// must not import the catalog's tables.
//
// It exists so the verticals stay dependency-free. laundry.Eligibility is
// declared in laundry and implemented here: the interface belongs to the
// consumer, the tables belong to this package, and neither imports the other's
// concerns.
package catalog

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Eligibility struct {
	pool *pgxpool.Pool
}

func NewEligibility(pool *pgxpool.Pool) *Eligibility { return &Eligibility{pool: pool} }

// OfferingBookable resolves a CTA's availability in one query.
//
// One query and not three, because this runs once per CTA on every feed page —
// up to 60 times for a page of 20 reels with 3 actions each. Three round-trips
// per CTA would put 180 queries behind a single feed request, which is how a
// read path that was fine in testing falls over on the first real morning.
//
// Reasons are written for the customer, not for a log. They are rendered
// verbatim next to a disabled button, and "eligibility_check_failed" next to a
// greyed-out Book button reads as a broken app.
func (e *Eligibility) OfferingBookable(ctx context.Context, offeringID uuid.UUID, lat, lng float64) (bool, string, error) {
	var offeringActive, branchActive bool
	var inArea *bool

	err := e.pool.QueryRow(ctx, `
		SELECT o.is_active,
		       (b.is_active AND b.deleted_at IS NULL AND v.status = 'active'),
		       CASE
		           WHEN $2::float8 IS NULL OR b.service_area IS NULL THEN NULL
		           ELSE ST_Intersects(b.service_area,
		                    ST_SetSRID(ST_MakePoint($3, $2), 4326)::geography)
		       END
		  FROM service_offerings o
		  JOIN vendor_branches b ON b.id = o.branch_id
		  JOIN vendors v ON v.id = b.vendor_id
		 WHERE o.id = $1`,
		offeringID, nullableFloat(lat), nullableFloat(lng),
	).Scan(&offeringActive, &branchActive, &inArea)

	if errors.Is(err, pgx.ErrNoRows) {
		// The offering was deleted after the reel was published. The reel is
		// stale content pointing at nothing, which is a real state and not an
		// error — it should render as unavailable and get cleaned up by the
		// reconciliation job, not 500 the feed.
		return false, "This service is no longer offered", nil
	}
	if err != nil {
		return false, "", fmt.Errorf("catalog: offering eligibility: %w", err)
	}

	switch {
	case !offeringActive:
		return false, "This service is not currently available", nil
	case !branchActive:
		return false, "This vendor is not currently accepting orders", nil
	case inArea != nil && !*inArea:
		// The asymmetry from PLAN-VIDEO-FIRST §3, enforced: services filter by
		// containment, not distance. A vendor two kilometres away who does not
		// cross the creek is not a candidate, and saying "too far" would be
		// both wrong and confusing.
		return false, "Outside this vendor's delivery area", nil
	}

	// inArea == nil means the viewer sent no location, or the branch has drawn
	// no service area. Both render as available: the booking flow checks
	// properly with a real address, and refusing here would hide every vendor
	// from an anonymous viewer who has not granted location permission — which
	// is most first-time viewers.
	return true, "", nil
}

func nullableFloat(f float64) any {
	if f == 0 {
		return nil
	}
	return f
}
