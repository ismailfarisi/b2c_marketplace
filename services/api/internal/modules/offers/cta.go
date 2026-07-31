package offers

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/marketplace/api/internal/seam/action"
)

// ClaimHandler owns the (claim_offer, flash_offer) CTA.
//
// The availability question is sharper here than anywhere else in the product,
// because for this vertical it changes *during the reel*. A car is for sale all
// day; an offer opens at 22:00, and a viewer who taps at 21:58 and one who taps
// at 22:01 must get different answers from the same button.
type ClaimHandler struct {
	pool *pgxpool.Pool
}

func NewClaimHandler(pool *pgxpool.Pool) *ClaimHandler { return &ClaimHandler{pool: pool} }

// Resolve renders the claim CTA and says why it is not live when it is not.
//
// Three distinct unavailable states, and they are worth keeping apart: "not open
// yet" is an invitation to come back, "gone" is a reason to stop looking, and
// "closed for tonight" is neither. Collapsing them into a single greyed button
// would tell a viewer their offer is dead when it opens again in two hours.
func (h *ClaimHandler) Resolve(ctx context.Context, a action.Action, v action.Viewer) (action.Resolved, error) {
	label, labelAr := "Claim offer", "احصل على العرض"
	if a.Label != "" {
		label, labelAr = a.Label, a.LabelAr
	}

	res := action.Resolved{
		Label:     label,
		LabelAr:   labelAr,
		Available: true,
		Route:     fmt.Sprintf("/offers/%s/claim", a.TargetID),
	}

	var (
		startMinutes, endMinutes int
		days                     []int16
		runsFrom, runsUntil      *time.Time
		isActive, branchLive     bool
		quantity                 *int
		claimed                  int
		zone                     *string
		inArea                   *bool
	)
	err := h.pool.QueryRow(ctx, `
		SELECT EXTRACT(hour FROM o.starts_at_local) * 60 + EXTRACT(minute FROM o.starts_at_local),
		       EXTRACT(hour FROM o.ends_at_local)   * 60 + EXTRACT(minute FROM o.ends_at_local),
		       o.days_of_week, o.runs_from, o.runs_until,
		       o.is_active, (b.is_active AND b.deleted_at IS NULL AND v.status = 'active'),
		       o.quantity, o.claimed, b.timezone,
		       CASE
		           WHEN $2::float8 IS NULL OR b.service_area IS NULL THEN NULL
		           ELSE ST_Intersects(b.service_area,
		                    ST_SetSRID(ST_MakePoint($3, $2), 4326)::geography)
		       END
		  FROM flash_offers o
		  JOIN vendor_branches b ON b.id = o.branch_id
		  JOIN vendors v ON v.id = b.vendor_id
		 WHERE o.id = $1`,
		a.TargetID, nullableFloat(v.Lat), nullableFloat(v.Lng),
	).Scan(&startMinutes, &endMinutes, &days, &runsFrom, &runsUntil,
		&isActive, &branchLive, &quantity, &claimed, &zone, &inArea)

	if errors.Is(err, pgx.ErrNoRows) {
		res.Available = false
		res.Reason = "This offer is no longer running"
		return res, nil
	}
	if err != nil {
		// Optimistic on error, matching the other handlers: the claim endpoint
		// refuses properly and can explain itself.
		return res, nil
	}

	loc := time.UTC
	if zone != nil && *zone != "" {
		if l, err := time.LoadLocation(*zone); err == nil {
			loc = l
		}
	}

	w := Window{StartLocal: startMinutes, EndLocal: endMinutes}
	for _, d := range days {
		w.Days = append(w.Days, int(d))
	}
	if runsFrom != nil {
		w.RunsFrom = runsFrom.In(loc)
	}
	if runsUntil != nil {
		w.RunsUntil = runsUntil.In(loc)
	}

	now := time.Now().In(loc)
	occ, hasOccurrence := w.Resolve(now)

	// Carried so a client can render "opens 22:00" beside a button it cannot
	// press yet, without a second round-trip or a second clock.
	if hasOccurrence {
		res.Meta = map[string]any{
			"window_start": occ.Start.Format(time.RFC3339),
			"window_end":   occ.End.Format(time.RFC3339),
			"live":         occ.Live,
		}
	}

	switch {
	case !isActive:
		res.Available = false
		res.Reason = "This offer has ended"
	case !branchLive:
		res.Available = false
		res.Reason = "This kitchen is closed"
	case quantity != nil && claimed >= *quantity:
		res.Available = false
		res.Reason = "All claimed"
	case !hasOccurrence:
		res.Available = false
		res.Reason = "This offer has ended"
	case !occ.Live:
		res.Available = false
		// The time is stated, not implied. "Not available" on an offer that
		// opens in ninety minutes loses a viewer who would have come back.
		res.Reason = fmt.Sprintf("Opens at %s", occ.Start.Format("15:04"))
	case inArea != nil && !*inArea:
		res.Available = false
		res.Reason = "Outside this kitchen's delivery area"
	}
	return res, nil
}

// nullableFloat maps the zero-value coordinate to SQL NULL.
//
// A viewer with no location is a supported state — the feed is browsable
// anonymously — and 0,0 is Null Island rather than "unknown". Without this every
// signed-out viewer would be told they are outside every delivery area.
func nullableFloat(v float64) any {
	if v == 0 {
		return nil
	}
	return v
}
