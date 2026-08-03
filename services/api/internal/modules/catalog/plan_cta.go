package catalog

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/marketplace/api/internal/seam/action"
)

// PlanHandler owns the (subscribe_plan, meal_plan) CTA.
//
// It lives here rather than in the meals vertical because `subscription_plans`
// is a catalogue table — DATA-MODEL.md §3 lists it under Catalog, not under
// Subscriptions — and this package exists precisely so callers can ask "is this
// bookable" without importing the catalogue's tables.
type PlanHandler struct {
	pool *pgxpool.Pool
}

func NewPlanHandler(pool *pgxpool.Pool) *PlanHandler { return &PlanHandler{pool: pool} }

// Resolve renders the subscribe CTA.
//
// Availability is answered before the tap, for the same reason the booking CTA
// answers it before the tap: in a feed where the next swipe is free, a customer
// who taps, waits for a screen, and is then told the kitchen does not deliver
// to them is a customer lost to a question we could have answered first.
//
// The stakes are higher here than for a single box. A subscription is weeks of
// commitment and the highest-value conversion in the product; sending someone
// into a signup flow that cannot complete wastes the best intent signal the
// feed will ever produce.
func (h *PlanHandler) Resolve(ctx context.Context, a action.Action, v action.Viewer) (action.Resolved, error) {
	label, labelAr := "Subscribe", "اشترك"
	label, labelAr = action.Override(label, labelAr, a.Label, a.LabelAr)

	res := action.Resolved{
		Label:     label,
		LabelAr:   labelAr,
		Available: true,
		Route:     fmt.Sprintf("/plans/%s/subscribe", a.TargetID),
	}

	var planActive, branchActive bool
	var priceMinor int64
	var currency string
	var daysPerWeek, mealsPerDay int
	var inArea *bool

	err := h.pool.QueryRow(ctx, `
		SELECT p.is_active,
		       (b.is_active AND b.deleted_at IS NULL AND v.status = 'active'),
		       p.price_minor, p.currency, p.days_per_week, p.meals_per_day,
		       CASE
		           WHEN $2::float8 IS NULL OR b.service_area IS NULL THEN NULL
		           ELSE ST_Intersects(b.service_area,
		                    ST_SetSRID(ST_MakePoint($3, $2), 4326)::geography)
		       END
		  FROM subscription_plans p
		  JOIN vendor_branches b ON b.id = p.branch_id
		  JOIN vendors v ON v.id = b.vendor_id
		 WHERE p.id = $1`,
		a.TargetID, nullableFloat(v.Lat), nullableFloat(v.Lng),
	).Scan(&planActive, &branchActive, &priceMinor, &currency,
		&daysPerWeek, &mealsPerDay, &inArea)

	if errors.Is(err, pgx.ErrNoRows) {
		// The plan was withdrawn after the reel was published. Stale content
		// pointing at nothing is a real state, not an error — it renders
		// unavailable and gets cleaned up, rather than 500-ing the feed.
		res.Available = false
		res.Reason = "This plan is no longer offered"
		return res, nil
	}
	if err != nil {
		// Optimistic on error, same as the booking CTA. The signup flow refuses
		// properly and can explain why.
		return res, nil
	}

	// The price and shape ride along in Meta so the client can render "AED 449 ·
	// 5 days a week" on the tile without a second round-trip. A subscribe button
	// with no price attached is the one CTA nobody taps.
	res.Meta = map[string]any{
		"price_minor":   priceMinor,
		"currency":      currency,
		"days_per_week": daysPerWeek,
		"meals_per_day": mealsPerDay,
	}

	switch {
	case !planActive:
		res.Available = false
		res.Reason = "This plan is not currently available"
	case !branchActive:
		res.Available = false
		res.Reason = "This kitchen is not currently accepting subscriptions"
	case inArea != nil && !*inArea:
		res.Available = false
		res.Reason = "Outside this kitchen's delivery area"
	}
	return res, nil
}
